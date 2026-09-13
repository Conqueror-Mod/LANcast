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
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
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

// availableDisplays is the list the picker shows.
func availableDisplays() []games.Display {
	mons := clientwindow.Monitors()
	here := monitorOfForegroundWindow(mons)

	out := make([]games.Display, 0, len(mons))
	for _, m := range mons {
		w, h := m.Work.Width(), m.Work.Height()
		out = append(out, games.Display{
			Device:  m.Device,
			Label:   games.DisplayLabel(m.Device, w, h, m.Primary),
			Primary: m.Primary,
			Current: m.Device == here,
			Width:   w,
			Height:  h,
		})
	}
	return out
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
	work, ok := workAreaOf(device)
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
				r, ok := windowRect(hwnd)
				if !ok {
					continue
				}
				/*
				 * Already on the chosen screen.
				 *
				 * Recorded rather than ignored, so a window that later moves
				 * itself off is noticed and put back — some games finish
				 * initialising after their window exists and reposition it
				 * onto whichever display their own settings name.
				 */
				cx, cy := r.Left+r.Width()/2, r.Top+r.Height()/2
				if cx >= work.Left && cx < work.Right && cy >= work.Top && cy < work.Bottom {
					if !placed[hwnd] {
						placed[hwnd] = true
						slog.Info("a game window opened on the chosen display already",
							"game", name, "display", device)
					}
					continue
				}

				x, y := games.MoveTarget(r, work)
				if moveWindow(hwnd, x, y) {
					moved++
					placed[hwnd] = true
					lastMove = time.Now()
					slog.Info("moved a game window to the chosen display",
						"game", name, "display", device, "x", x, "y", y, "moved_so_far", moved)
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

func workAreaOf(device string) (games.Rect, bool) {
	for _, m := range clientwindow.Monitors() {
		if m.Device == device {
			return games.Rect{
				Left:   m.Work.Left,
				Top:    m.Work.Top,
				Right:  m.Work.Right,
				Bottom: m.Work.Bottom,
			}, true
		}
	}
	return games.Rect{}, false
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

func windowTitle(hwnd uintptr) string {
	buf := make([]uint16, maxTitleLen)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
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

// moveWindow changes position and nothing else: no size, no z-order, and no
// activation, so a game that is still loading is not yanked in front of
// whatever the person is doing while they wait.
func moveWindow(hwnd uintptr, x, y int) bool {
	ok, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0,
		swpNoSize|swpNoZOrder|swpNoActivate)
	return ok != 0
}
