package dlna

import "testing"

// An unanswered search is logged the first time each sender asks for each
// target, and not again; past its bound the record starts over rather than
// growing.
func TestSeenOnce(t *testing.T) {
	q := newSeenOnce(2)
	if !q.first("192.168.1.1 urn:gateway") {
		t.Fatal("the first search was not logged")
	}
	if q.first("192.168.1.1 urn:gateway") {
		t.Error("a repeat was logged")
	}
	if !q.first("192.168.1.9 urn:gateway") {
		t.Error("a second sender was not logged")
	}
	// Full: the next new key starts the record again.
	if !q.first("192.168.1.5 ssdp:all") {
		t.Error("a third key was not logged")
	}
	if len(q.seen) != 1 {
		t.Errorf("the record grew to %d past its bound", len(q.seen))
	}
}
