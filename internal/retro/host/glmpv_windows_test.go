//go:build windows

package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/mpv"
	"lancast/internal/retro/libretro"
)

/*
 * A game and a film share the video window, and a window's pixel format can
 * be set once in its life. So after an N64 game has given the window an
 * OpenGL pixel format, every film afterwards is drawn by libmpv's D3D11 path
 * into a window carrying one. This checks that it still plays — rendering,
 * not merely decoding — against a window that never had one, as a control.
 *
 * The windows are made on a thread of their own that pumps messages, as the
 * client's UI thread does: a window nothing pumps is not the window mpv draws
 * into in the app, and the first version of this test, which skipped that,
 * failed its own control.
 *
 * Needs a real libmpv and ffmpeg, so it runs only when asked:
 *
 *	LANCAST_LIBMPV=<path to libmpv-2.dll>
 */
func TestLibmpvStillPlaysAfterAGame(t *testing.T) {
	dll := os.Getenv("LANCAST_LIBMPV")
	if dll == "" {
		t.Skip("LANCAST_LIBMPV not set")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg on the PATH")
	}
	if err := mpv.Load(dll); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp4")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=30",
		"-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}

	control, withGL := pumpedWindows(t, 2)

	play := func(name string, hwnd uintptr) {
		t.Helper()
		log := filepath.Join(dir, name+".log")
		var mu sync.Mutex
		var seen []string
		var pos float64
		p, err := mpv.New(uint64(hwnd), log, func(s mpv.State, ev []string) {
			mu.Lock()
			seen = append(seen, ev...)
			pos = s.CurrentTime
			mu.Unlock()
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		defer p.Close()
		_ = p.Set("ao", "null")
		if err := p.Load(clip, 0); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The player opens a file paused and is told to play once it has
		// loaded, as the page does; unpausing before that is undone by the
		// load. The real-libmpv test in internal/mpv follows the same order.
		for i := 0; i < 200; i++ {
			mu.Lock()
			loaded := slices.Contains(seen, "loadedmetadata")
			mu.Unlock()
			if loaded {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		_ = p.Set("pause", "no")
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := slices.Contains(seen, "playing") && pos > 0.5
			mu.Unlock()
			if ok {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		mu.Lock()
		got, events := pos, slices.Clone(seen)
		mu.Unlock()
		b, _ := os.ReadFile(log)
		if got <= 0.5 {
			t.Errorf("%s: playback never advanced (at %.2fs); events %v", name, got, events)
			for _, l := range strings.Split(string(b), "\n") {
				if strings.Contains(l, "][e][") || strings.Contains(l, "][w][") {
					t.Log(l)
				}
			}
		}
		for _, bad := range []string{"Failed to create", "Could not create", "No render context"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s: mpv.log has %q", name, bad)
			}
		}
	}

	play("control", control)

	// An OpenGL game renders into the second window and leaves, on a locked
	// thread as a session would: a context is current on one OS thread.
	func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		gl := &WGL{HWND: withGL}
		if err := gl.Init(libretro.HWRender{Context: libretro.HWContextOpenGLCore, Major: 3, Minor: 3,
			Depth: true, Stencil: true}, 64, 32); err != nil {
			t.Fatalf("GL init: %v", err)
		}
		gl.Present(64, 32, 2, true)
		gl.Close()
	}()
	dc, _, _ := procGetDC.Call(withGL)
	pf, _, _ := procGetPixelFormat.Call(dc)
	procReleaseDC.Call(withGL, dc)
	if pf == 0 {
		t.Fatal("the window has no pixel format: the case is not the case")
	}

	play("after-gl", withGL)
}

// pumpedWindows makes n visible windows on a thread that pumps their
// messages until the test ends.
func pumpedWindows(t *testing.T, n int) (uintptr, uintptr) {
	t.Helper()
	made := make(chan []uintptr, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		class, _ := windows.UTF16PtrFromString("STATIC")
		create := user32.NewProc("CreateWindowExW")
		var hs []uintptr
		for i := 0; i < n; i++ {
			h, _, _ := create.Call(0, uintptr(unsafe.Pointer(class)), 0,
				0x10000000 /* WS_VISIBLE */ |0x00C00000 /* WS_CAPTION */, uintptr(40+i*340), 40, 320, 240, 0, 0, 0, 0)
			hs = append(hs, h)
		}
		made <- hs
		peek := user32.NewProc("PeekMessageW")
		translate := user32.NewProc("TranslateMessage")
		dispatch := user32.NewProc("DispatchMessageW")
		var msg [48]byte
		for {
			select {
			case <-stop:
				for _, h := range hs {
					user32.NewProc("DestroyWindow").Call(h)
				}
				return
			default:
			}
			for {
				r, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1 /* PM_REMOVE */)
				if r == 0 {
					break
				}
				translate.Call(uintptr(unsafe.Pointer(&msg[0])))
				dispatch.Call(uintptr(unsafe.Pointer(&msg[0])))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	hs := <-made
	t.Cleanup(func() { close(stop); <-done })
	for _, h := range hs {
		if h == 0 {
			t.Fatal("no window")
		}
	}
	return hs[0], hs[1]
}
