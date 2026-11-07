package api

import (
	"net/http"
	"testing"

	"lancast/internal/auth"
	"lancast/internal/remote"
)

/*
 * Play on another of your own devices (ADR 0117).
 *
 * The test that matters most is the one about ownership: a member must not be
 * able to see, or send a film to, another account's screen, and the refusal
 * must look exactly like a player that is not there.
 */

const tvID = "tv-0123456789"

func playersOf(t *testing.T, resp *http.Response) []remote.Player {
	t.Helper()
	var body struct {
		Players []remote.Player `json:"players"`
	}
	decode(t, resp, &body)
	return body.Players
}

func TestAPlayerReportsAndIsListed(t *testing.T) {
	h := newHarness(t)

	var p remote.Player
	decode(t, h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{
		"name": "Living room", "kind": "desktop", "item_id": 7,
		"position_ms": 61000, "duration_ms": 7200000, "paused": false, "volume": 0.8,
	}), &p)
	if p.ID != tvID || p.Name != "Living room" || p.ItemID != 7 || p.PositionMS != 61000 {
		t.Fatalf("report answered %+v", p)
	}

	got := playersOf(t, h.do(t, "GET", "/api/remote/players", nil))
	if len(got) != 1 || got[0].ID != tvID || got[0].Kind != "desktop" || got[0].Volume != 0.8 {
		t.Fatalf("listed %+v", got)
	}
}

func TestACommandRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}).Body.Close()
	h.do(t, "PUT", "/api/remote/players/phone-01234567", map[string]any{"name": "Phone"}).Body.Close()

	resp := h.do(t, "POST", "/api/remote/players/"+tvID+"/commands", map[string]any{
		"action": "play", "item_id": 12, "queue": []int64{12, 13}, "position_ms": 90000,
		"from_player": "phone-01234567",
		// Whatever the body claims, the sender's name comes from its player.
		"from": "Somebody else",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("send = %d, want 202", resp.StatusCode)
	}

	// The next report says there is something waiting, so a window that
	// missed the event still finds it.
	var p remote.Player
	decode(t, h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}), &p)
	if p.Pending != 1 {
		t.Fatalf("pending = %d, want 1", p.Pending)
	}

	var body struct {
		Commands []remote.Command `json:"commands"`
	}
	decode(t, h.do(t, "GET", "/api/remote/players/"+tvID+"/commands", nil), &body)
	if len(body.Commands) != 1 {
		t.Fatalf("took %+v", body.Commands)
	}
	c := body.Commands[0]
	if c.Action != "play" || c.ItemID != 12 || len(c.Queue) != 2 || c.PositionMS == nil || *c.PositionMS != 90000 {
		t.Fatalf("command = %+v", c)
	}
	if c.From != "Phone" {
		t.Fatalf("from = %q, want the sending player's own name", c.From)
	}

	decode(t, h.do(t, "GET", "/api/remote/players/"+tvID+"/commands", nil), &body)
	if len(body.Commands) != 0 {
		t.Fatalf("taken twice: %+v", body.Commands)
	}
}

func TestACommandToNoPlayerIsNotFound(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/remote/players/"+tvID+"/commands",
		map[string]any{"action": "pause"}), http.StatusNotFound, "not_found")
	wantError(t, h.do(t, "GET", "/api/remote/players/"+tvID+"/commands", nil),
		http.StatusNotFound, "not_found")
}

func TestABadCommandIsRefused(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}).Body.Close()
	for _, body := range []map[string]any{
		{"action": "play"},
		{"action": "seek"},
		{"action": "volume", "volume": 2},
		{"action": "explode"},
	} {
		wantError(t, h.do(t, "POST", "/api/remote/players/"+tvID+"/commands", body),
			http.StatusBadRequest, "bad_request")
	}
}

func TestAPlayerIDMustLookLikeOne(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"short", "has%20space0000", "x234567890123456789012345678901234567890123456789012345678901234567"} {
		wantError(t, h.do(t, "PUT", "/api/remote/players/"+id, map[string]any{"name": "TV"}),
			http.StatusBadRequest, "bad_request")
	}
}

func TestRemovingAPlayer(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}).Body.Close()
	resp := h.do(t, "DELETE", "/api/remote/players/"+tvID, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	if got := playersOf(t, h.do(t, "GET", "/api/remote/players", nil)); len(got) != 0 {
		t.Fatalf("still listed: %+v", got)
	}
}

// The ownership rule. Another account's player is not listed, cannot be sent
// a command, and cannot have its commands taken, and every refusal is the
// answer a missing player gets.
func TestAnotherAccountsPlayerIsInvisible(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	member := h.addMember(t, "housemate", "correct horse battery staple")

	resp := h.authed(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "Owner's TV"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner's report = %d", resp.StatusCode)
	}

	if got := playersOf(t, h.doAs(t, member, "GET", "/api/remote/players", nil)); len(got) != 0 {
		t.Fatalf("the member sees %+v", got)
	}
	wantError(t, h.doAs(t, member, "POST", "/api/remote/players/"+tvID+"/commands",
		map[string]any{"action": "stop"}), http.StatusNotFound, "not_found")

	// The same id reported by the member is the member's own player and
	// leaves the owner's alone.
	h.doAs(t, member, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "Mine now"}).Body.Close()
	got := playersOf(t, h.authed(t, "GET", "/api/remote/players", nil))
	if len(got) != 1 || got[0].Name != "Owner's TV" {
		t.Fatalf("owner now sees %+v", got)
	}
	// And a command the member sends to "their" player never reaches the
	// owner's window.
	h.doAs(t, member, "POST", "/api/remote/players/"+tvID+"/commands",
		map[string]any{"action": "stop"}).Body.Close()
	var body struct {
		Commands []remote.Command `json:"commands"`
	}
	decode(t, h.authed(t, "GET", "/api/remote/players/"+tvID+"/commands", nil), &body)
	if len(body.Commands) != 0 {
		t.Fatalf("the member's command reached the owner: %+v", body.Commands)
	}
}

// A player learns of a command from the event stream, and a controller learns
// of a new player from it. Without these the feature is a poll.
func TestCommandsAndPlayersReachAnOpenWindow(t *testing.T) {
	h := newHarness(t)
	r := stream(t, h)

	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}).Body.Close()
	if topics := nextTopics(t, r); !has(topics, TopicPlayers) {
		t.Fatalf("a new player published %v", topics)
	}

	h.do(t, "POST", "/api/remote/players/"+tvID+"/commands", map[string]any{"action": "pause"}).Body.Close()
	if topics := nextTopics(t, r); !has(topics, TopicRemote) {
		t.Fatalf("a command published %v", topics)
	}
}

// A report that only moves the position is not worth a refetch in every
// window (the package tests prove that); one that pauses is.
func TestAPauseReachesAnOpenWindow(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV", "item_id": 3}).Body.Close()
	r := stream(t, h)

	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV", "item_id": 3, "paused": true}).Body.Close()
	if topics := nextTopics(t, r); !has(topics, TopicPlayers) {
		t.Fatalf("pausing published %v", topics)
	}
}

// A phone used only as a remote is not a player, and the window it sends to
// should still say which device it was: the name the phone gave itself, or
// failing that the description Authorized devices shows for its browser.
func TestASenderThatIsNotAPlayerIsStillNamed(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/remote/players/"+tvID, map[string]any{"name": "TV"}).Body.Close()

	take := func() remote.Command {
		t.Helper()
		var body struct {
			Commands []remote.Command `json:"commands"`
		}
		decode(t, h.do(t, "GET", "/api/remote/players/"+tvID+"/commands", nil), &body)
		if len(body.Commands) != 1 {
			t.Fatalf("took %+v", body.Commands)
		}
		return body.Commands[0]
	}

	h.do(t, "POST", "/api/remote/players/"+tvID+"/commands",
		map[string]any{"action": "pause", "from_name": "  Chris's phone\n"}).Body.Close()
	if got := take().From; got != "Chris's phone" {
		t.Fatalf("from = %q, want the name the device gave itself", got)
	}

	// A from_player that is not live falls through to the name, not to "".
	h.do(t, "POST", "/api/remote/players/"+tvID+"/commands",
		map[string]any{"action": "pause", "from_player": "gone-0123456789", "from_name": "Tablet"}).Body.Close()
	if got := take().From; got != "Tablet" {
		t.Fatalf("from = %q, want Tablet", got)
	}

	// Nothing given: the server's own description of the browser.
	h.do(t, "POST", "/api/remote/players/"+tvID+"/commands", map[string]any{"action": "pause"}).Body.Close()
	if got := take().From; got == "" {
		t.Fatal("an unnamed sender came through with no name at all")
	}
}

func TestSenderNameFromTheBrowser(t *testing.T) {
	m := remote.New()
	ua := "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36"
	if got := senderName(m, "u", "", "", ua); got != auth.DeviceName(ua) {
		t.Fatalf("got %q, want %q", got, auth.DeviceName(ua))
	}
	m.Report("u", "phone-01234567", remote.Report{Name: "Pocket"})
	if got := senderName(m, "u", "phone-01234567", "Ignored", ua); got != "Pocket" {
		t.Fatalf("a live player is named %q, want its own name", got)
	}
	// Another account's player id names nothing here.
	if got := senderName(m, "v", "phone-01234567", "", ua); got == "Pocket" {
		t.Fatal("another account's player lent its name")
	}
}
