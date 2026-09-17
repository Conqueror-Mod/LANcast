//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	if n.window == nil || n.window.Window() == 0 {
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
		p, err := mpv.New(uint64(n.window.Window()), filepath.Join(clientDataDir(), "mpv.log"), n.emit)
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
	n.window.EnterVideoOverlay()
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
		return n.player.Set("volume", strconv.FormatFloat(clamp(value, 0, 1)*100, 'f', 1, 64))
	case "mute":
		return n.player.Set("mute", yesNo(value != 0))
	case "speed":
		return n.player.Set("speed", strconv.FormatFloat(clamp(value, 0.25, 4), 'f', 3, 64))
	case "audio":
		return n.player.Set("aid", trackID(value))
	case "subtitle":
		return n.player.Set("sid", trackID(value))
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
	if n.window != nil {
		n.window.LeaveVideoOverlay()
	}
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

func (n *nativePlayer) bindings() map[string]any {
	return map[string]any{
		"lancastMpvAvailable": n.available,
		"lancastMpvOpen": func(itemID float64, ticket string) error {
			return n.open(int64(itemID), ticket)
		},
		"lancastMpvCommand": n.command,
		"lancastMpvStop":    n.stop,
	}
}
