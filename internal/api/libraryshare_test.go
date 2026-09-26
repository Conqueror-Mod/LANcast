package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * The host choosing what a paired server may see (ADR 0071 §1, §6).
 *
 * The store tests next door prove the grant behaves; these prove the surface
 * a person actually touches refuses what it should — including a ceiling that
 * would do nothing, which is the case the ADR singles out as worse than no
 * control at all.
 */

type shareFixture struct {
	h       *harness
	peerFP  string
	music   *store.Library
	videoID int64
}

func newShareFixture(t *testing.T) shareFixture {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "a good long password")
	georgia := anotherServer(t)
	pairedPeer(t, h, georgia, "Utopia")

	music, err := h.st.CreateLibrary(context.Background(), "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return shareFixture{
		h: h, peerFP: georgia.Fingerprint(), music: music, videoID: h.lib.ID,
	}
}

type shareRow struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Shared          bool   `json:"shared"`
	Ceiling         string `json:"ceiling"`
	SupportsCeiling bool   `json:"supports_ceiling"`
	Unrated         int    `json:"unrated"`
	Total           int    `json:"total"`
}

func (f shareFixture) list(t *testing.T) map[int64]shareRow {
	t.Helper()
	var got struct {
		Libraries []shareRow `json:"libraries"`
	}
	decode(t, f.h.authed(t, "GET", "/api/peers/"+f.peerFP+"/shares", nil), &got)
	out := map[int64]shareRow{}
	for _, r := range got.Libraries {
		out[r.ID] = r
	}
	return out
}

// Pairing grants nothing (ADR 0071 §1): every library starts unshared.
func TestSharesStartEmpty(t *testing.T) {
	f := newShareFixture(t)

	rows := f.list(t)
	if len(rows) == 0 {
		t.Fatal("no libraries listed")
	}
	for id, r := range rows {
		if r.Shared {
			t.Errorf("library %d (%s) is shared before anybody shared it", id, r.Name)
		}
	}
}

func TestSharingAndUnsharingRoundTrip(t *testing.T) {
	f := newShareFixture(t)

	resp := f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.videoID),
		map[string]any{"ceiling": "PG-13"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("share: status %d, want 204", resp.StatusCode)
	}

	row := f.list(t)[f.videoID]
	if !row.Shared || row.Ceiling != "PG-13" {
		t.Errorf("after sharing: shared=%v ceiling=%q, want true/PG-13", row.Shared, row.Ceiling)
	}

	resp = f.h.authed(t, "DELETE", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.videoID), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unshare: status %d, want 204", resp.StatusCode)
	}
	if f.list(t)[f.videoID].Shared {
		t.Error("still shared after being un-shared")
	}
}

/*
 * A ceiling on a music library is a control that does nothing, and ADR 0071 §6
 * says the UI must say so rather than offer it. The server refuses one rather
 * than storing it, because a stored-and-ignored limit leaves a host believing
 * something is in force that is not.
 */
func TestACeilingIsRefusedWhereItWouldDoNothing(t *testing.T) {
	f := newShareFixture(t)

	resp := f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.music.ID),
		map[string]any{"ceiling": "PG-13"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a ceiling on a music library", resp.StatusCode)
	}
	if f.list(t)[f.music.ID].Shared {
		t.Error("the refused request shared the library anyway")
	}
}

// Sharing music itself is fine — it is only the limit that is meaningless.
func TestMusicCanBeSharedWithoutACeiling(t *testing.T) {
	f := newShareFixture(t)

	resp := f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.music.ID),
		map[string]any{"ceiling": ""})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	row := f.list(t)[f.music.ID]
	if !row.Shared {
		t.Error("music library was not shared")
	}
	if row.SupportsCeiling {
		t.Error("music reports that it supports a ceiling; the UI would offer an inert control")
	}
}

// The host is told what a ceiling would cost before choosing it (ADR 0071 §6).
func TestTheCostOfACeilingIsReported(t *testing.T) {
	f := newShareFixture(t)

	row := f.list(t)[f.videoID]
	if !row.SupportsCeiling {
		t.Fatal("a film library reports no ceiling support")
	}
	// Reported for an unshared library too: the number is needed before the
	// decision, not after it.
	if row.Unrated < 0 || row.Total < 0 {
		t.Errorf("unrated=%d total=%d", row.Unrated, row.Total)
	}
}

func TestAnUnknownCeilingIsRefused(t *testing.T) {
	f := newShareFixture(t)

	resp := f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.videoID),
		map[string]any{"ceiling": "NC-42"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// A share cannot name a server this one has never been introduced to.
func TestNoSharesForAnUnknownPeer(t *testing.T) {
	f := newShareFixture(t)
	stranger := anotherServer(t)

	resp := f.h.authed(t, "GET", "/api/peers/"+stranger.Fingerprint()+"/shares", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

/*
 * Administrative, and the contrast is the point: presence grants next door are
 * deliberately *not*, because a switch somebody else can flip is not consent.
 * A library share is about this server's content rather than anybody's own
 * consent, which is the same class as adding a library or pairing.
 */
func TestAMemberCannotShareALibrary(t *testing.T) {
	f := newShareFixture(t)
	sam := f.h.member(t, "sam", "another good long password")

	resp := f.h.asUser(t, sam, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.videoID),
		map[string]any{"ceiling": ""})
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Error("a member shared a library")
	}

	resp = f.h.asUser(t, sam, "GET", "/api/peers/"+f.peerFP+"/shares", nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a member read what is shared")
	}
}

// Unpairing takes every share with it, so the list for a peer that is gone is
// a 404 rather than a list of grants nobody holds.
func TestUnpairingRemovesTheShares(t *testing.T) {
	f := newShareFixture(t)
	f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(f.videoID),
		map[string]any{"ceiling": ""}).Body.Close()

	f.h.authed(t, "DELETE", "/api/peers/"+f.peerFP, nil).Body.Close()

	resp := f.h.authed(t, "GET", "/api/peers/"+f.peerFP+"/shares", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d after unpairing, want 404", resp.StatusCode)
	}
}
