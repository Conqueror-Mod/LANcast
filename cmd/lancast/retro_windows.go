//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lancast/internal/clientwindow"
	"lancast/internal/retro/cores"
	"lancast/internal/retro/host"
	"lancast/internal/retro/libretro"
	"lancast/internal/retro/remote"
)

/*
 * Retro games in the desktop window (ADR 0073, stage 2), as window bindings.
 *
 * The page names a game and passes the stream ticket it minted with its own
 * session, exactly as for libmpv (ADR 0068); this side builds every URL and
 * every path. A compromised page can at most play a game it can already see,
 * with a core this machine already has.
 *
 * The picture goes into a game window of its own (ADR 0076), not libmpv's
 * video window, so a film can go on playing in the docked corner above the
 * game. This side shows the game window while a game runs and hides it the
 * moment one stops; where the film goes is still the page's to say.
 */

type retroPlayer struct {
	origin, pin string

	mu      sync.Mutex
	window  clientwindow.Controller
	session *host.Session
	cancel  context.CancelFunc
	done    chan struct{}

	install coreInstall
}

/*
 * coreInstall is the one download of the emulator cores (ADR 0073), run on
 * its own goroutine because bindings are called on the UI thread and the
 * archive is 230 MB. The page starts it and asks how it is going; a second
 * start while one runs is the same download, not another.
 */
type coreInstall struct {
	mu       sync.Mutex
	running  bool
	progress cores.Progress
	err      string
	cancel   context.CancelFunc
}

func (ci *coreInstall) start() {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	if ci.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ci.running, ci.err, ci.cancel = true, "", cancel
	ci.progress = cores.Progress{Stage: "download", Total: cores.Stable.SizeBytes}
	go func() {
		began := time.Now()
		err := cores.InstallAll(ctx, cores.Stable, retroDir("cores"), cores.All(), func(p cores.Progress) {
			ci.mu.Lock()
			ci.progress = p
			ci.mu.Unlock()
		})
		ci.mu.Lock()
		defer ci.mu.Unlock()
		ci.running, ci.cancel = false, nil
		switch {
		case err == nil:
			slog.Info("retro cores installed", "version", cores.Stable.Version, "took", time.Since(began).Round(time.Second))
		case errors.Is(err, context.Canceled):
			ci.progress = cores.Progress{}
		default:
			slog.Warn("retro cores not installed", "error", err)
			ci.err = err.Error()
		}
	}()
}

func (ci *coreInstall) stop() {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	if ci.cancel != nil {
		ci.cancel()
	}
}

func (ci *coreInstall) status() map[string]any {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	return map[string]any{
		"running": ci.running,
		"stage":   ci.progress.Stage,
		"done":    ci.progress.Done,
		"total":   ci.progress.Total,
		"error":   ci.err,
		"version": cores.Stable.Version,
		"bytes":   cores.Stable.SizeBytes,
	}
}

func retroDir(parts ...string) string {
	return filepath.Join(append([]string{clientDataDir(), "retro"}, parts...)...)
}

// coreOverrides are the cores a person has pointed consoles at, by platform.
// Kept in a file beside the client's other state, not in the server's
// settings: which DLL plays a console is a fact about this machine.
func coreOverrides() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(retroDir("cores.json"))
	if err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func setCoreOverride(platform, path string) error {
	if _, ok := cores.For(platform); !ok {
		return fmt.Errorf("no console called %q", platform)
	}
	path = strings.TrimSpace(path)
	if path != "" {
		if !strings.EqualFold(filepath.Ext(path), ".dll") || !filepath.IsAbs(path) {
			return errors.New("choose a core's .dll by its full path")
		}
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			return fmt.Errorf("there is no file at %s", path)
		}
	}
	m := coreOverrides()
	if path == "" {
		delete(m, platform)
	} else {
		m[platform] = path
	}
	if err := os.MkdirAll(retroDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(retroDir("cores.json"), b, 0o644)
}

/*
 * Core options a person has chosen, per console, kept beside the cores
 * file: like the choice of core, they are about this machine (an upscaling
 * factor one GPU can run another cannot).
 */
func savedOptions(platform string) map[string]string {
	all := map[string]map[string]string{}
	if b, err := os.ReadFile(retroDir("options.json")); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	if all[platform] == nil {
		return map[string]string{}
	}
	return all[platform]
}

func saveOption(platform, key, value string) error {
	if _, ok := cores.For(platform); !ok {
		return fmt.Errorf("no console called %q", platform)
	}
	if key == "" || len(key) > 128 || len(value) > 128 {
		return errors.New("not an option")
	}
	all := map[string]map[string]string{}
	if b, err := os.ReadFile(retroDir("options.json")); err == nil {
		_ = json.Unmarshal(b, &all)
	}
	if all[platform] == nil {
		all[platform] = map[string]string{}
	}
	all[platform][key] = value
	if err := os.MkdirAll(retroDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(all, "", "  ")
	return os.WriteFile(retroDir("options.json"), b, 0o644)
}

// availability is what the page needs to decide between a Play button and a
// sentence saying why not.
func (r *retroPlayer) availability(platform string) map[string]any {
	c, ok := cores.For(platform)
	if !ok {
		return map[string]any{"available": false, "reason": "This console is not supported yet."}
	}
	// needs_gl is reported so the page can say a console draws through the
	// GPU; since stage 3 it no longer stops a game (host.WGL).
	out := map[string]any{"core": c.Display, "licence": c.Licence, "needs_gl": c.NeedsGL}
	path, _, err := cores.Resolve(platform, retroDir("cores"), coreOverrides())
	switch {
	case err == nil:
		out["available"] = true
		out["path"] = path
	case errors.Is(err, cores.ErrNotInstalled) && c.Pinned():
		out["available"] = false
		out["installable"] = true
		out["download_bytes"] = cores.Stable.SizeBytes
		out["reason"] = c.Display + " needs to be downloaded first."
	case errors.Is(err, cores.ErrNotInstalled):
		out["available"] = false
		out["reason"] = "No " + c.Display + " core is installed. Choose one in Settings → This app."
	default:
		out["available"] = false
		out["reason"] = err.Error()
	}
	return out
}

// gameCache is where one server's copy of a game lives, keyed by the server
// so two servers' item 12 are never the same files.
func (r *retroPlayer) gameCache(itemID int64) string {
	sum := sha256.Sum256([]byte(r.origin))
	return retroDir("games", hex.EncodeToString(sum[:6]), fmt.Sprint(itemID))
}

func (r *retroPlayer) emit(e map[string]any) {
	r.mu.Lock()
	w := r.window
	r.mu.Unlock()
	if w == nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	w.Eval("window.__lancastRetroEvent && window.__lancastRetroEvent(" + string(b) + ")")
}

/*
 * open starts a game. Bindings are called on the window's UI thread, so
 * nothing here waits: what can be checked at once is checked (and refused
 * with a reason the page shows), and the rest — waiting for a previous game
 * to finish sending its saves, fetching the files, running — happens on the
 * game's own goroutine.
 */
func (r *retroPlayer) open(itemID int64, ticket, platform string, resume bool) error {
	if itemID <= 0 || ticket == "" {
		return errors.New("a game and a ticket are required")
	}
	previous := r.stop()
	r.mu.Lock()
	w := r.window
	r.mu.Unlock()
	// Made here, on the window's thread, as GameWindow requires; the game's
	// goroutine only ever uses the handle.
	var hwnd uintptr
	if w != nil {
		hwnd = w.GameWindow()
	}
	if hwnd == 0 {
		return errors.New("no window to play into")
	}
	corePath, _, err := cores.Resolve(platform, retroDir("cores"), coreOverrides())
	if err != nil {
		return err
	}
	game, err := remote.New(r.origin, r.pin, itemID, ticket)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.mu.Lock()
	r.cancel, r.done = cancel, done
	r.mu.Unlock()

	go func() {
		defer close(done)
		if previous != nil {
			select {
			case <-previous:
			case <-time.After(45 * time.Second):
				slog.Warn("retro: the previous game is still sending its saves")
			}
		}
		err := r.run(ctx, game, corePath, w, hwnd, resume, platform)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("retro game", "item", itemID, "error", err)
			r.emit(map[string]any{"kind": "error", "text": err.Error()})
		}
	}()
	return nil
}

func (r *retroPlayer) run(ctx context.Context, game *remote.Game, corePath string, w clientwindow.Controller, hwnd uintptr, resume bool, platform string) error {
	r.emit(map[string]any{"kind": "loading", "done": 0, "total": 0})
	last := time.Time{}
	entry, err := game.Download(ctx, r.gameCache(gameID(game)), func(done, total int64) {
		if time.Since(last) > 200*time.Millisecond || done == total {
			last = time.Now()
			r.emit(map[string]any{"kind": "loading", "done": done, "total": total})
		}
	})
	if err != nil {
		return fmt.Errorf("the game could not be fetched: %w", err)
	}
	core, err := libretro.Open(corePath)
	if err != nil {
		return err
	}
	// Zipped dumps are extracted beside the download for a core that cannot
	// open a zip, and a file the core never claimed is refused before it can
	// crash the process.
	if entry, err = host.GameFile(entry, core.SystemInfo(), filepath.Join(filepath.Dir(entry), ".extracted")); err != nil {
		return err
	}
	var data []byte
	if !core.SystemInfo().NeedFullpath {
		if data, err = os.ReadFile(entry); err != nil {
			return err
		}
	}
	for _, d := range []string{retroDir("system"), retroDir("core-saves")} {
		_ = os.MkdirAll(d, 0o755)
	}
	resumeSlot := ""
	if resume {
		resumeSlot = "auto"
	}
	s := host.New(host.Config{
		Core: core, GamePath: entry, GameData: data,
		// The BIOS lives here; LANcast never supplies one (ADR 0073).
		SystemDir: retroDir("system"),
		// Where a core writes anything of its own. The game's save goes to
		// the server through the session, not here.
		SaveDir: retroDir("core-saves"),
		Video:   &host.GDIVideo{HWND: hwnd},
		// The GPU path (stage 3). A framebuffer core never asks for it, so
		// it costs nothing there; an N64 core cannot run without it.
		GL:              &host.WGL{HWND: hwnd},
		Audio:           &host.WaveOut{},
		Input:           host.Controllers{HWND: hwnd},
		Saves:           game,
		Options:         savedOptions(platform),
		ResumeState:     resumeSlot,
		SaveStateOnStop: true,
		OnEvent: func(e host.Event) {
			out := map[string]any{"kind": e.Kind}
			if e.Slot != "" {
				out["slot"] = e.Slot
			}
			if e.Text != "" {
				out["text"] = e.Text
			}
			if e.Options != nil {
				out["options"] = e.Options
			}
			r.emit(out)
		},
		Log: slog.Default(),
	})
	s.SetVolume(r.volume())
	r.mu.Lock()
	r.session = s
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.session = nil
		r.mu.Unlock()
		w.SetGameLayout(false)
	}()
	w.SetGameLayout(true)
	return s.Run(ctx)
}

// gameID recovers the item a remote.Game was made for, for the cache path.
func gameID(g *remote.Game) int64 { return g.ItemID() }

// command applies one control from the page. The set is closed.
func (r *retroPlayer) command(name, slot string) error {
	r.mu.Lock()
	s := r.session
	r.mu.Unlock()
	if s == nil {
		return errors.New("no game is running")
	}
	switch name {
	case "pause":
		s.Pause()
	case "resume":
		s.Resume()
	case "reset":
		s.Reset()
	case "save-state", "load-state":
		if !validStateSlot(slot) {
			return fmt.Errorf("no slot %q", slot)
		}
		if name == "save-state" {
			s.SaveState(slot)
		} else {
			s.LoadState(slot)
		}
	case "stop":
		r.stop()
	default:
		return fmt.Errorf("unknown game command %q", name)
	}
	return nil
}

func validStateSlot(s string) bool {
	return s == "auto" || (len(s) == 7 && strings.HasPrefix(s, "state-") && s[6] >= '0' && s[6] <= '9')
}

/*
 * stop asks the running game to end and returns at once with a channel that
 * closes when it has — saves sent and all.
 *
 * It does not wait, because it is called on the UI thread: closing a game
 * whose save is still uploading must not freeze the window. The game stops
 * drawing within a frame, which is all a film taking the window needs; the
 * upload finishes behind it.
 */
func (r *retroPlayer) stop() <-chan struct{} {
	r.mu.Lock()
	s, cancel, done, w := r.session, r.cancel, r.done, r.window
	r.mu.Unlock()
	// The picture goes now, not when the last save has been sent: the person
	// has left the game, and a film docked above it is what they are looking
	// at next. Run hides it again when it returns, which is harmless.
	if w != nil && (s != nil || cancel != nil) {
		w.SetGameLayout(false)
	}
	switch {
	case s != nil:
		s.Stop()
	case cancel != nil:
		cancel() // still downloading: abandon it
	}
	return done
}

func (r *retroPlayer) bindings() map[string]any {
	return map[string]any{
		"lancastRetroAvailable": r.availability,
		"lancastRetroOpen": func(itemID float64, ticket, platform string, resume bool) error {
			return r.open(int64(itemID), ticket, platform, resume)
		},
		"lancastRetroCommand": r.command,
		"lancastRetroStop":    func() { r.stop() },
		// Fetching the default cores from libretro's pinned stable archive.
		// The page names nothing: the URL, the checksums and where the files
		// go are all this side's.
		"lancastRetroInstallCores":       func() { r.install.start() },
		"lancastRetroCancelInstallCores": func() { r.install.stop() },
		"lancastRetroInstallStatus":      r.install.status,
		// Pointing a console at a core DLL already on this machine — the
		// ADR's advanced option, and the way to play before a build of each
		// core is pinned. An empty path goes back to the default.
		"lancastRetroSetCore": setCoreOverride,
		"lancastRetroCores": func() map[string]string {
			return coreOverrides()
		},
		// A core option, remembered for the console and applied to the
		// running game if there is one.
		"lancastRetroSetOption": func(platform, key, value string) error {
			if err := saveOption(platform, key, value); err != nil {
				return err
			}
			r.mu.Lock()
			s := r.session
			r.mu.Unlock()
			if s != nil {
				s.SetOption(key, value)
			}
			return nil
		},
		// The game's own volume, kept apart from a film's (ADR 0076): both
		// can play at once, and turning one down must not touch the other.
		"lancastRetroVolume": r.volume,
		"lancastRetroSetVolume": func(v float64) error {
			if err := r.setVolume(v); err != nil {
				return err
			}
			r.mu.Lock()
			s := r.session
			r.mu.Unlock()
			if s != nil {
				s.SetVolume(v)
			}
			return nil
		},
	}
}

// retroSettings is the client's own state about games that is not per
// console. Only the volume, so far.
type retroSettings struct {
	Volume *float64 `json:"volume,omitempty"`
}

func readRetroSettings() retroSettings {
	var s retroSettings
	if b, err := os.ReadFile(retroDir("settings.json")); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

// volume is the game volume, 0 to 1, full until somebody chooses otherwise.
func (r *retroPlayer) volume() float64 {
	if v := readRetroSettings().Volume; v != nil && *v >= 0 && *v <= 1 {
		return *v
	}
	return 1
}

func (r *retroPlayer) setVolume(v float64) error {
	if v < 0 || v > 1 || v != v {
		return errors.New("a volume is between 0 and 1")
	}
	s := readRetroSettings()
	s.Volume = &v
	if err := os.MkdirAll(retroDir(), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(retroDir("settings.json"), b, 0o644)
}
