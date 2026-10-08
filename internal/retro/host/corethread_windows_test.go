//go:build windows

package host

import (
	"context"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"

	"lancast/internal/retro/libretro"
)

// threadCore records the OS thread its Init ran on, then ends the game.
type threadCore struct {
	fakeCore
	tid uint32
}

func (c *threadCore) Init(fe libretro.Frontend) error {
	c.tid = windows.GetCurrentThreadId()
	return c.fakeCore.Init(fe)
}

/*
 * Every session runs on the same OS thread. Mupen64Plus-Next keeps the fiber
 * it made of its first thread in a static; a second game on another thread
 * crashed the client in KERNELBASE. Each caller here holds a thread of its
 * own until the test ends, so a session that merely locked its caller's
 * thread would land on a different one every time.
 */
func TestEverySessionRunsOnTheSameThread(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	var tids []uint32
	for i := 0; i < 3; i++ {
		c := &threadCore{}
		s := New(Config{Core: c, Video: &fakeVideo{}, Saves: newFakeSaves()})
		c.onRun = func(*fakeCore) { s.Stop() }
		errc := make(chan error, 1)
		go func() {
			runtime.LockOSThread()
			errc <- s.Run(context.Background())
			<-release
		}()
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
		tids = append(tids, c.tid)
	}
	if tids[0] == 0 || tids[1] != tids[0] || tids[2] != tids[0] {
		t.Fatalf("sessions ran on threads %v; every one must share the first", tids)
	}
}
