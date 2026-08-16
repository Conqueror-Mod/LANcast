package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * The invariant worth a test more than any other in this file: the server must
 * always have somebody who can administer it.
 *
 * Losing the last admin is not recoverable through the API — the only way back
 * is `lancastd reset-auth` on the machine itself, which a remote operator does
 * not have. So the refusal lives in the store, inside a transaction with the
 * count, and this asserts the handler surfaces it as something a client can act
 * on rather than a 500.
 */
func TestTheLastAdminCannotBeDemoted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	// The setup account is the only admin.
	admin, err := h.st.UserByName(ctx, testUser)
	if err != nil {
		t.Fatal(err)
	}

	wantError(t, h.authed(t, "PATCH", "/api/users/"+admin.ID,
		map[string]any{"role": "member"}),
		http.StatusConflict, "last_admin")

	// And the account is untouched, not half-changed.
	after, err := h.st.UserByID(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Role != store.RoleAdmin {
		t.Errorf("role = %q after a refused demotion, want admin", after.Role)
	}
}

// With a second admin the demotion is ordinary.
func TestDemotionIsAllowedWhenAnotherAdminRemains(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	first, err := h.st.CreateUser(ctx, "", "admin-two", "hash", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	var got managedUserView
	resp := h.authed(t, "PATCH", "/api/users/"+first.ID, map[string]any{"role": "member"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &got)
	if got.Role != store.RoleMember {
		t.Errorf("role = %q, want member", got.Role)
	}
}

/*
 * A rename keeps the id, which is what makes it a rename.
 *
 * Sessions, watch history, ratings and playlist membership all hang off the id.
 * A "rename" that minted a new one would silently orphan every one of them, and
 * the damage would only show up as a profile that had forgotten everything.
 */
func TestRenameKeepsTheIdAndTheHistory(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	u, err := h.st.CreateUser(ctx, "", "before", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	item := h.addFile(t, "Arrival.mkv", []byte("x"))
	if err := h.st.SaveProgress(ctx, item, u.ID, 1000, false); err != nil {
		t.Fatal(err)
	}

	var got managedUserView
	decode(t, h.authed(t, "PATCH", "/api/users/"+u.ID, map[string]any{"name": "after"}), &got)
	if got.ID != u.ID {
		t.Errorf("id = %q, want it unchanged (%q)", got.ID, u.ID)
	}
	if got.Name != "after" {
		t.Errorf("name = %q, want after", got.Name)
	}

	entries, err := h.st.History(ctx, u.ID, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("history has %d entries after a rename, want 1 — the rename orphaned it",
			len(entries))
	}
}

func TestRenameRefusesADuplicate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")

	if _, err := h.st.CreateUser(ctx, "", "taken", "hash", store.RoleMember); err != nil {
		t.Fatal(err)
	}
	other, err := h.st.CreateUser(ctx, "", "other", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	wantError(t, h.authed(t, "PATCH", "/api/users/"+other.ID, map[string]any{"name": "taken"}),
		http.StatusConflict, "duplicate")
}

func TestUserPatchValidation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.secure(t, "a good long password")
	u, err := h.st.CreateUser(ctx, "", "someone", "hash", store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		body map[string]any
	}{
		// A no-op patch is a client bug: it looks like a change that did not
		// stick, which is the hardest kind to report.
		{"nothing to change", map[string]any{}},
		{"empty name", map[string]any{"name": "   "}},
		{"unknown role", map[string]any{"role": "owner"}},
		{"control characters", map[string]any{"name": "badname"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, h.authed(t, "PATCH", "/api/users/"+u.ID, tc.body),
				http.StatusBadRequest, "bad_request")
		})
	}
}

func TestPatchUnknownUser(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	wantError(t, h.authed(t, "PATCH", "/api/users/u_nope", map[string]any{"name": "x"}),
		http.StatusNotFound, "not_found")
}

// On an unconfigured loopback server there is no account to edit. Saying so
// beats succeeding against a row that does not exist.
func TestProfilePatchWithoutAnAccount(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PATCH", "/api/profile", map[string]any{"name": "Chris"}),
		http.StatusConflict, "no_account")
}
