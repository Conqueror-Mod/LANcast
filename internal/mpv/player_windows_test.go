//go:build windows

package mpv

import (
	"os"
	"slices"
	"sync"
	"testing"
	"time"
)

/*
 * Against a real libmpv, when one is named. Skipped otherwise: CI has no DLL,
 * and LANcast does not ship one until ADR 0067's licensing question is settled.
 *
 *	LANCAST_LIBMPV=<path to libmpv-2.dll> LANCAST_MPV_MEDIA=<short media file>
 *
 * What only this can prove is the syscall layer: the struct layouts read out of
 * mpv's memory, and a double passed through a uintptr to mpv_wait_event. Either
 * wrong gives garbage or a hang rather than a compile error.
 */
func TestRealLibmpvRaisesElementEvents(t *testing.T) {
	dll, media := os.Getenv("LANCAST_LIBMPV"), os.Getenv("LANCAST_MPV_MEDIA")
	if dll == "" || media == "" {
		t.Skip("LANCAST_LIBMPV and LANCAST_MPV_MEDIA not set")
	}
	if err := Load(dll); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var seen []string
	var last State
	p, err := New(0, "", func(s State, ev []string) {
		mu.Lock()
		seen = append(seen, ev...)
		last = s
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, kv := range [][2]string{{"vo", "null"}, {"ao", "null"}} {
		if err := p.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}

	waitFor := func(event string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			ok := slices.Contains(seen, event)
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no %q within 15s; saw %v", event, seen)
	}

	if err := p.Load(media, 0); err != nil {
		t.Fatal(err)
	}
	waitFor("loadedmetadata")
	if err := p.Set("pause", "no"); err != nil {
		t.Fatal(err)
	}
	waitFor("playing")
	waitFor("timeupdate")

	/*
	 * Duration, once it arrives — which is not necessarily by the first frame.
	 * mpv says a file is open before it knows how long it is, exactly as the
	 * element reports NaN until it does, so this waits rather than assuming
	 * the order. What it is really checking is the double read out of mpv's
	 * memory, which would be garbage rather than late if the layout were wrong.
	 */
	deadline := time.Now().Add(15 * time.Second)
	var d float64
	for time.Now().Before(deadline) {
		mu.Lock()
		d = last.Duration
		mu.Unlock()
		if d > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !(d > 0) {
		t.Fatalf("duration = %v after 15s; the double read from mpv's memory is wrong", d)
	}
	if err := p.Command("seek", "100", "absolute-percent"); err != nil {
		t.Fatal(err)
	}
	waitFor("ended")
}
