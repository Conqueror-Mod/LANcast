package presence

import (
	"sync"
	"testing"
	"time"
)

// clock is a hand-driven time source, so every expiry case below is exact
// rather than a sleep and a hope.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newTest() (*Tracker, *clock) {
	c := &clock{t: time.Date(2026, 8, 23, 20, 0, 0, 0, time.UTC)}
	return newAt(c.now), c
}

func TestSeenMakesSomebodyOnlineButNotWatching(t *testing.T) {
	tr, _ := newTest()
	tr.Seen("chris")

	got, ok := tr.Snapshot("chris")
	if !ok {
		t.Fatal("no snapshot after Seen")
	}
	if !got.Online {
		t.Error("want online")
	}
	if got.Watching != "" {
		t.Errorf("watching = %q, want empty — Seen says nothing about what is playing", got.Watching)
	}
	if !got.Idle() {
		t.Error("want idle: online and not watching is the definition")
	}
}

func TestWatchingDisclosesTheTitleAndImpliesOnline(t *testing.T) {
	tr, _ := newTest()
	tr.Watching("chris", "Blade Runner")

	got, _ := tr.Snapshot("chris")
	if got.Watching != "Blade Runner" {
		t.Errorf("watching = %q, want Blade Runner", got.Watching)
	}
	// A heartbeat that recorded the film but not the activity would produce
	// somebody watching a film while offline.
	if !got.Online {
		t.Error("a playback heartbeat is also activity; want online")
	}
	if got.Idle() {
		t.Error("watching something is not idle")
	}
}

// The trap the federation plan names in advance, and the bug this project has
// already shipped once: Watch Together swept members before recording the
// caller's poll, so a host polling exactly on the interval was judged absent
// and took down their own room mid-film for being punctual.
func TestPunctualHeartbeatSurvives(t *testing.T) {
	tr, c := newTest()
	tr.Watching("chris", "Blade Runner")

	// Exactly on the boundary, which is where a sweep-then-record ordering
	// deletes the entry a moment before refreshing it.
	c.advance(watchingTimeout)
	tr.Watching("chris", "Blade Runner")

	got, ok := tr.Snapshot("chris")
	if !ok {
		t.Fatal("punctual heartbeat was swept: record must happen before sweep")
	}
	if got.Watching != "Blade Runner" {
		t.Errorf("watching = %q, want Blade Runner — the on-time beat was discarded", got.Watching)
	}
}

func TestWatchingExpiresButOnlineSurvivesIt(t *testing.T) {
	tr, c := newTest()
	tr.Watching("chris", "Blade Runner")

	// Long enough that the film is over, well short of going offline. Somebody
	// who paused and wandered off is still here.
	c.advance(watchingTimeout + time.Second)
	tr.Seen("chris")

	got, ok := tr.Snapshot("chris")
	if !ok {
		t.Fatal("want still present")
	}
	if got.Watching != "" {
		t.Errorf("watching = %q, want empty after the heartbeat stopped", got.Watching)
	}
	if !got.Idle() {
		t.Error("want idle: the two signals expire separately, which is the point")
	}
}

func TestGoingQuietRemovesTheEntryEntirely(t *testing.T) {
	tr, c := newTest()
	tr.Watching("chris", "Blade Runner")

	c.advance(onlineTimeout + time.Second)

	// ADR 0045 §4: the sweep must actually delete. Expiry implemented as a
	// display filter is persistence with a polite UI, so the entry must be
	// gone rather than merely reported as offline.
	if _, ok := tr.Snapshot("chris"); ok {
		t.Fatal("expired entry is still there; the sweep hid it instead of deleting it")
	}
	if got := tr.Online(); len(got) != 0 {
		t.Errorf("Online() = %v, want empty", got)
	}
}

func TestStoppedClearsTheTitleWithoutWaitingForTheSweep(t *testing.T) {
	tr, _ := newTest()
	tr.Watching("chris", "Blade Runner")
	tr.Stopped("chris")

	got, ok := tr.Snapshot("chris")
	if !ok {
		t.Fatal("Stopped must not take the person offline, only the film")
	}
	if got.Watching != "" {
		t.Errorf("watching = %q, want empty immediately — the title must not outlive the film", got.Watching)
	}
	if !got.Online {
		t.Error("want still online: they closed the player, not the app")
	}
}

// An empty title is how a caller with nothing to disclose says so — a music
// track, a photo, or an item ADR 0045 §3 excludes. It must not be recorded as a
// film called "".
func TestEmptyTitleIsNotAWork(t *testing.T) {
	tr, _ := newTest()
	tr.Watching("chris", "Blade Runner")
	tr.Watching("chris", "")

	got, _ := tr.Snapshot("chris")
	if got.Watching != "" {
		t.Errorf("watching = %q, want empty", got.Watching)
	}
	if !got.Online {
		t.Error("want online")
	}
}

func TestUnknownAccountHasNothingToSay(t *testing.T) {
	tr, _ := newTest()
	if _, ok := tr.Snapshot("nobody"); ok {
		t.Error("an account never heard from must report ok=false, not a zero State")
	}
}

func TestForgetRemovesImmediately(t *testing.T) {
	tr, _ := newTest()
	tr.Watching("chris", "Blade Runner")
	tr.Forget("chris")
	if _, ok := tr.Snapshot("chris"); ok {
		t.Error("Forget must remove the entry outright")
	}
}

// The empty user id is what an unauthenticated request looks like after the
// middleware has failed to find anybody. Recording it would create a shared
// phantom account that every anonymous request refreshes.
func TestEmptyUserIsIgnored(t *testing.T) {
	tr, _ := newTest()
	tr.Seen("")
	tr.Watching("", "Blade Runner")
	tr.Stopped("")
	if got := tr.Online(); len(got) != 0 {
		t.Errorf("Online() = %v, want empty", got)
	}
}

func TestOnlineIsSortedAndStable(t *testing.T) {
	tr, _ := newTest()
	for _, id := range []string{"georgia", "chris", "alex"} {
		tr.Seen(id)
	}
	first := tr.Online()
	second := tr.Online()
	want := []string{"alex", "chris", "georgia"}
	for i, id := range want {
		if first[i] != id {
			t.Fatalf("Online() = %v, want %v", first, want)
		}
	}
	// A list that reorders itself between reads is a list nobody can render.
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("Online() reordered between calls: %v then %v", first, second)
		}
	}
}

// Presence is read by an HTTP handler while heartbeats arrive from other
// requests, so the map is genuinely shared. Run with -race.
func TestConcurrentUseIsSafe(t *testing.T) {
	tr := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				tr.Watching("chris", "Blade Runner")
				tr.Seen("georgia")
				tr.Snapshot("chris")
				tr.Online()
				tr.Stopped("georgia")
			}
		}()
	}
	wg.Wait()
}
