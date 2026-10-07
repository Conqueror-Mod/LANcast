//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/mpv"
)

/*
 * Native playback through libmpv (ADR 0067), as window bindings.
 *
 * The page never hands this code a URL or a path. It names an item and passes
 * the stream ticket it minted with its own session (ADR 0068); the relay builds
 * the only URL mpv will ever open. A compromised page could at most play an
 * item it can already see.
 *
 * libmpv is looked for next to the executable and nowhere else. LANcast does
 * not ship it yet — the licensing question in ADR 0067 gates that — so on a
 * machine without it `lancastMpvAvailable` answers false and the page keeps
 * using the browser's player, unchanged.
 */

type nativePlayer struct {
	origin, pin string

	mu     sync.Mutex
	window clientwindow.Controller
	relay  *mpv.Relay
	player *mpv.Player
	// lastTick throttles timeupdate, which mpv reports every frame; the
	// element raises it about four times a second and the page is written for
	// that rate, not for an Eval per frame.
	lastTick time.Time

	// The audio pass (docs/audio-pass-plan.md). fx is what the page asked
	// for, channels what mpv is decoding, and af the graph last handed to mpv,
	// so an unchanged graph is not re-set — setting af reinitialises the audio
	// chain, which is audible.
	fx       mpv.AudioFX
	channels int
	af       string
}

// applyAF hands mpv the graph for the current settings and channel count, if
// it differs from the one it has. n.mu must be held.
func (n *nativePlayer) applyAF() error {
	if n.player == nil {
		return nil
	}
	g := mpv.AudioFilter(n.fx, n.channels)
	if g == n.af {
		return nil
	}
	if err := n.player.Set("af", g); err != nil {
		return err
	}
	n.af = g
	return nil
}

// mpvDLL is the one place libmpv may be loaded from.
func mpvDLL() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "libmpv-2.dll")
}

func (n *nativePlayer) attach(c clientwindow.Controller) {
	n.mu.Lock()
	n.window = c
	n.mu.Unlock()
}

func (n *nativePlayer) available() bool {
	path := mpvDLL()
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if err := mpv.Load(path); err != nil {
		slog.Warn("libmpv present but unusable", "path", path, "error", err)
		return false
	}
	return true
}

// ensure starts the relay and the player on first use.
func (n *nativePlayer) ensure() error {
	if !n.available() {
		return errors.New("native playback is not available")
	}
	if n.window == nil {
		return errors.New("no window to play into")
	}
	wid := n.window.VideoWindow()
	if wid == 0 {
		return errors.New("no window to play into")
	}
	if n.relay == nil {
		r, err := mpv.NewRelay(n.origin, n.pin)
		if err != nil {
			return err
		}
		n.relay = r
	}
	if n.player == nil {
		p, err := mpv.New(uint64(wid), filepath.Join(clientDataDir(), "mpv.log"), n.emit)
		if err != nil {
			return err
		}
		n.player = p
	}
	return nil
}

// emit forwards mpv's translated events to the page's backend (mpvBackend.ts).
func (n *nativePlayer) emit(s mpv.State, events []string) {
	n.mu.Lock()
	// The channel count is ours, not the page's: rebuild the audio filter for
	// it and forward whatever else arrived alongside.
	if i := slices.Index(events, mpv.AudioChannelsEvent); i >= 0 {
		events = slices.Delete(slices.Clone(events), i, i+1)
		n.channels = s.Channels
		if err := n.applyAF(); err != nil {
			slog.Warn("audio filter", "channels", s.Channels, "error", err)
		}
		if len(events) == 0 {
			n.mu.Unlock()
			return
		}
	}
	w := n.window
	if len(events) == 1 && events[0] == "timeupdate" {
		if time.Since(n.lastTick) < 250*time.Millisecond {
			n.mu.Unlock()
			return
		}
		n.lastTick = time.Now()
	}
	n.mu.Unlock()
	if w == nil {
		return
	}
	b, err := json.Marshal(map[string]any{
		"events":       events,
		"current_time": s.CurrentTime,
		"duration":     jsonNumber(s.Duration),
		"paused":       s.Paused,
		"ended":        s.Ended,
	})
	if err != nil {
		return
	}
	w.Eval("window.__lancastMpvEvent && window.__lancastMpvEvent(" + string(b) + ")")
}

// jsonNumber is nil for NaN, which JSON cannot carry; the page reads null as
// "not known yet", matching the element's NaN.
func jsonNumber(f float64) any {
	if f != f {
		return nil
	}
	return f
}

func (n *nativePlayer) open(itemID int64, ticket string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if itemID <= 0 || ticket == "" {
		return errors.New("an item and a ticket are required")
	}
	if err := n.ensure(); err != nil {
		return err
	}
	n.relay.Forget()
	u, err := n.relay.Register(itemID, ticket)
	if err != nil {
		return err
	}
	slog.Info("native playback", "item", itemID)
	return n.player.Load(u, 0)
}

// command applies one control from the page. The set is closed: anything not
// named here is refused, so the page cannot reach mpv's general command
// language (which can run subprocesses and open files).
func (n *nativePlayer) command(name string, value float64) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.player == nil {
		return errors.New("nothing is playing")
	}
	num := strconv.FormatFloat(value, 'f', -1, 64)
	switch name {
	case "play":
		return n.player.Set("pause", "no")
	case "pause":
		return n.player.Set("pause", "yes")
	case "seek":
		return n.player.Command("seek", num, "absolute+exact")
	case "volume":
		// Not the slider as a percentage: mpv's volume is cubic (mpv.Volume).
		return n.player.Set("volume", strconv.FormatFloat(mpv.Volume(value), 'f', 2, 64))
	case "mute":
		return n.player.Set("mute", yesNo(value != 0))
	case "speed":
		return n.player.Set("speed", strconv.FormatFloat(clamp(value, 0.25, 4), 'f', 3, 64))
	case "audio":
		return n.player.Set("aid", trackID(value))
	case "subtitle":
		return n.player.Set("sid", trackID(value))
	case "night", "dialogue":
		// A number in, a graph out, built by mpv.AudioFilter from its own
		// fixed vocabulary. The page never names a filter (audiofx.go says
		// why: `amovie` reads files).
		n.fx, _ = n.fx.With(name, value)
		return n.applyAF()
	}
	return fmt.Errorf("unknown player command %q", name)
}

func (n *nativePlayer) stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.player != nil {
		_ = n.player.Command("stop")
	}
	if n.relay != nil {
		n.relay.Forget()
	}
	/*
	 * The window is left where the page put it.
	 *
	 * Hiding it here read as an obvious tidy-up and broke every film after the
	 * first: stopping one source to open the next hid the video window, and the
	 * page only re-sends a layout when its own layout changes — which moving
	 * from one film to the next is not. mpv played to a hidden window. Randomize
	 * all was the shape it showed up in, because it is the path that plays one
	 * film after another without ever leaving the player screen.
	 *
	 * So layout has exactly one owner, the page, which is the only side that
	 * knows whether the player is on screen at all.
	 */
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// trackID turns a 1-based mpv track number into its property value; zero or
// less means off.
func trackID(v float64) string {
	if v < 1 {
		return "no"
	}
	return strconv.Itoa(int(v))
}

/*
 * mpvFeatures is what this build's player can do beyond play a file, for the
 * page to ask before it offers a control.
 *
 * The settings panel comes from the server and the player from this client,
 * and the two are routinely different versions: the server updates itself,
 * and the window it is drawn in may be older. v0.9.44's client met a server
 * with night mode and dialogue boost, showed both, and refused every command
 * they sent — silently, because a refused command is caught so playback never
 * stops over it. Somebody tested the feature for an evening against a client
 * that could not run it. An old client has no such binding at all, which the
 * page reads as "none", so the rows only appear where they work.
 */
var mpvFeatures = []string{"audiofx"}

func (n *nativePlayer) bindings() map[string]any {
	return map[string]any{
		"lancastMpvAvailable": n.available,
		"lancastMpvFeatures":  func() []string { return mpvFeatures },
		"lancastMpvOpen": func(itemID float64, ticket string) error {
			return n.open(int64(itemID), ticket)
		},
		"lancastMpvCommand": n.command,
		"lancastMpvStop":    n.stop,
		// Where the picture goes: "full", "mini" with the docked rectangle in
		// client pixels, or "hidden". The page knows its own layout; this
		// window only follows it.
		"lancastMpvLayout": func(layout string, x, y, w, h float64) error {
			n.mu.Lock()
			win := n.window
			n.mu.Unlock()
			if win == nil {
				return errors.New("no window")
			}
			switch layout {
			case "full", "mini", "hidden":
			default:
				return fmt.Errorf("unknown layout %q", layout)
			}
			win.SetVideoLayout(layout, int(x), int(y), int(w), int(h))
			return nil
		},
	}
}
