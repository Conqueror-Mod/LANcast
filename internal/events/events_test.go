package events

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

const window = 20 * time.Millisecond

func next(t *testing.T, s *Sub) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := s.Next(ctx)
	sort.Strings(got)
	return got
}

// nothing asserts that no event arrives within a few windows.
func nothing(t *testing.T, s *Sub) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*window)
	defer cancel()
	if got := s.Next(ctx); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}
}

func TestPublishReachesEverySubscriber(t *testing.T) {
	h := New(window)
	a, b := h.Subscribe("u_a"), h.Subscribe("u_b")
	defer h.Unsubscribe(a)
	defer h.Unsubscribe(b)

	h.Publish("peers")
	for _, s := range []*Sub{a, b} {
		if got := next(t, s); len(got) != 1 || got[0] != "peers" {
			t.Fatalf("got %v, want [peers]", got)
		}
	}
}

// What a friend's server lets one person see is nobody else's business.
func TestPublishToReachesOnlyThatAccount(t *testing.T) {
	h := New(window)
	mine, theirs := h.Subscribe("u_a"), h.Subscribe("u_b")
	defer h.Unsubscribe(mine)
	defer h.Unsubscribe(theirs)

	h.PublishTo("u_a", "presence")
	if got := next(t, mine); len(got) != 1 || got[0] != "presence" {
		t.Fatalf("got %v, want [presence]", got)
	}
	nothing(t, theirs)
}

// A scan touching thousands of rows is one event, not thousands.
func TestABurstIsOneEvent(t *testing.T) {
	h := New(window)
	s := h.Subscribe("u_a")
	defer h.Unsubscribe(s)

	for i := 0; i < 1000; i++ {
		h.Publish("items")
	}
	h.Publish("libraries")
	got := next(t, s)
	if len(got) != 2 || got[0] != "items" || got[1] != "libraries" {
		t.Fatalf("got %v, want [items libraries]", got)
	}
	nothing(t, s)
}

/*
The race the drain order exists for. Publishers running alongside a reader must
never leave a topic stranded: every topic published is delivered by some call
to Next. A topic landing between the copy and a late drain would be lost until
something else changed.
*/
func TestNoTopicIsLostUnderConcurrentPublishing(t *testing.T) {
	h := New(time.Millisecond)
	s := h.Subscribe("u_a")
	defer h.Unsubscribe(s)

	const n = 500
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			h.Publish(topicN(i))
		}
	}()

	seen := map[string]bool{}
	deadline := time.Now().Add(5 * time.Second)
	for len(seen) < n && time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		for _, topic := range s.Next(ctx) {
			seen[topic] = true
		}
		cancel()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("delivered %d distinct topics, want %d", len(seen), n)
	}
}

func topicN(i int) string { return "t" + strconv.Itoa(i) }

// The stream gives up on Next every heartbeat. Giving up mid-window must leave
// what was gathered for the next call, not drop it.
func TestGivingUpMidWindowKeepsTheTopics(t *testing.T) {
	h := New(200 * time.Millisecond)
	s := h.Subscribe("u_a")
	defer h.Unsubscribe(s)

	h.Publish("peers")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if got := s.Next(ctx); got != nil {
		t.Fatalf("got %v before the window closed", got)
	}
	cancel()
	if got := next(t, s); len(got) != 1 || got[0] != "peers" {
		t.Fatalf("got %v, want [peers] on the next call", got)
	}
}

func TestUsersListsEachAccountOnce(t *testing.T) {
	h := New(window)
	a1, a2, b := h.Subscribe("u_a"), h.Subscribe("u_a"), h.Subscribe("u_b")
	got := h.Users()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "u_a" || got[1] != "u_b" {
		t.Fatalf("got %v, want [u_a u_b]", got)
	}
	h.Unsubscribe(a1)
	h.Unsubscribe(a2)
	h.Unsubscribe(b)
	if got := h.Users(); len(got) != 0 {
		t.Fatalf("after unsubscribing: %v", got)
	}
}

// Shutdown must not wait on windows that would otherwise stream for ever.
func TestCloseEndsEveryStream(t *testing.T) {
	h := New(window)
	s := h.Subscribe("u_a")
	ended := make(chan []string, 1)
	go func() { ended <- s.Next(context.Background()) }()
	h.Close()
	select {
	case got := <-ended:
		if got != nil {
			t.Fatalf("got %v from a closed hub", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not return after Close")
	}
	late := h.Subscribe("u_b")
	select {
	case <-late.Done():
	default:
		t.Fatal("a subscription after Close is open")
	}
}

func TestNilHubPublishIsHarmless(t *testing.T) {
	var h *Hub
	h.Publish("peers")
	h.PublishTo("u_a", "presence")
}
