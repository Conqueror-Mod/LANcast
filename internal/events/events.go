/*
Package events tells open windows that something changed
([ADR 0079](../../docs/adr/0079-the-server-says-when-something-changed.md)).

It carries the names of things that changed, never the things. A topic means
"ask again", and the answer comes from the same route it always did, so the
server stays the only source of truth and a missed event costs freshness, never
correctness.

In memory and per process, like `together`: a topic only means anything to a
window listening now, so nothing here is written down.

# Coalescing is per subscriber

Each subscriber gathers the topics published to it and hands them over at most
once per window. A scan that touches nine thousand tracks therefore reaches a
window as one event naming `items`, not nine thousand. Coalescing per
subscriber, rather than with one global timer, means a slow window cannot hold
up a fast one, and nothing needs to know who is listening in order to publish.
*/
package events

import (
	"context"
	"sync"
	"time"
)

// DefaultWindow is how long a subscriber gathers topics before handing them
// over. Short enough that a change feels immediate, long enough that a burst
// of writes becomes one refetch.
const DefaultWindow = 250 * time.Millisecond

// Hub fans topic names out to subscribers.
type Hub struct {
	window time.Duration

	mu     sync.Mutex
	subs   map[*Sub]struct{}
	closed bool
}

// Sub is one open stream: one window, belonging to one account.
type Sub struct {
	user   string
	hub    *Hub
	nudge  chan struct{} // capacity one: "there is something pending"
	mu     sync.Mutex
	topics map[string]struct{}
	done   chan struct{} // closed when the hub closes or the sub leaves
	once   sync.Once
}

// New makes a hub whose subscribers coalesce over window. Zero means
// DefaultWindow.
func New(window time.Duration) *Hub {
	if window <= 0 {
		window = DefaultWindow
	}
	return &Hub{window: window, subs: map[*Sub]struct{}{}}
}

// Subscribe opens a stream for user. The caller must Unsubscribe it.
func (h *Hub) Subscribe(user string) *Sub {
	s := &Sub{
		user:   user,
		hub:    h,
		nudge:  make(chan struct{}, 1),
		topics: map[string]struct{}{},
		done:   make(chan struct{}),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		s.finish()
		return s
	}
	h.subs[s] = struct{}{}
	return s
}

// Unsubscribe closes s. Safe to call more than once.
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
	s.finish()
}

// Publish tells every subscriber that topics changed.
func (h *Hub) Publish(topics ...string) {
	h.send(func(*Sub) bool { return true }, topics)
}

/*
PublishTo tells only user's windows.

For changes whose meaning depends on who is asking. Presence is the example:
what a friend's server lets one person here see is not what it lets another
see, so a change in one person's answer is nobody else's business.
*/
func (h *Hub) PublishTo(user string, topics ...string) {
	h.send(func(s *Sub) bool { return s.user == user }, topics)
}

func (h *Hub) send(to func(*Sub) bool, topics []string) {
	if h == nil || len(topics) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		if to(s) {
			s.add(topics)
		}
	}
}

/*
Users is every account with at least one stream open, each once.

The peer watcher asks a friend's server about presence only for these: the
answer is per person, and a person with no window open has nobody to tell.
*/
func (h *Hub) Users() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for s := range h.subs {
		if !seen[s.user] {
			seen[s.user] = true
			out = append(out, s.user)
		}
	}
	return out
}

/*
Close ends every stream, and every later Subscribe returns one already ended.

Called on shutdown before the HTTP server's graceful close. A stream is a
response that never finishes on its own, so without this every open window
would hold the shutdown for its whole grace period.
*/
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	subs := h.subs
	h.subs = map[*Sub]struct{}{}
	h.mu.Unlock()
	for s := range subs {
		s.finish()
	}
}

func (s *Sub) add(topics []string) {
	s.mu.Lock()
	for _, t := range topics {
		s.topics[t] = struct{}{}
	}
	s.mu.Unlock()
	select {
	case s.nudge <- struct{}{}:
	default:
	}
}

func (s *Sub) finish() { s.once.Do(func() { close(s.done) }) }

// Done is closed when the stream has ended.
func (s *Sub) Done() <-chan struct{} { return s.done }

/*
Next waits for something to change and returns every topic gathered over one
coalescing window, or nil when the stream ends or ctx is done.

The window starts at the first topic, not on a fixed clock, so a lone change is
delivered one window after it happens rather than up to a window late.
*/
func (s *Sub) Next(ctx context.Context) []string {
	for {
		select {
		case <-s.nudge:
		case <-s.done:
			return nil
		case <-ctx.Done():
			return nil
		}
		t := time.NewTimer(s.hub.window)
		select {
		case <-t.C:
		case <-s.done:
			t.Stop()
			return nil
		case <-ctx.Done():
			t.Stop()
			// The nudge was spent on topics still waiting in the set. Put it
			// back, or a caller that gives up mid-window (the stream's
			// heartbeat does, every few seconds) strands them until the next
			// change.
			select {
			case s.nudge <- struct{}{}:
			default:
			}
			return nil
		}
		/*
		 * The nudge is drained before the topics are taken, never after.
		 * A publisher adds its topic and then nudges, so a topic added after
		 * the copy below leaves a nudge behind and wakes the next call. Taken
		 * the other way round, a topic could land between the copy and the
		 * drain and lose its nudge, and sit unsent until something else
		 * changed.
		 */
		select {
		case <-s.nudge:
		default:
		}
		s.mu.Lock()
		out := make([]string, 0, len(s.topics))
		for k := range s.topics {
			out = append(out, k)
		}
		s.topics = map[string]struct{}{}
		s.mu.Unlock()
		// A nudge whose topic an earlier call already delivered wakes to an
		// empty set. That is nothing to say, so wait again.
		if len(out) > 0 {
			return out
		}
	}
}
