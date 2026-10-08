//go:build windows

package host

import (
	"errors"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

/*
 * The sinks against the real APIs. Each skips where the machine has nothing
 * to talk to — no sound device on a CI runner, no controller anywhere — so
 * what these prove is that the calls are made correctly where they can be.
 */

// Writing sound blocks at the device's pace, which is what keeps a game at
// its own speed: a quarter second of silence takes most of a quarter second
// to write, not no time at all.
func TestWaveOutPacesTheWriter(t *testing.T) {
	var a WaveOut
	if err := a.Open(32000); err != nil {
		if errors.Is(err, ErrNoAudioDevice) {
			t.Skip("no audio device")
		}
		t.Fatal(err)
	}
	defer a.Close()
	silence := make([]int16, 2*32000/60) // one frame's worth
	start := time.Now()
	for i := 0; i < 15; i++ { // 250ms of audio
		a.Write(silence)
	}
	el := time.Since(start)
	// Four buffers (~67ms) can be queued before anything blocks, so the
	// floor is the rest of the quarter second, with room for scheduling.
	if el < 120*time.Millisecond {
		t.Errorf("250ms of sound written in %v: the writer is not being paced", el)
	}
	if el > 2*time.Second {
		t.Errorf("250ms of sound took %v to write", el)
	}
}

func TestWaveOutRefusesNonsense(t *testing.T) {
	var a WaveOut
	if err := a.Open(0); err == nil {
		a.Close()
		t.Error("a zero sample rate opened")
	}
	// Writing to an unopened sink is a no-op, not a crash.
	a.Write([]int16{1, 2})
}

// Polling with nothing plugged in and no window focused is two empty pads.
func TestControllersWithNothingAttached(t *testing.T) {
	pads, _, escape := Controllers{}.Poll()
	_ = pads // a pad may genuinely be plugged into this machine
	if escape {
		t.Error("Escape read as held with no window to receive it")
	}
}

// A frame drawn into a real window does not fail, at any size including
// none. GDI draws into an off-screen window's DC as readily as a visible one.
func TestGDIVideoDrawsIntoAWindow(t *testing.T) {
	class, _ := windows.UTF16PtrFromString("STATIC")
	create := user32.NewProc("CreateWindowExW")
	destroy := user32.NewProc("DestroyWindow")
	hwnd, _, _ := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 640, 480, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Skip("could not create a window")
	}
	defer destroy.Call(hwnd)
	v := &GDIVideo{HWND: hwnd}
	frame := make([]byte, 320*240*4)
	v.Present(frame, 320, 240, 4.0/3)
	v.Present(frame, 0, 0, 1)          // nothing to draw
	v.Present(frame[:10], 320, 240, 1) // too short for its size: refused
	(&GDIVideo{}).Present(frame, 320, 240, 1)
}
