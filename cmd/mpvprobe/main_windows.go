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
	"syscall"
	"time"
	"unsafe"

	"lancast/internal/clientwindow"
	"lancast/internal/displaymode"
	"lancast/internal/mpv"

	"golang.org/x/sys/windows"
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW = user32.NewProc("FindWindowW")
)

func main() {
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
	for _, o := range [][2]string{
		{"fullscreen", "yes"},
		{"fs-screen-name", screen},
		{"target-colorspace-hint", mpv.HDRHint(hdr)},
	} {
		if err := p.Set(o[0], o[1]); err != nil {
			fmt.Printf("set %s=%s: %v\n", o[0], o[1], err)
		}
	}
	if err := p.Load(file, start); err != nil {
		return err
	}
	_ = p.Set("pause", "no")

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
