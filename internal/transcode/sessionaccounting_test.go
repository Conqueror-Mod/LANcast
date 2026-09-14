package transcode

import (
	"testing"
	"time"
)

/*
 * Why a server with three slots refused sixteen requests in sixteen seconds.
 *
 * From a real log, the forty seconds before a film would not play:
 *
 *     22:13:56  transcode started  item=7694  (hls)
 *     22:14:03  transcode started  item=6688  (hls)
 *     22:14:04  transcode started  item=6688  (progressive)
 *     22:14:05  refused a transcode: at the session ceiling  running=3 max=3
 *
 * Two faults, and neither is the ceiling being too low.
 *
 * One film held two slots, because each start superseded only its own delivery
 * method: a player that fell back from segments to a progressive stream left
 * the segments behind, holding a slot for a viewer who had already moved on.
 *
 * And nothing could be evicted to make room, because eviction defends any
 * session touched in the last ninety seconds — including sessions that had
 * never handed over a single byte, which the reaper is meanwhile willing to
 * destroy at thirty. Three such sessions, all started within half a minute,
 * refuse every request made on the machine.
 */

// viewerSession is one account watching one film, delivered a particular way.
func viewerSession(id string, itemID int64, owner string, out Output, idleFor time.Duration, served int) *Session {
	s := &Session{ID: id, ItemID: itemID, Owner: owner, Output: out}
	s.lastTouch = time.Now().Add(-idleFor)
	if served > 0 {
		s.NoteServed(served)
	}
	return s
}

func TestFallingBackToTheOtherDeliveryDoesNotHoldTwoSlots(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.sessions["segments"] = viewerSession("segments", 6688, "u1", HLS, 0, 0)

	// The player gave up on segments and asked for a progressive stream. That is
	// one viewer changing their mind, not a second viewer arriving.
	m.supersedeOutput("u1", 6688, HLS)

	if _, ok := m.sessions["segments"]; ok {
		t.Error("the abandoned HLS session still holds a slot after the fallback")
	}
}

func TestSupersedingOneViewerLeavesAnother(t *testing.T) {
	// Two people watching one film at once is a thing a media server must do.
	m := NewManager(t.TempDir(), quiet())
	m.sessions["mine"] = viewerSession("mine", 6688, "u1", HLS, 0, 0)
	m.sessions["theirs"] = viewerSession("theirs", 6688, "u2", HLS, 0, 0)

	m.supersedeOutput("u1", 6688, HLS)

	if _, ok := m.sessions["theirs"]; !ok {
		t.Error("another account watching the same film lost its session")
	}
}

func TestAnAnonymousOwnerIsNeverCollapsed(t *testing.T) {
	/*
	 * The unconfigured loopback state, where every request is anonymous.
	 * Treating those as one player would let a second viewer end the first
	 * viewer's film.
	 */
	m := NewManager(t.TempDir(), quiet())
	m.sessions["a"] = viewerSession("a", 6688, "", HLS, 0, 0)

	m.supersedeOutput("", 6688, HLS)

	if _, ok := m.sessions["a"]; !ok {
		t.Error("an anonymous session was collapsed into another")
	}
}

func TestGraceDefendsASessionThatIsFeedingSomebody(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.UnreadIdleTimeout = 30 * time.Second
	s := viewerSession("watching", 7005, "u1", HLS, 0, 4<<20)

	if got := m.evictionGrace(s); got != EvictionGrace {
		t.Errorf("grace = %s, want the full %s for a session handing over picture", got, EvictionGrace)
	}
}

func TestGraceDoesNotDefendASessionFeedingNobody(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	m.UnreadIdleTimeout = 30 * time.Second
	s := viewerSession("unread", 7005, "u1", HLS, 0, 0)

	if got := m.evictionGrace(s); got != 30*time.Second {
		t.Errorf("grace = %s, want the unread timeout", got)
	}
	// The contradiction that caused the refusals: defended for ninety seconds
	// by eviction, destroyed at thirty by the reaper.
	if m.evictionGrace(s) >= EvictionGrace {
		t.Error("a session that has served nothing is defended as long as one being watched")
	}
}

func TestReserveEvictsAnUnreadSessionRatherThanRefusing(t *testing.T) {
	// The reported failure, in one test: three sessions, none ever read from,
	// all inside the ninety-second grace, and a film refused.
	m := NewManager(t.TempDir(), quiet())
	m.MaxSessions = 3
	m.UnreadIdleTimeout = 30 * time.Second
	for i, id := range []string{"a", "b", "c"} {
		m.sessions[id] = viewerSession(id, int64(7000+i), "u1", HLS, 40*time.Second, 0)
	}

	if err := m.reserve(); err != nil {
		t.Fatalf("refused with three unread sessions held: %v", err)
	}
	if len(m.sessions) != 2 {
		t.Errorf("sessions = %d, want one evicted to make room", len(m.sessions))
	}
}

func TestReserveStillRefusesWhenEverySlotIsBeingWatched(t *testing.T) {
	/*
	 * The case the ceiling exists for, and the one this must not break. Every
	 * slot is feeding a player, so refusing is the right answer: taking one
	 * would stop somebody else's film to start this one.
	 */
	m := NewManager(t.TempDir(), quiet())
	m.MaxSessions = 3
	m.UnreadIdleTimeout = 30 * time.Second
	for i, id := range []string{"a", "b", "c"} {
		m.sessions[id] = viewerSession(id, int64(7000+i), "u1", HLS, time.Second, 4<<20)
	}

	if err := m.reserve(); err == nil {
		t.Error("a slot was taken from a session that was feeding a player")
	}
}
