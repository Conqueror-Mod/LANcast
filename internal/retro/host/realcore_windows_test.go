//go:build windows

package host

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/retro/libretro"
)

/*
 * The whole stack on a real DLL: a Session running the libretro package's
 * test core through the syscall binding, paced by waveOut where there is a
 * sound device. Skipped without gcc, like the binding's own test.
 */
func TestSessionDrivesARealCore(t *testing.T) {
	gcc, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("no gcc on the PATH")
	}
	dir, err := os.MkdirTemp("", "lancast-hostcore-")
	if err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(dir, "testcore.dll")
	if b, err := exec.Command(gcc, "-shared", "-O2", "-o", dll,
		filepath.Join("..", "libretro", "testdata", "testcore.c")).CombinedOutput(); err != nil {
		t.Fatalf("gcc: %v\n%s", err, b)
	}

	core, err := libretro.Open(dll)
	if err != nil {
		t.Fatal(err)
	}
	saves := newFakeSaves()
	video := &fakeVideo{}
	var audio AudioSink = &WaveOut{}
	if err := audio.Open(32768); errors.Is(err, ErrNoAudioDevice) {
		audio = nil
	} else if err == nil {
		audio.Close()
	}
	events := make(chan Event, 64)
	s := New(Config{
		Core: core, GamePath: `C:\games\x.tst`, GameData: []byte{9},
		Video: video, Audio: audio, Saves: saves,
		Options:    map[string]string{"test_colour": "blue"},
		FlushEvery: 10 * time.Millisecond,
		OnEvent:    func(e Event) { events <- e },
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	done := make(chan error, 1)
	go func() { done <- s.Run(context.Background()) }()

	start := time.Now()
	time.Sleep(300 * time.Millisecond)
	s.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
	el := time.Since(start)

	video.mu.Lock()
	frames, last := video.frames, append([]byte(nil), video.last...)
	video.mu.Unlock()
	// At 60fps, 300ms is about 18 frames; two thirds of them are drawn (the
	// core repeats every third).
	if frames < 6 || frames > 40 {
		t.Errorf("%d frames drawn in %v: not paced to 60fps", frames, el)
	}
	if len(last) != 4*2*4 || last[0] != 0xFF || last[1] != 0 || last[2] != 0 {
		t.Errorf("last frame starts % x, want blue (the option reached the real core)", last[:4])
	}
	saves.mu.Lock()
	defer saves.mu.Unlock()
	if len(saves.sram) != 16 || saves.sram[2] != 9 {
		t.Errorf("save RAM on stop = % x (byte 2 is the game's first byte)", saves.sram)
	}
}
