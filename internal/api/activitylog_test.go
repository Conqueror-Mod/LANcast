package api

import (
	"strings"
	"testing"

	"lancast/internal/together"
)

/*
 * Watch Together's life, in the log: opened, joined (once, however often a
 * refresh rejoins), and closed when the host leaves.
 */
func TestWatchTogetherIsLogged(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	item := h.addFile(t, "Shrek (2001).mkv", []byte("x"))
	buf := h.captureLog()

	var room together.Session
	decode(t, h.authed(t, "POST", "/api/together", map[string]any{"item_id": item}), &room)
	if !strings.Contains(buf.String(), `msg="watch together: room opened" room=`+room.ID) {
		t.Fatalf("opening a room was not logged:\n%s", buf)
	}
	if !strings.Contains(buf.String(), `title="Shrek`) {
		t.Errorf("the room line does not name the film:\n%s", buf)
	}

	for range 3 { // a refresh, a dropped connection, a second tab
		resp := h.authed(t, "POST", "/api/together/"+room.ID+"/join", nil)
		resp.Body.Close()
	}
	if n := countLines(buf, `msg="watch together: joined"`); n != 1 {
		t.Errorf("three rejoins logged %d lines, want 1:\n%s", n, buf)
	}

	resp := h.authed(t, "DELETE", "/api/together/"+room.ID, nil)
	resp.Body.Close()
	if !strings.Contains(buf.String(), `msg="watch together: room closed" room=`+room.ID) {
		t.Errorf("the host leaving did not log the room closing:\n%s", buf)
	}
}

// What the sweep does with nobody pressing anything reaches the log through
// the hook the server installs.
func TestTheSweepReachesTheLog(t *testing.T) {
	h := newHarness(t)
	buf := h.captureLog()
	h.srvAPI.together.OnSweep(together.Event{Kind: together.EventRoomEnded, RoomID: "r1", ItemID: 5})
	if !strings.Contains(buf.String(), `msg="watch together: room ended: its host went quiet" room=r1`) {
		t.Fatalf("a room ended by the sweep was not logged:\n%s", buf)
	}
}

// Every audited action also lands in the log's timeline, with its summary.
func TestAuditedActionsReachTheLog(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	buf := h.captureLog()

	resp := h.authed(t, "POST", "/api/backups", nil)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("creating a backup: %d", resp.StatusCode)
	}
	if !strings.Contains(buf.String(), "action=backup.create") || !strings.Contains(buf.String(), "actor="+testUser) {
		t.Fatalf("an audited action did not reach the log with its action and actor:\n%s", buf)
	}
}
