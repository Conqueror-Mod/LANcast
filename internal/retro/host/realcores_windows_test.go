//go:build windows

package host

import (
	"archive/zip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/retro/libretro"
)

/*
 * Real cores against real dumps, headless, when asked:
 *
 *	RETRO_LIB=<a retro library folder>  RETRO_CORES=<folder of *_libretro.dll>
 *
 * Each game runs for eight seconds and must draw a real picture: pixels
 * that are not black from a framebuffer core, or a non-black centre read
 * back from the framebuffer object for a GPU core. This is the test that
 * found three faults no test core could (setter order, the log interface,
 * GL teardown order); RETRO_TRACE=1 prints every environment call, and
 * RETRO_ONLY=<core> runs one. Eight seconds, because Pokemon Emerald is
 * black for its first three.
 */
type capVideo struct {
	mu       sync.Mutex
	frames   int
	nonBlack int
	w, h     int
	aspect   float64
}

func (v *capVideo) Present(b []byte, w, h int, aspect float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.frames++
	v.w, v.h, v.aspect = w, h, aspect
	n := 0
	for i := 0; i+3 < len(b); i += 4 {
		if b[i] > 16 || b[i+1] > 16 || b[i+2] > 16 {
			n++
		}
	}
	v.nonBlack = n
}

func unzipFirst(t *testing.T, zp, dir string) string {
	zr, err := zip.OpenReader(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	f := zr.File[0]
	rc, _ := f.Open()
	defer rc.Close()
	out := filepath.Join(dir, filepath.Base(f.Name))
	w, _ := os.Create(out)
	io.Copy(w, rc)
	w.Close()
	return out
}

func TestRealCoresDrawRealGames(t *testing.T) {
	lib, cores := os.Getenv("RETRO_LIB"), os.Getenv("RETRO_CORES")
	if lib == "" {
		t.Skip()
	}
	cases := []struct{ core, zip string }{
		{"mgba", "gba roms/Pokemon - Emerald Version (USA, Europe).zip"},
		{"mesen", "nes roms/Legend of Zelda, The (USA) (Rev 1).zip"},
		{"bsnes", "snes roms/Bubsy II (USA).zip"},
		{"blastem", "genesis roms/Sonic The Hedgehog 3 (USA).zip"},
		{"mupen64plus_next", "n64 roms/Super Mario 64 (USA).zip"},
	}
	class, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 640, 480, 0, 0, 0, 0)
	if only := os.Getenv("RETRO_ONLY"); only != "" {
		var keep = cases[:0]
		for _, c := range cases {
			if c.core == only {
				keep = append(keep, c)
			}
		}
		cases = keep
	}
	if os.Getenv("RETRO_TRACE") != "" {
		libretro.Trace = func(s string) { t.Log(s) }
	}
	libretro.Log = func(l int, m string) {
		if l >= 2 {
			t.Logf("corelog[%d] %s", l, strings.TrimSpace(m))
		}
	}
	for _, c := range cases {
		t.Run(c.core, func(t *testing.T) {
			dir := t.TempDir()
			game := unzipFirst(t, filepath.Join(lib, c.zip), dir)
			data, _ := os.ReadFile(game)
			core, err := libretro.Open(filepath.Join(cores, c.core+"_libretro.dll"))
			if err != nil {
				t.Fatal(err)
			}
			info := core.SystemInfo()
			t.Logf("core %s %s exts=%s fullpath=%v", info.LibraryName, info.LibraryVersion, info.ValidExtensions, info.NeedFullpath)
			video := &capVideo{}
			gl := &WGL{HWND: hwnd, CaptureCentre: true}
			var msgs []string
			var mu sync.Mutex
			sysdir := filepath.Join(dir, "system")
			os.MkdirAll(sysdir, 0o755)
			s := New(Config{
				Core: core, GamePath: game, GameData: data, Video: video, GL: gl,
				Saves: newFakeSaves(), SystemDir: sysdir, SaveDir: sysdir,
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
				OnEvent: func(e Event) {
					mu.Lock()
					defer mu.Unlock()
					if e.Kind == "options" {
						var ks []string
						for _, o := range e.Options {
							ks = append(ks, o.Key)
						}
						msgs = append(msgs, "options: "+strings.Join(ks, ","))
						return
					}
					msgs = append(msgs, e.Kind+" "+e.Text)
				},
			})
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- s.Run(context.Background()) }()
			time.Sleep(8 * time.Second)
			s.Stop()
			err = <-done
			el := time.Since(start)
			video.mu.Lock()
			t.Logf("ran %v; frames=%d last %dx%d aspect %.3f nonBlack=%d glCentre=%v", el, video.frames, video.w, video.h, video.aspect, video.nonBlack, gl.LastCentre)
			drew := video.nonBlack > 0 || gl.LastCentre != [4]byte{}
			video.mu.Unlock()
			if err != nil {
				t.Errorf("run: %v", err)
			}
			if !drew {
				t.Errorf("no picture after eight seconds")
			}
			mu.Lock()
			for _, m := range msgs {
				if len(m) > 300 {
					m = m[:300] + "…"
				}
				t.Logf("  event %s", m)
			}
			mu.Unlock()
		})
	}
}
