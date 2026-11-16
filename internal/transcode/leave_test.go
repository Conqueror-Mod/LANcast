package transcode

import "testing"

// The Fifth Element, as it was seen: the viewer left and the encode ran on to
// idle_seconds=614. Leaving now ends it.
func TestLeavingStopsTheEncodeStillRunning(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	growingSession(t, m, "encoding", 7022, "u1")

	m.Leave("u1", 7022)

	if m.Session("encoding") != nil {
		t.Error("an HLS encode kept running after its viewer left")
	}
}

// A finished conversion costs nothing and is what coming back reuses.
func TestLeavingSparesAFinishedConversion(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	finishedSession(t, m, "done", 7022, "u1")

	m.Leave("u1", 7022)

	if m.Session("done") == nil {
		t.Error("leaving destroyed a finished conversion; coming back would encode it again")
	}
}

// Leaving a film never ends somebody else's viewing of it, or the same
// viewer's other title.
func TestLeavingTouchesOnlyThisViewersItem(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	growingSession(t, m, "theirs", 7022, "u2")
	growingSession(t, m, "other", 7023, "u1")

	m.Leave("u1", 7022)

	if m.Session("theirs") == nil {
		t.Error("one viewer leaving ended another viewer's film")
	}
	if m.Session("other") == nil {
		t.Error("leaving one title ended a different one")
	}
}

// The unconfigured loopback state, where every request is anonymous, is not
// collapsed: "" is everybody, not one viewer.
func TestLeavingAsNobodyStopsNothing(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	growingSession(t, m, "anon", 7022, "")

	m.Leave("", 7022)

	if m.Session("anon") == nil {
		t.Error("an anonymous leave stopped an anonymous session")
	}
}

// Deleting the file stops everything reading it, whoever is watching and
// finished or not; nothing else.
func TestStoppingAnItemStopsEveryReaderOfIt(t *testing.T) {
	m := NewManager(t.TempDir(), quiet())
	growingSession(t, m, "a", 7022, "u1")
	finishedSession(t, m, "b", 7022, "u2")
	growingSession(t, m, "c", 7023, "u1")

	if n := m.StopItem(7022); n != 2 {
		t.Errorf("StopItem stopped %d sessions, want 2", n)
	}
	if m.Session("a") != nil || m.Session("b") != nil {
		t.Error("a session still holds the file being deleted")
	}
	if m.Session("c") == nil {
		t.Error("deleting one item stopped another")
	}
}
