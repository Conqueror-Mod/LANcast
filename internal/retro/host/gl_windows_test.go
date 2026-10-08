//go:build windows

package host

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"lancast/internal/retro/libretro"
)

/*
 * The OpenGL path against a real driver: a core that asks for a 3.3 core
 * profile, resolves its GL functions through the host, and clears the host's
 * framebuffer to green — read back from that framebuffer to prove the pixels
 * the core drew are the ones the host presents.
 *
 * Skipped without gcc, so never on CI (which builds on Linux). It fails
 * rather than skips when the context cannot be made: a bug in Init looks
 * exactly like a missing driver, and a skip would hide it. What it cannot
 * prove is a real N64 core's renderer, or any GPU but this one.
 */
func TestGLCoreRendersThroughTheHost(t *testing.T) {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH")
	}
	dir, err := os.MkdirTemp("", "lancast-glcore-")
	if err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(dir, "glcore.dll")
	if b, err := exec.Command(gcc, "-shared", "-O2", "-o", dll,
		filepath.Join("..", "libretro", "testdata", "glcore.c")).CombinedOutput(); err != nil {
		t.Fatalf("gcc: %v\n%s", err, b)
	}

	class, _ := windows.UTF16PtrFromString("STATIC")
	create := user32.NewProc("CreateWindowExW")
	destroy := user32.NewProc("DestroyWindow")
	hwnd, _, _ := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 640, 480, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Skip("could not create a window")
	}
	defer destroy.Call(hwnd)

	core, err := libretro.Open(dll)
	if err != nil {
		t.Fatal(err)
	}
	gl := &WGL{HWND: hwnd, CaptureCentre: true}
	started := make(chan struct{}, 1)
	s := New(Config{
		Core: core, GamePath: "x.tst", GameData: []byte{1},
		GL: gl, Saves: newFakeSaves(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnEvent: func(e Event) {
			if e.Kind == "started" {
				started <- struct{}{}
			}
		},
	})
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()
	/*
	 * Timed from "started", not from launch. The first OpenGL use in a
	 * process loads the GPU driver, which took most of a quarter second on
	 * the development machine — a test timed from launch stopped the game
	 * before its first frame and read back an empty framebuffer.
	 */
	select {
	case <-started:
	case err := <-done:
		// A failure, not a skip: a bug in Init returns an error too, and a
		// test that skipped on it would pass with the GL path broken. This
		// file runs only on Windows with gcc, which has a driver.
		t.Fatalf("the game did not start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the game did not start")
	}
	time.Sleep(200 * time.Millisecond)
	s.Stop()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}

	if gl.LastCentre != [4]byte{0, 255, 0, 255} {
		t.Errorf("centre pixel = %v, want opaque green from the core", gl.LastCentre)
	}
	sram := core.Memory(libretro.MemorySaveRAM)
	if sram[0] != 1 || sram[1] != 1 || sram[3] != 0 || sram[2] < 5 {
		t.Errorf("resets %d destroys %d frames %d missing functions %d", sram[0], sram[1], sram[2], sram[3])
	}
	if gl.rc != 0 || gl.dc != 0 {
		t.Error("the context outlived the session")
	}
}

// Without a GL sink, a core asking for a hardware context is told no, and
// the game does not start rather than drawing into nothing.
func TestGLCoreWithoutAGLSinkIsRefused(t *testing.T) {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH")
	}
	dir, _ := os.MkdirTemp("", "lancast-glcore-")
	dll := filepath.Join(dir, "glcore.dll")
	if b, err := exec.Command(gcc, "-shared", "-O2", "-o", dll,
		filepath.Join("..", "libretro", "testdata", "glcore.c")).CombinedOutput(); err != nil {
		t.Fatalf("gcc: %v\n%s", err, b)
	}
	core, err := libretro.Open(dll)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Config{Core: core, GamePath: "x.tst", GameData: []byte{1}, Saves: newFakeSaves(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := s.Run(context.Background()); err == nil {
		t.Error("a GL core started with no GL sink")
	}
}
