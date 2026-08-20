package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"lancast/internal/auth"
	"lancast/internal/identity"
	"lancast/internal/peer"
	"lancast/internal/store"
)

/*
 * A second account, signed in for real.
 *
 * Through the login route rather than by forging a session row: what is under
 * test here is an authorization boundary, and a hand-made session is the one
 * thing that could make a boundary look right while the real path disagrees.
 */
type memberSession struct {
	id     string
	cookie *http.Cookie
}

func (h *harness) member(t *testing.T, name, password string) memberSession {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	u, err := h.st.CreateUser(context.Background(), "", name, hash, store.RoleMember)
	if err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "POST", "/api/auth/login", map[string]any{
		"username": name, "password": password,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member login: status %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return memberSession{id: u.ID, cookie: c}
		}
	}
	t.Fatal("member login returned no session cookie")
	return memberSession{}
}

func (h *harness) asUser(t *testing.T, m memberSession, method, path string, body any) *http.Response {
	t.Helper()
	saved := h.cookie
	h.cookie = m.cookie
	defer func() { h.cookie = saved }()
	return h.authed(t, method, path, body)
}

// anotherServer is a second identity, standing in for Georgia's machine.
func anotherServer(t *testing.T) identity.Identity {
	t.Helper()
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func inviteFrom(t *testing.T, id identity.Identity, name string) string {
	t.Helper()
	s, err := peer.Encode(id, name, []string{"10.121.240.21:8080"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddPeerFromAnInvite(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	other := anotherServer(t)

	var created struct {
		Fingerprint string   `json:"fingerprint"`
		Display     string   `json:"fingerprint_display"`
		Name        string   `json:"name"`
		State       string   `json:"state"`
		Addrs       []string `json:"addrs"`
	}
	resp := h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, other, "Utopia"),
	})
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	decode(t, resp, &created)

	if created.Fingerprint != other.Fingerprint() {
		t.Errorf("fingerprint = %q, want the far server's", created.Fingerprint)
	}
	if identity.Normalize(created.Display) != created.Fingerprint {
		t.Error("the display fingerprint does not normalize to the canonical one")
	}
	if created.Name != "Utopia" {
		t.Errorf("name = %q", created.Name)
	}
	// Accepting an invite is not a pairing. ADR 0044 makes that mutual, and
	// only the transport can find out whether the other side holds us.
	if created.State != "added" {
		t.Errorf("state = %q, want added; this route cannot create a pairing", created.State)
	}
	if len(created.Addrs) != 1 {
		t.Errorf("addrs = %v", created.Addrs)
	}
}

func TestPeerAppearsInTheList(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	other := anotherServer(t)

	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, other, "Utopia"),
	}).Body.Close()

	var got struct {
		Peers []struct {
			Fingerprint string `json:"fingerprint"`
			Name        string `json:"name"`
		} `json:"peers"`
	}
	decode(t, h.authed(t, "GET", "/api/peers", nil), &got)
	if len(got.Peers) != 1 || got.Peers[0].Fingerprint != other.Fingerprint() {
		t.Fatalf("peers = %v", got.Peers)
	}
}

// Pasting your own invite would produce a peer that is this server: a row that
// looks reachable, answers its own reachability check, and appears in its own
// list. Somebody will do it while testing.
func TestCannotPairWithYourself(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var mine struct {
		Invite string `json:"invite"`
	}
	decode(t, h.authed(t, "GET", "/api/peers/invite", nil), &mine)
	/*
	 * Assert we actually got an invite before asserting it is refused.
	 *
	 * Written after this test passed against a broken route: the invite handler
	 * was 500ing, `mine.Invite` was empty, and an empty invite is refused as
	 * malformed — a 400 for the wrong reason, which is indistinguishable from
	 * the right one if all you check is the status.
	 */
	if mine.Invite == "" {
		t.Fatal("this server produced no invite, so the self-check is untested")
	}

	resp := h.authed(t, "POST", "/api/peers", map[string]any{"invite": mine.Invite})
	if resp.StatusCode != http.StatusBadRequest {
		resp.Body.Close()
		t.Fatalf("status = %d, want 400 when pasting our own invite", resp.StatusCode)
	}
	// And refused as *ours*, not merely as unreadable.
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, resp, &e)
	if e.Error.Code != "self" {
		t.Errorf("code = %q, want \"self\"", e.Error.Code)
	}

	// Nothing was recorded, either.
	var got struct {
		Peers []any `json:"peers"`
	}
	decode(t, h.authed(t, "GET", "/api/peers", nil), &got)
	if len(got.Peers) != 0 {
		t.Errorf("%d peers recorded after pasting our own invite", len(got.Peers))
	}
}

// The invite this server hands out has to be parseable by the thing that reads
// invites, and has to carry this server's own fingerprint.
func TestOurInviteRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var mine struct {
		Invite      string   `json:"invite"`
		Fingerprint string   `json:"fingerprint"`
		Addrs       []string `json:"addrs"`
	}
	resp := h.authed(t, "GET", "/api/peers/invite", nil)
	if resp.StatusCode == http.StatusConflict {
		// A machine with no non-loopback address cannot introduce itself, and
		// says so. That is a legitimate outcome on a CI runner, not a failure.
		resp.Body.Close()
		t.Skip("this machine has no address a peer could reach")
	}
	decode(t, resp, &mine)

	parsed, err := peer.Parse(mine.Invite)
	if err != nil {
		t.Fatalf("our own invite does not parse: %v", err)
	}
	if parsed.Fingerprint != mine.Fingerprint {
		t.Errorf("invite carries %q, route reports %q", parsed.Fingerprint, mine.Fingerprint)
	}
	if len(parsed.Addrs) == 0 {
		t.Error("invite carries no address")
	}
}

func TestBadInvitesAreRefusedWithSomethingUseful(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	for name, invite := range map[string]string{
		"empty":       "",
		"not ours":    "syncthing://ABCDEF",
		"truncated":   "lancast-invite-v1:abc",
		"nonsense":    "hello",
		"missing key": "lancast-invite-v1:",
	} {
		t.Run(name, func(t *testing.T) {
			resp := h.authed(t, "POST", "/api/peers", map[string]any{"invite": invite})
			if resp.StatusCode != http.StatusBadRequest {
				resp.Body.Close()
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			decode(t, resp, &e)
			// The parser writes for the person holding the paste. A message
			// that says nothing is the failure this is guarding against.
			if strings.TrimSpace(e.Error.Message) == "" {
				t.Error("refused the invite without saying why")
			}
		})
	}
}

/*
 * Removal is the revocation mechanism, so it is asserted end to end rather than
 * trusted: the peer goes, and so does everything the pairing carried.
 */
func TestRemovingAPeerTakesTheRosterWithIt(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	other := anotherServer(t)
	ctx := context.Background()

	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, other, "Utopia"),
	}).Body.Close()

	if err := h.st.ReplaceRemotePeople(ctx, other.Fingerprint(), []store.RemotePerson{
		{ID: "u1", Name: "Georgia"},
	}); err != nil {
		t.Fatal(err)
	}

	resp := h.authed(t, "DELETE", "/api/peers/"+other.Fingerprint(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	people, err := h.st.RemotePeople(ctx, other.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("%d remote people survived unpairing", len(people))
	}
}

// A fingerprint copied off a screen arrives grouped. Removing must remove the
// peer somebody is looking at, not report that no such peer exists.
func TestRemovingAcceptsAGroupedFingerprint(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	other := anotherServer(t)

	h.authed(t, "POST", "/api/peers", map[string]any{
		"invite": inviteFrom(t, other, "Utopia"),
	}).Body.Close()

	resp := h.authed(t, "DELETE", "/api/peers/"+other.Grouped(), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a grouped fingerprint", resp.StatusCode)
	}
}

func TestRemovingAnUnknownPeerIs404(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	resp := h.authed(t, "DELETE", "/api/peers/"+strings.Repeat("A", 52), nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

/*
 * Pairing is admin; it opens a network relationship for the whole server.
 *
 * Gated on the server rather than hidden in the client, which is the rule
 * ADR 0015 set for every operational power and the reason a member cannot pair
 * a machine they merely have an account on.
 */
func TestPairingIsAdminOnly(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	member := h.member(t, "Georgia", "another good password")

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/peers"},
		{"POST", "/api/peers"},
		{"GET", "/api/peers/invite"},
		{"DELETE", "/api/peers/" + strings.Repeat("A", 52)},
	} {
		resp := h.asUser(t, member, tc.method, tc.path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s: status = %d, want 403 for a member", tc.method, tc.path, resp.StatusCode)
		}
	}
}

/*
 * Peer visibility is the opposite: personal, self-service, and reported back on
 * auth status so the control can show what the server holds.
 *
 * That last part is here because the equivalent for share_activity shipped
 * without it and rendered off for somebody who had opted in. Once is enough.
 */
func TestPeerVisibilityIsPersonalAndReadsBack(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")

	var st struct {
		User struct {
			Visible bool `json:"visible_to_peers"`
		} `json:"user"`
	}
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if st.User.Visible {
		t.Fatal("a new account is visible to peers; the default must be off")
	}

	resp := h.authed(t, "PUT", "/api/profile/peer-visibility", map[string]any{"visible": true})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	st.User.Visible = false
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if !st.User.Visible {
		t.Error("after opting in, auth status still reports invisible")
	}

	// And back off, because a switch that cannot be seen to turn off is the
	// same bug in the other direction.
	h.authed(t, "PUT", "/api/profile/peer-visibility", map[string]any{"visible": false}).Body.Close()
	st.User.Visible = true
	decode(t, h.authed(t, "GET", "/api/auth/status", nil), &st)
	if st.User.Visible {
		t.Error("after opting out, auth status still reports visible")
	}
}

// A member sets their own visibility. This is not an administrative power, and
// there is no shape of the call that sets somebody else's.
func TestAMemberMaySetTheirOwnVisibility(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	member := h.member(t, "Georgia", "another good password")

	resp := h.asUser(t, member, "PUT", "/api/profile/peer-visibility", map[string]any{"visible": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a member setting their own", resp.StatusCode)
	}

	on, err := h.st.VisibleToPeers(context.Background(), member.id)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Error("the member's own visibility was not set")
	}
}
