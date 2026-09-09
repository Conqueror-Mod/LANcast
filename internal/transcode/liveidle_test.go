package transcode

import (
	"testing"
	"time"
)

/*
 * When a channel nobody is watching stops (ADR 0065).
 *
 * The property the live feature "lives or dies on" already had a test —
 * TestLiveStopsFFmpegWhenTheClientGoes — and it exercises the **progressive**
 * endpoint, the one path that can satisfy it by construction, because there the
 * request context is the lifetime.
 *
 * The HLS path ships and cannot satisfy it that way: a poll of a playlist is
 * not a lifetime, and tying the encode to one would kill the channel between
 * the playlist and its first segment. So it is ended by an idle timeout
 * instead, and nothing asserted anything about that at all.
 *
 * What that cost, on a real server: two minutes of channel surfing left three
 * session slots held by channels already abandoned, and the server refused to
 * play anything else. One of them was still pulling a provider's stream at
 * about 120 KB/s three minutes after Live TV had been left entirely.
 */

// liveSession is a session shaped like a channel's — the negated item id is the
// convention LiveHLS uses, and what IsLive reads.
func liveSession(id string, channelID int64, idleFor time.Duration) *Session {
	s := &Session{ID: id, ItemID: -channelID, Output: HLS}
	s.lastTouch = time.Now().Add(-idleFor)
	return s
}

/*
 * A film somebody is watching, paused.
 *
 * The served bytes are not decoration. A paused film is the case IdleTimeout's
 * ten minutes exist for, and what makes it that case rather than an abandoned
 * session is that picture was collected before the pause — an HLS session that
 * has never handed over a byte is nobody's pause, and is reaped in thirty
 * seconds (see unreadidle_test.go). This fixture predates that distinction and
 * modelled a paused film as one that had served nothing, which was only ever
 * true because the segment route never reported its bytes.
 */
func fileSession(id string, itemID int64, idleFor time.Duration) *Session {
	s := &Session{ID: id, ItemID: itemID, Output: HLS}
	s.lastTouch = time.Now().Add(-idleFor)
	s.NoteServed(4 << 20)
	return s
}

func TestIsLiveReadsTheNegatedChannelID(t *testing.T) {
	if !liveSession("a", 30598, 0).IsLive() {
		t.Error("a channel session did not report itself as live")
	}
	if fileSession("b", 7459, 0).IsLive() {
		t.Error("a film session reported itself as live")
	}
}

/*
 * The two timeouts answer different questions, and this is the whole decision
 * in one table.
 *
 * A paused film keeps its ffmpeg for ten minutes because waiting costs nothing
 * and resuming is expensive. A channel is pulled at full rate the entire time
 * and there is nothing to resume into — coming back to live means joining at
 * now.
 */
func TestIdleLimitIsShorterForAChannel(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())

	if got := m.idleLimit(fileSession("f", 1, 0)); got != m.IdleTimeout {
		t.Errorf("film limit = %v, want the film timeout %v", got, m.IdleTimeout)
	}
	if got := m.idleLimit(liveSession("l", 1, 0)); got != m.LiveIdleTimeout {
		t.Errorf("channel limit = %v, want the live timeout %v", got, m.LiveIdleTimeout)
	}
	if m.LiveIdleTimeout >= m.IdleTimeout {
		t.Errorf("the live timeout (%v) is not shorter than the film's (%v), which is "+
			"the entire point: ten minutes is a film's number",
			m.LiveIdleTimeout, m.IdleTimeout)
	}
	/*
	 * Derived, not picked: a viewer polls for a segment about every
	 * SegmentSeconds, so the timeout has to be several polls — long enough that
	 * a slow network is not mistaken for an empty room, short enough that an
	 * empty room is noticed.
	 */
	if min := time.Duration(3*SegmentSeconds) * time.Second; m.LiveIdleTimeout < min {
		t.Errorf("the live timeout %v is under three segment polls (%v); a viewer on a "+
			"slow connection would be cut off mid-programme", m.LiveIdleTimeout, min)
	}
}

/*
 * A channel nobody has polled is reaped, and a film in the same state is not.
 *
 * Asserted together on one manager, because the failure that matters is not
 * "the number is wrong" but "both sessions were judged by the same number".
 */
func TestReapTakesTheIdleChannelAndLeavesTheIdleFilm(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.LiveIdleTimeout = 30 * time.Second
	m.IdleTimeout = 10 * time.Minute

	// Idle for a minute: gone for a channel, nothing at all for a film.
	m.sessions["chan"] = liveSession("chan", 30598, time.Minute)
	m.sessions["film"] = fileSession("film", 7459, time.Minute)

	m.reap()

	if _, ok := m.sessions["chan"]; ok {
		t.Error("a channel idle for a minute survived; it is still pulling a stream nobody is watching")
	}
	if _, ok := m.sessions["film"]; !ok {
		t.Error("a film idle for a minute was reaped; pausing for a minute must not cost a re-seek")
	}
}

// A manager that never set the live timeout behaves exactly as it did before
// this existed, rather than reaping every channel instantly.
func TestAZeroLiveTimeoutFallsBackToTheFilmTimeout(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.LiveIdleTimeout = 0

	if got := m.idleLimit(liveSession("l", 1, 0)); got != m.IdleTimeout {
		t.Errorf("limit = %v, want the film timeout %v when the live one is unset", got, m.IdleTimeout)
	}
}

/*
 * StopLive is the exact signal, and it is idempotent.
 *
 * Stopping a channel that is not running is what the caller asked for. A race
 * between a beacon, a reload and the reaper is unremarkable and must not read
 * as a failure to a client that can do nothing about it.
 */
func TestStopLiveIsIdempotent(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.sessions["chan"] = liveSession("chan", 30598, 0)

	if !m.StopLive(30598) {
		t.Error("StopLive did not report stopping a running channel")
	}
	if _, ok := m.sessions["chan"]; ok {
		t.Error("the session survived StopLive")
	}
	if m.StopLive(30598) {
		t.Error("StopLive claimed to stop a channel that was not running")
	}
}

// Stopping one channel must not touch another, which is the obvious way to get
// this wrong when sessions are keyed by a negated id.
func TestStopLiveStopsOnlyThatChannel(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.sessions["a"] = liveSession("a", 30598, 0)
	m.sessions["b"] = liveSession("b", 30599, 0)
	m.sessions["film"] = fileSession("film", 30598, 0) // same number, an item

	m.StopLive(30598)

	if _, ok := m.sessions["a"]; ok {
		t.Error("the named channel was not stopped")
	}
	if _, ok := m.sessions["b"]; !ok {
		t.Error("another channel was stopped")
	}
	if _, ok := m.sessions["film"]; !ok {
		t.Error("an item with the same number as the channel was stopped — the negated " +
			"id is what keeps the two numbering schemes apart")
	}
}
