package transcode

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

/*
 * A session nobody ever watched does not get a viewer's allowance.
 *
 * IdleTimeout's ten minutes protect one thing: a paused film keeping its
 * ffmpeg, because waiting is free and resuming is expensive. That is a bargain
 * struck on behalf of somebody who is *there*.
 *
 * Skipping around an episode three times started five sessions in seven
 * seconds — each seek asks for a playlist, falls back to the progressive
 * stream about six hundred milliseconds later, and abandons the HLS session
 * where it stands. The third seek was refused (`running=3 max=3`), the server
 * would play nothing at all, and the slots were held for ten minutes after the
 * person had given up.
 *
 * The discriminator is whether any picture was ever collected, which is why
 * these are worth stating: it only became a real number when the segment route
 * started reporting bytes. Before that every HLS session read as zero whether
 * it was working or not, and this rule would have killed a film mid-scene.
 */

func idleManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAnUnreadHLSSessionIsNotGivenAPausedFilmsAllowance(t *testing.T) {
	m := idleManager(t)
	s := &Session{ID: "abandoned", ItemID: 37106, Output: HLS}

	if got := m.idleLimit(s); got != m.UnreadIdleTimeout {
		t.Errorf("idleLimit = %v, want %v — an HLS session that never served a "+
			"byte holds a slot for the full ten minutes, which is how three "+
			"seeks stopped the server playing anything", got, m.UnreadIdleTimeout)
	}
}

func TestAnHLSSessionSomebodyIsWatchingKeepsTheFullAllowance(t *testing.T) {
	m := idleManager(t)
	s := &Session{ID: "watched", ItemID: 37106, Output: HLS}
	s.NoteServed(1 << 20)

	if got := m.idleLimit(s); got != m.IdleTimeout {
		t.Fatalf("idleLimit = %v, want %v — a paused film is exactly what the "+
			"ten minutes exist for, and this reaps it after thirty seconds",
			got, m.IdleTimeout)
	}
}

/*
 * Progressive is deliberately untouched.
 *
 * It is one held request, so the transport itself reports the client leaving —
 * there is no polls-are-not-a-lifetime gap to close, and a session whose first
 * byte is slow to arrive would be reaped underneath somebody still waiting for
 * it. The leak that was measured was on the HLS path; this rule goes no further
 * than the evidence does.
 */
func TestAProgressiveSessionIsNotSubjectToTheUnreadRule(t *testing.T) {
	m := idleManager(t)
	s := &Session{ID: "starting", ItemID: 37106, Output: Progressive}

	if got := m.idleLimit(s); got != m.IdleTimeout {
		t.Errorf("idleLimit = %v, want %v", got, m.IdleTimeout)
	}
}

// A channel is still a channel. Live has its own number and its own reasons
// (ADR 0065), and it wins — a live session also serves through the HLS path.
func TestALiveChannelKeepsItsOwnTimeout(t *testing.T) {
	m := idleManager(t)
	s := &Session{ID: "channel", ItemID: -30596, Output: HLS}

	if got := m.idleLimit(s); got != m.LiveIdleTimeout {
		t.Errorf("idleLimit = %v, want %v", got, m.LiveIdleTimeout)
	}
}

/*
 * A zero disables it, the same way LiveIdleTimeout's does.
 *
 * A Manager built by hand and not told about this field behaves as it did
 * before the field existed, rather than reaping everything in half a minute.
 */
func TestAZeroUnreadTimeoutLeavesBehaviourAsItWas(t *testing.T) {
	m := idleManager(t)
	m.UnreadIdleTimeout = 0
	s := &Session{ID: "abandoned", ItemID: 37106, Output: HLS}

	if got := m.idleLimit(s); got != m.IdleTimeout {
		t.Errorf("idleLimit = %v, want %v", got, m.IdleTimeout)
	}
}

// The reaper acts on it, not just the rule. Stated separately because the two
// have been out of step before: a correct limit nothing consults frees nothing.
func TestTheReaperTakesAnAbandonedHLSSession(t *testing.T) {
	m := idleManager(t)
	m.UnreadIdleTimeout = time.Millisecond

	abandoned := &Session{ID: "abandoned", ItemID: 37106, Output: HLS}
	watched := &Session{ID: "watched", ItemID: 37106, Output: HLS}
	watched.NoteServed(1 << 20)
	for _, s := range []*Session{abandoned, watched} {
		s.Touch()
		m.sessions[s.ID] = s
	}

	time.Sleep(5 * time.Millisecond)
	m.reap()

	if m.Session("abandoned") != nil {
		t.Error("the abandoned session still holds a slot")
	}
	if m.Session("watched") == nil {
		t.Error("a session that had served a film was reaped with it")
	}
}

/*
 * A seek stops the position it left, rather than stacking beside it.
 *
 * The reaper alone does not fix what was reported. An abandoned HLS session is
 * now taken in thirty seconds instead of ten minutes, and three seeks take
 * about seven — so waiting would still have filled a ceiling of three and
 * refused the episode outright.
 *
 * EnsureHLS reuses the session for the offset being asked for before any of
 * this runs, so anything these tests find is at a different offset, which is
 * what a seek is and what nothing will ever request again.
 */

func hlsAt(id string, itemID int64, owner string, startAt float64) *Session {
	s := &Session{ID: id, ItemID: itemID, Owner: owner, Output: HLS, StartAt: startAt}
	s.Touch()
	return s
}

func TestSeekingStopsThePositionThisViewerLeft(t *testing.T) {
	m := idleManager(t)
	m.sessions["at115"] = hlsAt("at115", 37106, "u_3f9", 115)

	m.supersedeHLS("u_3f9", 37106)

	if m.Session("at115") != nil {
		t.Error("the position the viewer seeked away from still holds a slot; " +
			"three of those is the ceiling, and the server then plays nothing")
	}
}

/*
 * Two people watching one episode do not end each other's playback.
 *
 * The reason this is keyed on (owner, item) and not on item alone, and the
 * failure it would cause is worse than the one being fixed: a household where
 * starting a programme stops somebody else's.
 */
func TestSeekingDoesNotDisturbAnotherViewer(t *testing.T) {
	m := idleManager(t)
	m.sessions["mine"] = hlsAt("mine", 37106, "u_3f9", 115)
	m.sessions["theirs"] = hlsAt("theirs", 37106, "u_a21", 300)

	m.supersedeHLS("u_3f9", 37106)

	if m.Session("theirs") == nil {
		t.Fatal("one viewer's seek ended another viewer's episode")
	}
	if m.Session("mine") != nil {
		t.Error("the viewer's own abandoned position survived")
	}
}

// A different item is a different thing being watched. Somebody who leaves a
// film running and starts an episode has two sessions on purpose.
func TestSeekingDoesNotDisturbTheSameViewersOtherItem(t *testing.T) {
	m := idleManager(t)
	m.sessions["film"] = hlsAt("film", 7459, "u_3f9", 481)

	m.supersedeHLS("u_3f9", 37106)

	if m.Session("film") == nil {
		t.Error("seeking in one item stopped the same viewer's other item")
	}
}

/*
 * Anonymous is not an owner.
 *
 * An unsecured server binds loopback only and every request arrives with no
 * account, so collapsing on "" would treat every viewer on that machine as one
 * player — the same reason superseding a progressive stream refuses to.
 */
func TestAnonymousSessionsAreNotCollapsedTogether(t *testing.T) {
	m := idleManager(t)
	m.sessions["anon"] = hlsAt("anon", 37106, "", 115)

	m.supersedeHLS("", 37106)

	if m.Session("anon") == nil {
		t.Error("an anonymous session was superseded; on a loopback server that " +
			"is one viewer ending another's playback")
	}
}

// A channel cannot be caught by it. Live sessions record a negated channel id,
// so they never match a library item — asserted rather than assumed, because
// the two numbering schemes overlap and only the sign separates them.
func TestSupersedingAnItemCannotTakeAChannel(t *testing.T) {
	m := idleManager(t)
	m.sessions["chan"] = hlsAt("chan", -37106, "u_3f9", 0)

	m.supersedeHLS("u_3f9", 37106)

	if m.Session("chan") == nil {
		t.Error("superseding item 37106 stopped channel 37106")
	}
}

/*
 * An unwatched session never outlives a watched one.
 *
 * The rule is "abandoned sessions go sooner", and returning the unread timeout
 * flatly inverts it whenever IdleTimeout is the smaller number — the session
 * nobody ever attached to becomes the most durable thing on the server. Found
 * by a test that lowers IdleTimeout to reap quickly and then could not reap.
 */
func TestAnUnwatchedSessionNeverOutlivesAWatchedOne(t *testing.T) {
	m := idleManager(t)
	m.IdleTimeout = 50 * time.Millisecond

	unread := &Session{ID: "unread", ItemID: 37106, Output: HLS}
	watched := &Session{ID: "watched", ItemID: 37106, Output: HLS}
	watched.NoteServed(1 << 20)

	if m.idleLimit(unread) > m.idleLimit(watched) {
		t.Errorf("unread %v outlives watched %v", m.idleLimit(unread), m.idleLimit(watched))
	}
}
