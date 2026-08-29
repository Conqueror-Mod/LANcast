package transcode

import (
	"strings"
	"testing"
)

/*
 * What a superseded stream leaves behind in the log.
 *
 * The server log has shown two progressive starts on one item milliseconds
 * apart since before `start_at` was added, and `start_at` did not separate
 * them: both say zero. The reason it stayed unexplained is that the log
 * recorded the *start* of every session at Info and the *end* of a superseded
 * one at Debug — and debug logging is off on a normal server, so the pair of
 * births was the whole record.
 *
 * `served_bytes` is what makes the pair readable. Zero bytes in a few
 * milliseconds is a media stack opening the source twice and abandoning one;
 * real bytes is a player that asked again. Those have different fixes, and
 * neither can be chosen without the number.
 */

func TestASessionCountsWhatItServed(t *testing.T) {
	s := &Session{}
	if got := s.Served(); got != 0 {
		t.Fatalf("a new session served %d bytes, want 0", got)
	}
	s.NoteServed(1500)
	s.NoteServed(500)
	if got := s.Served(); got != 2000 {
		t.Errorf("served = %d, want 2000", got)
	}
}

/*
 * The distinction the field exists to draw, stated as a test so it cannot be
 * quietly collapsed back into a single "a session ended" line.
 */
func TestServedSeparatesAProbeFromARealStream(t *testing.T) {
	probe := &Session{}
	real := &Session{}
	real.NoteServed(4 << 20)

	if probe.Served() != 0 {
		t.Error("a stream that delivered nothing reported bytes")
	}
	if real.Served() == probe.Served() {
		t.Error("a stream that delivered 4MB is indistinguishable from one that delivered none")
	}
}

// The reader is where the count has to happen: nothing else sees a byte leave.
func TestTheReaderCountsWhatItPassesOn(t *testing.T) {
	src := strings.NewReader("0123456789")
	s := &Session{}
	r := &sessionReader{ReadCloser: nopCloser{src}, m: nil, s: s}

	buf := make([]byte, 4)
	if _, err := r.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := s.Served(); got != 4 {
		t.Errorf("after one 4-byte read, served = %d, want 4", got)
	}
}

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
