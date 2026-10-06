package main

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/clientwindow"
	"lancast/internal/games"
)

/*
 * Putting a game on the display somebody asked for (ADR 0066's amendment).
 *
 * There is no way to ask for this. steam://rungameid takes no display, and a
 * game picks its screen from its own settings, from whichever display Windows
 * calls primary, or from where it was last time. So the only thing left is to
 * watch for the window it opens and move it.
 *
 * That makes this a guess with a time limit, and it is written to behave like
 * one: it never touches anything but a window that appeared *after* the launch,
 * it gives up after a minute and a half, and when it cannot find one it says so
 * in the log and does nothing. The interface has already told the person that
 * an exclusive-fullscreen game will ignore this.
 */

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows = user32.NewProc("EnumWindows")
	/*
	 * Not GetWindowTextW. It looks like a getter and is not: for a window owned
	 * by another process it sends WM_GETTEXT to that window's thread and waits,
	 * with no timeout at all. See windowTitle.
	 */
	procSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")
	procIsHungAppWindow          = user32.NewProc("IsHungAppWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
)

const (
	swpNoSize     = 0x0001
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// How long to keep looking, and how often. A big game decompressing
	// shaders on a cold start is minutes from a window; ninety seconds is the
	// point past which a watcher is a background process nobody remembers
	// starting, and the cost of giving up is only that the game stays where it
	// opened.
	watchFor   = 90 * time.Second
	watchEvery = 500 * time.Millisecond
	// maxMovesPerWindow is how many times one window is put back before it
	// is left where it puts itself.
	maxMovesPerWindow = 3
	/*
	 * watchAfterMove keeps the watch alive after a window is actually moved,
	 * because a launcher appearing is evidence the game has not yet.
	 *
	 * watchCap ends it regardless. A watcher that a busy desktop can keep
	 * alive for ever is a process moving windows long after anybody would
	 * connect it to having pressed Play.
	 */
	watchAfterMove = 3 * time.Minute
	watchCap       = 10 * time.Minute
	maxTitleLen    = 256

	wmGetText       = 0x000D
	smtoAbortIfHung = 0x0002
	// titleTimeout is how long a window gets to say what it is called. A window
	// that cannot answer in a fifth of a second is busy starting a game, and
	// waiting on it is what wedged this watcher.
	titleTimeout = 200
)

type win32Rect struct{ Left, Top, Right, Bottom int32 }

/*
 * shells are windows that belong to the machine rather than to a game.
 *
 * Excluded by process image name rather than by window title: titles are
 * whatever a program feels like, and they change with what is on screen —
 * Steam's own window is called "Steam" one moment and the name of a store page
 * the next.
 */
var shells = map[string]bool{
	"explorer.exe":             true,
	"steam.exe":                true,
	"steamwebhelper.exe":       true,
	"applicationframehost.exe": true,
	"textinputhost.exe":        true,
	"searchhost.exe":           true,
	"shellexperiencehost.exe":  true,
}

// watchGeneration lets a second launch retire the first watcher. Two games
// started a few seconds apart would otherwise race for the next window that
// appears, and the loser would move the winner's game.
var watchGeneration atomic.Int64

/*
 * displayID is how a screen is remembered: its monitor device path, which
 * names the physical monitor on its port, or the GDI device name when Windows
 * would not give a path.
 *
 * Not the GDI name when there is a choice. That name (`\\.\DISPLAY6`) is
 * renumbered whenever the graphics driver re-enumerates its screens: a choice
 * saved as DISPLAY3 was found pointing at nothing, while the same three
 * monitors had become 6, 7 and 8, and the game quietly opened wherever it liked.
 */
func displayID(m clientwindow.Monitor) string {
	if m.Path != "" {
		return m.Path
	}
	return m.Device
}

// availableDisplays is the list the picker shows.
func availableDisplays() []games.Display {
	mons := clientwindow.Monitors()
	here := monitorOfForegroundWindow(mons)

	screens := make([]games.Screen, len(mons))
	for i, m := range mons {
		screens[i] = games.Screen{
			Name: m.Name, Width: m.Work.Width(), Height: m.Work.Height(), Primary: m.Primary,
			Rect: games.Rect{Left: m.Work.Left, Top: m.Work.Top, Right: m.Work.Right, Bottom: m.Work.Bottom},
		}
	}
	labels := games.DisplayLabels(screens)

	out := make([]games.Display, 0, len(mons))
	for i, m := range mons {
		out = append(out, games.Display{
			Device:  displayID(m),
			Label:   labels[i],
			Primary: m.Primary,
			Current: m.Device == here,
			Width:   screens[i].Width,
			Height:  screens[i].Height,
		})
	}
	return out
}

/*
 * resolveDisplay turns a stored choice into the identity the picker uses now.
 *
 * A choice saved before identities were device paths is a GDI name. While that
 * monitor is still called by that name it is translated, so the choice keeps
 * working and can be saved again in the form that survives renumbering. A name
 * that matches nothing attached is returned as it is: the screen is unplugged,
 * or was renumbered before this could notice, and either way the game opens
 * where it likes and the page can offer the choice again.
 */
func resolveDisplay(stored string) string {
	if stored == "" || stored == games.DisplayDefault {
		return stored
	}
	for _, m := range clientwindow.Monitors() {
		if m.Path != "" && m.Path == stored {
			return stored
		}
		if m.Device == stored {
			return displayID(m)
		}
	}
	return stored
}

// targetMonitor finds the attached screen a stored choice names, in either
// form.
func targetMonitor(device string) (clientwindow.Monitor, bool) {
	for _, m := range clientwindow.Monitors() {
		if (m.Path != "" && m.Path == device) || m.Device == device {
			return m, true
		}
	}
	return clientwindow.Monitor{}, false
}

// monitorOfForegroundWindow is where LANcast is, asked at the moment the page
// calls: the window in front when somebody clicks Play is this one, and "not
// the screen I am reading this on" is the most common thing they want.
func monitorOfForegroundWindow(mons []clientwindow.Monitor) string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	r, ok := windowRect(hwnd)
	if !ok {
		return ""
	}
	cx := r.Left + r.Width()/2
	cy := r.Top + r.Height()/2
	for _, m := range mons {
		if cx >= m.Work.Left && cx < m.Work.Right && cy >= m.Work.Top && cy < m.Work.Bottom {
			return m.Device
		}
	}
	return ""
}

// moveGameToDisplay watches for the game's window and moves it. The watching
// happens in its own goroutine: a launch must not wait ninety seconds to tell
// the page it worked.
func moveGameToDisplay(device, name string) {
	mon, ok := targetMonitor(device)
	work := games.Rect{Left: mon.Work.Left, Top: mon.Work.Top, Right: mon.Work.Right, Bottom: mon.Work.Bottom}
	if !ok {
		// The display was unplugged between choosing it and playing. Leaving
		// the game where it opens is the right failure: the alternative is
		// putting it on a screen nobody asked for.
		slog.Info("game display not attached; leaving the game where it opens",
			"game", name, "display", device)
		return
	}

	mine := watchGeneration.Add(1)
	before := topLevelWindows()
	self := windows.GetCurrentProcessId()
	start := time.Now()

	go func() {
		/*
		 * Every new window, not just the first.
		 *
		 * Stopping at the first one is why this did nothing for Zenless Zone
		 * Zero: its Steam entry starts HoYoPlay — the install directory holds
		 * HYP.exe and its helpers, not the game — so the first window to appear
		 * is the launcher. That got moved, the watcher returned satisfied, and
		 * the game opened minutes later on whatever screen it liked.
		 *
		 * It is not one game's quirk. EA, Ubisoft and Battle.net titles all
		 * arrive through a launcher of their own, and for those the first window
		 * is never the one somebody meant.
		 */
		placed := map[uintptr]bool{}
		attempts := map[uintptr]int{}
		moved := 0
		lastMove := time.Time{}

		for time.Now().Before(watchDeadline(start, lastMove)) {
			time.Sleep(watchEvery)
			if watchGeneration.Load() != mine {
				// Another launch took over.
				return
			}
			for hwnd := range topLevelWindows() {
				if before[hwnd] || !isGameWindow(hwnd, self) {
					continue
				}
				/*
				 * Already on the chosen screen, as Windows sees it: the screen
				 * holding most of the window. Not the window's centre, which
				 * for a window bigger than its screen sits on the next one, and
				 * which kept Minecraft being moved twenty-eight times.
				 *
				 * Recorded rather than ignored, so a window that later moves
				 * itself off is noticed and put back — some games finish
				 * initialising after their window exists and reposition it
				 * onto whichever display their own settings name.
				 */
				if clientwindow.MonitorOfWindow(hwnd) == mon.Device {
					if !placed[hwnd] {
						placed[hwnd] = true
						slog.Info("a game window opened on the chosen display already",
							"game", name, "display", device)
					}
					continue
				}

				/*
				 * A window that keeps leaving is let go. A game that insists on
				 * its own screen will win a tug of war, and losing it every half
				 * second for a minute and a half is worse than losing it once.
				 */
				if attempts[hwnd] >= maxMovesPerWindow {
					continue
				}
				win, maximized, normal, ok := clientwindow.WindowState(hwnd)
				if !ok {
					continue
				}
				plan := games.PlanMove(gameRect(win), maximized, gameRect(normal), work)
				attempts[hwnd]++
				if clientwindow.PlaceWindow(hwnd, plan.X, plan.Y, plan.Width, plan.Height, plan.Restore, plan.Maximize) {
					moved++
					placed[hwnd] = true
					lastMove = time.Now()
					slog.Info("moved a game window to the chosen display",
						"game", name, "display", device, "x", plan.X, "y", plan.Y,
						"width", plan.Width, "height", plan.Height, "maximize", plan.Maximize,
						"moved_so_far", moved)
					if attempts[hwnd] == maxMovesPerWindow {
						slog.Info("a game window keeps moving itself; leaving it where it puts itself",
							"game", name, "display", device)
					}
				} else if !placed[hwnd] {
					// Almost always an elevated game: Windows refuses window
					// changes from a lower-integrity process, and several
					// anti-cheats run as administrator.
					placed[hwnd] = true
					slog.Info("could not move a game window; it is probably running as administrator",
						"game", name, "display", device)
				}
			}
		}
		if moved == 0 {
			slog.Info("no game window appeared in time; leaving it wherever it opened",
				"game", name, "display", device)
		} else {
			slog.Info("finished watching for game windows",
				"game", name, "display", device, "moved", moved)
		}
	}()
}

// watchDeadline is when to stop looking. The rule is in internal/games, where
// it can be tested without a window in sight.
func watchDeadline(start, lastMove time.Time) time.Time {
	return games.WatchDeadline(start, lastMove, watchFor, watchAfterMove, watchCap)
}

func topLevelWindows() map[uintptr]bool {
	out := map[uintptr]bool{}
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		out[hwnd] = true
		return 1 // keep enumerating
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return out
}

// isGameWindow applies the rules that need Win32 — visibility, whose process it
// is — around the ones that do not, which live in internal/games and are tested
// without a window in sight.
func isGameWindow(hwnd uintptr, self uint32) bool {
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return false
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 || pid == self {
		return false
	}
	if shells[strings.ToLower(processName(pid))] {
		return false
	}
	r, ok := windowRect(hwnd)
	if !ok {
		return false
	}
	return games.LooksLikeGameWindow(windowTitle(hwnd), r.Width(), r.Height())
}

/*
 * windowTitle, without the call that hangs.
 *
 * GetWindowTextW sends WM_GETTEXT to a window owned by another process and
 * waits for that thread to answer, with no timeout. A game still loading — or
 * an anti-cheat that does not pump messages — blocks it for ever, and it takes
 * the whole watcher with it.
 *
 * That is not hypothetical. A launch of Zenless Zone Zero left a client that
 * was perfectly responsive, with its UI thread answering in nine milliseconds,
 * and not one line in its own log: the watching goroutine was parked in here
 * before it could move a window or report that it had not. The game opened on
 * the main display, the launcher happened to be on the chosen one because
 * HoYoPlay remembers its own position, and the feature looked half-working
 * while doing nothing at all.
 *
 * SendMessageTimeout asks the same question and gives up. SMTO_ABORTIFHUNG
 * refuses outright for a window whose thread is already known to be hung.
 */
func windowTitle(hwnd uintptr) string {
	if hung, _, _ := procIsHungAppWindow.Call(hwnd); hung != 0 {
		return ""
	}
	buf := make([]uint16, maxTitleLen)
	var answer uintptr
	ok, _, _ := procSendMessageTimeoutW.Call(hwnd, wmGetText,
		uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])),
		smtoAbortIfHung, titleTimeout, uintptr(unsafe.Pointer(&answer)))
	if ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func windowRect(hwnd uintptr) (games.Rect, bool) {
	var r win32Rect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return games.Rect{}, false
	}
	return games.Rect{
		Left:   int(r.Left),
		Top:    int(r.Top),
		Right:  int(r.Right),
		Bottom: int(r.Bottom),
	}, true
}

func processName(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

// gameRect copies a clientwindow rectangle into the games package's own type
// (internal/games keeps its own, so the rules stay free of the window code).
func gameRect(r clientwindow.Rect) games.Rect {
	return games.Rect{Left: r.Left, Top: r.Top, Right: r.Right, Bottom: r.Bottom}
}
