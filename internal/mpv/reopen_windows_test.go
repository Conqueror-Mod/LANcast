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
 * Re-opening the same file, against a real libmpv.
 *
 * This is the case the page depends on and the one a pure test cannot reach:
 * changing the audio track re-opens the file that is already playing, so every
 * property mpv might report about it — duration above all — is unchanged, and
 * mpv reports a property only when its value changes. If "the file is open"
 * rides on one of those properties, the second open says nothing and the page
 * never starts trusting the clock again.
 *
 *	LANCAST_LIBMPV=<libmpv-2.dll> LANCAST_MPV_MEDIA=<a media file>
 */
func TestRealLibmpvAnnouncesASecondOpenOfTheSameFile(t *testing.T) {
	dll, media := os.Getenv("LANCAST_LIBMPV"), os.Getenv("LANCAST_MPV_MEDIA")
	if dll == "" || media == "" {
		t.Skip("LANCAST_LIBMPV and LANCAST_MPV_MEDIA not set")
	}
	if err := Load(dll); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var seen []string
	p, err := New(0, "", func(_ State, ev []string) {
		mu.Lock()
		seen = append(seen, ev...)
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

	open := func(what string) {
		t.Helper()
		mu.Lock()
		seen = nil
		mu.Unlock()
		if err := p.Load(media, 0); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			got := slices.Contains(seen, "loadedmetadata")
			mu.Unlock()
			if got {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("%s: no loadedmetadata within 15s; saw %v", what, seen)
	}

	open("first open")
	// The same file again, exactly as changing the audio track does.
	open("second open of the same file")
}
