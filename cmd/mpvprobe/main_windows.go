//go:build windows

// mpvprobe is a SPIKE control for docs/uncomposed-video-plan.md: LANcast's own
// libmpv, options and refresh-rate matching, but with mpv in a fullscreen
// window of its own instead of inside LANcast's. If Windows flips this one
// straight to the TV and not LANcast's, the way LANcast hosts mpv is what keeps
// video composed. If this one is composed too, it is the desktop.
//
//	mpvprobe.exe -file "W:\Films\Some Film.mkv" [-screen DISPLAY3] [-start 600] [-seconds 150] [-hdr]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"lancast/internal/clientwindow"
	"lancast/internal/displaymode"
	"lancast/internal/mpv"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procSetDpiContext       = user32.NewProc("SetThreadDpiAwarenessContext")
)

const swpNoZOrder = 0x0004

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfoEx struct {
	cbSize  uint32
	monitor rect
	work    rect
	flags   uint32
	device  [32]uint16
}

// monitorRect finds a monitor by its Windows name ("DISPLAY3" or
// "\\.\DISPLAY3"), in physical pixels.
func monitorRect(name string) (rect, bool) {
	// Physical pixels, as the window is placed in them.
	_, _, _ = procSetDpiContext.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2 (-4)
	want := strings.TrimPrefix(strings.ToUpper(name), `\\.\`)
	var found rect
	var ok bool
	cb := windows.NewCallback(func(hmon, _, _, _ uintptr) uintptr {
		mi := monitorInfoEx{cbSize: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		_, _, _ = procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
		if strings.TrimPrefix(strings.ToUpper(windows.UTF16ToString(mi.device[:])), `\\.\`) == want {
			found, ok = mi.monitor, true
			return 0
		}
		return 1
	})
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return found, ok
}

func main() {
	// One OS thread, so the DPI context set for placing the window holds.
	runtime.LockOSThread()
	file := flag.String("file", "", "film to play")
	screen := flag.String("screen", "DISPLAY3", "monitor to play on, as Windows names it")
	start := flag.Float64("start", 600, "seconds into the film to start")
	seconds := flag.Int("seconds", 150, "how long to play")
	hdr := flag.Bool("hdr", true, "hint HDR to the display, as LANcast does with 'Send HDR' on")
	dll := flag.String("dll", `C:\Program Files\LANcast\libmpv-2.dll`, "libmpv to load")
	flag.Parse()
	if *file == "" {
		fmt.Println("usage: mpvprobe -file <film>")
		os.Exit(2)
	}
	if err := run(*file, *screen, *dll, *start, *seconds, *hdr); err != nil {
		fmt.Println("mpvprobe:", err)
		os.Exit(1)
	}
}

func run(file, screen, dll string, start float64, seconds int, hdr bool) error {
	if err := mpv.Load(dll); err != nil {
		return err
	}
	dir := filepath.Join(os.TempDir(), "mpvprobe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// A crash in an earlier run left the TV at the film's rate.
	_, _, _ = displaymode.Recover(dir)

	p, err := mpv.New(mpv.OwnWindow, filepath.Join(dir, "mpv.log"), func(mpv.State, []string) {})
	if err != nil {
		return err
	}
	defer p.Close()
	// Fullscreen on whichever monitor the window is on: the window is moved
	// onto the screen asked for below, since mpv's own screen options landed
	// it on the main display.
	for _, o := range [][2]string{
		{"fs-screen", "current"},
		{"target-colorspace-hint", mpv.HDRHint(hdr)},
	} {
		if err := p.Set(o[0], o[1]); err != nil {
			fmt.Printf("set %s=%s: %v\n", o[0], o[1], err)
		}
	}
	// Load opens paused (as LANcast's player does); unpaused once the window
	// is placed, since a pause=no sent now is undone when the file opens.
	if err := p.Load(file, start); err != nil {
		return err
	}

	// The window mpv made, once it exists, decides which monitor to match.
	var hwnd uintptr
	class, _ := windows.UTF16PtrFromString("mpv")
	for i := 0; i < 100 && hwnd == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		hwnd, _, _ = procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0)
	}
	if hwnd == 0 {
		return fmt.Errorf("mpv never made a window")
	}
	rc, ok := monitorRect(screen)
	if !ok {
		return fmt.Errorf("no monitor named %s", screen)
	}
	// Into the middle of that monitor, then fullscreen there.
	w, h := (rc.Right-rc.Left)/2, (rc.Bottom-rc.Top)/2
	_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(rc.Left+w/2), uintptr(rc.Top+h/2),
		uintptr(w), uintptr(h), swpNoZOrder)
	time.Sleep(300 * time.Millisecond)
	if err := p.Set("fullscreen", "yes"); err != nil {
		return fmt.Errorf("fullscreen: %w", err)
	}
	time.Sleep(time.Second)
	var got rect
	_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&got)))
	fmt.Printf("mpv window %+v, %s is %+v\n", got, screen, rc)
	if err := p.Set("pause", "no"); err != nil {
		return fmt.Errorf("unpausing: %w", err)
	}
	fps, _ := p.Double("container-fps")
	sw := displaymode.NewSwitcher(dir)
	defer func() {
		if err := sw.Restore(); err != nil {
			fmt.Println("restoring the display:", err)
		} else {
			fmt.Println("display restored")
		}
	}()
	res, err := sw.Match(hwnd, fps)
	if err != nil {
		return err
	}
	fmt.Printf("film %.3f fps on %s: %s -> %+v\n", fps, res.Device, res.From, res.Decision)
	time.Sleep(3 * time.Second)
	if hz, ok := clientwindow.RefreshRate(res.Device); ok {
		for _, o := range mpv.DisplayTiming(hz) {
			_ = p.Set(o.Name, o.Value)
		}
		fmt.Printf("display at %.3f Hz\n", hz)
	}

	fmt.Printf("playing for %d s. Start the capture now; Ctrl+C stops early.\n", seconds)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-time.After(time.Duration(seconds) * time.Second):
	case <-stop:
	}
	dropped, _ := p.Int64("frame-drop-count")
	delayed, _ := p.Int64("vo-delayed-frame-count")
	fmt.Printf("mpv's own counters: dropped %d, delayed %d\n", dropped, delayed)
	return nil
}
