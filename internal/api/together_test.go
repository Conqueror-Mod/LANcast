package api

import (
	"net/http"
	"testing"

	"lancast/internal/together"
)

func TestTogetherRoundTrip(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "Arrival.mkv", []byte("x"))

	var room together.Session
	resp := h.do(t, "POST", "/api/together", map[string]any{"item_id": item})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	decode(t, resp, &room)
	if room.ID == "" || room.ItemID != item {
		t.Fatalf("session = %+v, want a room around item %d", room, item)
	}

	// The host reports; a poll returns what the host said. That round trip is
	// the whole feature: the server holds the truth and clients read it.
	var after together.Session
	decode(t, h.do(t, "PUT", "/api/together/"+room.ID,
		map[string]any{"position_ms": 61000, "paused": true}), &after)
	if after.PositionMS != 61000 || !after.Paused {
		t.Errorf("session = %+v, want position 61000 and paused", after)
	}

	var polled together.Session
	decode(t, h.do(t, "GET", "/api/together/"+room.ID, nil), &polled)
	if polled.PositionMS != 61000 {
		t.Errorf("polled position = %d, want 61000", polled.PositionMS)
	}

	if listed := h.togetherList(t); len(listed) != 1 {
		t.Errorf("sessions = %d, want 1", len(listed))
	}
}

func (h *harness) togetherList(t *testing.T) []together.Session {
	t.Helper()
	var body struct {
		Sessions []together.Session `json:"sessions"`
	}
	decode(t, h.do(t, "GET", "/api/together", nil), &body)
	return body.Sessions
}

// A room around an item that does not exist would leave everybody who joined
// staring at a player that cannot load, with nothing to say why.
func TestTogetherRefusesAnUnknownItem(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/together", map[string]any{"item_id": 9999}),
		http.StatusNotFound, "not_found")
}

func TestTogetherRequiresAnItem(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/together", map[string]any{}),
		http.StatusBadRequest, "bad_request")
}

// Once a room has ended, following it is a 404 — which is the client's cue to
// stop, and is why the code matters more than the status.
func TestPollingAnEndedRoom(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "Arrival.mkv", []byte("x"))

	var room together.Session
	decode(t, h.do(t, "POST", "/api/together", map[string]any{"item_id": item}), &room)

	resp := h.do(t, "DELETE", "/api/together/"+room.ID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("leave = %d, want 204", resp.StatusCode)
	}

	wantError(t, h.do(t, "GET", "/api/together/"+room.ID, nil),
		http.StatusNotFound, "not_found")
}

func TestTogetherUnknownRoom(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/together/nope", nil),
		http.StatusNotFound, "not_found")
	wantError(t, h.do(t, "PUT", "/api/together/nope",
		map[string]any{"position_ms": 1}), http.StatusNotFound, "not_found")
}

/*
 * The host rule, at the HTTP boundary.
 *
 * The manager enforces it and has its own test; this asserts the *shape* of the
 * refusal, because a client uses it to decide whether to show transport
 * controls at all. A 500 here would read as a broken server rather than as a
 * control this person does not have.
 */
func TestOnlyTheHostMayDriveOverHTTP(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	item := h.addFile(t, "Arrival.mkv", []byte("x"))

	var room together.Session
	decode(t, h.authed(t, "POST", "/api/together", map[string]any{"item_id": item}), &room)

	member := h.addMember(t, "guest", "another long password")
	joined := h.doAs(t, member, "POST", "/api/together/"+room.ID+"/join", nil)
	joined.Body.Close()
	if joined.StatusCode != http.StatusOK {
		t.Fatalf("join = %d, want 200", joined.StatusCode)
	}

	wantError(t, h.doAs(t, member, "PUT", "/api/together/"+room.ID,
		map[string]any{"position_ms": 5000}),
		http.StatusForbidden, "forbidden")

	// The guest can still follow — refusing to drive is not refusing to watch.
	polled := h.doAs(t, member, "GET", "/api/together/"+room.ID, nil)
	polled.Body.Close()
	if polled.StatusCode != http.StatusOK {
		t.Errorf("guest poll = %d, want 200", polled.StatusCode)
	}
}

// Watching something with the people you live with is not an administrative
// act, so a member may open a room of their own.
func TestAMemberMayHostARoom(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	item := h.addFile(t, "Arrival.mkv", []byte("x"))
	member := h.addMember(t, "guest", "another long password")

	resp := h.doAs(t, member, "POST", "/api/together", map[string]any{"item_id": item})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201 — hosting is not an admin power", resp.StatusCode)
	}
}
