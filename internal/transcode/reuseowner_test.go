package transcode

import (
	"context"
	"testing"
)

/*
 * A session is reused only by the viewer it was started for (review finding
 * R09-2).
 *
 * Keyed on item and offset alone, a second account pressing play on the same
 * film from the same place was handed the first account's session — its audio
 * track, its quality, its codec decision — and lost it the moment the first
 * account sought, because supersedeHLS stops sessions by owner and the shared
 * one still carried the first owner's name.
 *
 * No ffmpeg runs: the binary does not exist, so a request that is not reused
 * fails to start, which is how this tells reuse from a new session.
 */
func TestHLSReuseDoesNotCrossOwners(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.bin = "ffmpeg-not-run"

	alice, err := m.AdoptForTest(7, "alice")
	if err != nil {
		t.Fatal(err)
	}

	got, err := m.EnsureHLS(context.Background(), 7, "bob", Options{AudioIndex: 3, StartAt: 2})
	if got == alice {
		t.Fatal("bob was handed alice's session")
	}
	if err == nil {
		t.Fatal("fixture: a session started with no ffmpeg")
	}
	if m.Session(alice.ID) == nil {
		t.Fatal("bob pressing play stopped alice's session")
	}

	// Alice asking again at the same place is still her own session reused.
	again, err := m.EnsureHLS(context.Background(), 7, "alice", Options{StartAt: 2})
	if err != nil || again != alice {
		t.Fatalf("alice asking again got %v, %v — want her own session back", again, err)
	}
}
