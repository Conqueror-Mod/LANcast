package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * ADR 0035 at the HTTP boundary.
 *
 * The store has its own tests for the filter; these assert the *shape of the
 * refusal*, which is a separate decision: not sharing returns an empty list
 * rather than a 403, because a 403 confirms there is something being withheld.
 * What somebody watched is private, and so is how much of it there is.
 */

func TestPeopleListsEverybodyElse(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	if _, err := h.st.CreateUser(ctx, "", "housemate", "hash", store.RoleMember); err != nil {
		t.Fatal(err)
	}

	var body struct {
		People []store.Person `json:"people"`
	}
	decode(t, h.authed(t, "GET", "/api/people", nil), &body)
	if len(body.People) != 1 || body.People[0].Name != "housemate" {
		t.Fatalf("people = %+v, want just the other account", body.People)
	}
	// Reported even when false, so a page can say "has not shared" rather than
	// showing an empty list that reads as "watches nothing".
	if body.People[0].Sharing {
		t.Error("a new account is reported as sharing")
	}
}

// Not sharing is indistinguishable from having watched nothing, from outside.
func TestActivityOfANonSharerIsEmptyNotForbidden(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	other, err := h.st.CreateUser(ctx, "", "quiet", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	item := h.addFile(t, "Arrival.mkv", []byte("x"))
	if err := h.st.SaveProgress(ctx, item, other.ID, 5000, true); err != nil {
		t.Fatal(err)
	}

	resp := h.authed(t, "GET", "/api/people/"+other.ID+"/activity", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a 403 would confirm there is something to withhold",
			resp.StatusCode)
	}
	var body struct {
		Activity []store.HistoryEntry `json:"activity"`
	}
	decode(t, resp, &body)
	if len(body.Activity) != 0 {
		t.Errorf("activity = %d entries for an account that never opted in", len(body.Activity))
	}
}

func TestActivityAppearsAfterOptingIn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	other, err := h.st.CreateUser(ctx, "", "sharer", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	item := h.addFile(t, "Arrival.mkv", []byte("x"))
	if err := h.st.SaveProgress(ctx, item, other.ID, 5000, true); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetShareActivity(ctx, other.ID, true); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Activity []store.HistoryEntry `json:"activity"`
	}
	decode(t, h.authed(t, "GET", "/api/people/"+other.ID+"/activity", nil), &body)
	if len(body.Activity) != 1 {
		t.Fatalf("activity = %d, want 1 after opting in", len(body.Activity))
	}
	if body.Activity[0].PositionMS != 0 {
		t.Errorf("position_ms = %d; ADR 0035 excludes resume positions from sharing",
			body.Activity[0].PositionMS)
	}
}

// The switch is the caller's own. There is deliberately no admin variant: a
// switch somebody else can flip is not consent.
func TestSharingIsSetByTheAccountItself(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	resp := h.authed(t, "PUT", "/api/profile/sharing", map[string]any{"share": true})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	me, err := h.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}
	on, err := h.st.SharesActivity(ctx, me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("the switch did not take")
	}

	// And back off again — retroactively, which is what makes it a switch.
	h.authed(t, "PUT", "/api/profile/sharing", map[string]any{"share": false}).Body.Close()
	if on, _ := h.st.SharesActivity(ctx, me.ID); on {
		t.Error("sharing could not be turned off")
	}
}

/*
 * The account can read its own sharing choice back.
 *
 * This is the round trip TestSharingIsSetByTheAccountItself does not make. That
 * test puts the setting and then asserts the *database*, which passed happily
 * while the only thing a client could actually see said otherwise: `/api/people`
 * excludes the caller by design, nothing else reported the value, and the
 * settings toggle fell back to off on every mount. Somebody who had opted in was
 * shown a control saying they had not — the worst direction for a privacy
 * control to be wrong in.
 *
 * So this asserts the wire, not the column.
 */
func TestAuthStatusReportsMyOwnSharing(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var st struct {
		User struct {
			Sharing bool `json:"sharing"`
		} `json:"user"`
	}

	// Off is the default (ADR 0035) and must be reported as such rather than
	// simply omitted.
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if st.User.Sharing {
		t.Fatal("a new account reports sharing on; the default is off")
	}

	h.authed(t, "PUT", "/api/profile/sharing", map[string]any{"share": true}).Body.Close()

	st.User.Sharing = false
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if !st.User.Sharing {
		t.Error("after opting in, auth status still reports sharing off")
	}

	// And back, because a switch that cannot be seen to turn off is the same
	// bug in the other direction.
	h.authed(t, "PUT", "/api/profile/sharing", map[string]any{"share": false}).Body.Close()

	st.User.Sharing = true
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if st.User.Sharing {
		t.Error("after opting out, auth status still reports sharing on")
	}
}
