package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * A content-rating ceiling, at the boundary that serves files (ADR 0015).
 *
 * The store's own suite proves the rule. These prove the thing the roadmap
 * entry insists on and a store test cannot reach: that it is enforced **in
 * playback authorisation**, not merely in the listing. A client-side hide is a
 * suggestion, and this API serves files — an account that can be talked into a
 * stream by a hand-written request has no ceiling at all.
 */

// fmtPath fills an item id into a route template. A tiny helper, but these
// tests are about which id was refused, and a stray Sprintf in the middle of an
// assertion reads as arithmetic.
func fmtPath(pattern string, id int64) string {
	return fmt.Sprintf(pattern, id)
}

// accountID finds a member by name. GET /api/users answers an object rather
// than a bare array, and every test here needs the id of the account it just
// made.
func accountID(t *testing.T, h *harness, name string) string {
	t.Helper()
	var page struct {
		Users []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"users"`
	}
	decode(t, h.authed(t, "GET", "/api/users", nil), &page)
	for _, u := range page.Users {
		if u.Name == name {
			return u.ID
		}
	}
	t.Fatalf("no account called %q", name)
	return ""
}

func rate(t *testing.T, h *harness, id int64, label string) {
	t.Helper()
	if err := h.st.UpdateItemMetadata(context.Background(), id,
		store.ItemMetadata{ContentRating: &label}); err != nil {
		t.Fatal(err)
	}
}

func ratedFile(t *testing.T, h *harness, name, label string) int64 {
	t.Helper()
	id := h.addFile(t, name, []byte("not really a film"))
	if label != "" {
		rate(t, h, id, label)
	}
	return id
}

// limitedMember creates a member and sets a ceiling on it the way an
// administrator does — through the API, so the route is covered too.
func limitedMember(t *testing.T, h *harness, name, ceiling string) *http.Cookie {
	t.Helper()
	cookie := h.addMember(t, name, "correct horse battery staple")
	id := accountID(t, h, name)

	resp := h.authed(t, "PATCH", "/api/users/"+id,
		map[string]any{"max_content_rating": ceiling})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setting the ceiling answered %d", resp.StatusCode)
	}
	return cookie
}

func TestAStreamIsRefusedAboveTheCeiling(t *testing.T) {
	/*
	 * The assertion this whole feature stands on. Not "the tile is hidden" —
	 * the bytes are refused, to a request that never looked at a listing.
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	blocked := ratedFile(t, h, "grown-up.mkv", "R")
	child := limitedMember(t, h, "kiddo", "PG")

	for _, path := range []string{
		"/api/items/%d/stream",
		"/api/items/%d/download",
	} {
		resp := h.doAs(t, child, "GET", fmtPath(path, blocked), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestARefusalLooksLikeAnAbsence(t *testing.T) {
	/*
	 * 404 rather than 403, deliberately.
	 *
	 * "You may not see this" and "this does not exist" have to be
	 * indistinguishable from outside, or the refusal becomes a way to
	 * enumerate what the library holds — a restricted account could walk the
	 * ids and learn exactly which ones its household keeps from it.
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	blocked := ratedFile(t, h, "grown-up.mkv", "R")
	child := limitedMember(t, h, "kiddo", "PG")

	real := h.doAs(t, child, "GET", fmtPath("/api/items/%d", blocked), nil)
	real.Body.Close()
	missing := h.doAs(t, child, "GET", "/api/items/99999", nil)
	missing.Body.Close()

	if real.StatusCode != missing.StatusCode {
		t.Errorf("a blocked item answered %d and a nonexistent one %d; they must match",
			real.StatusCode, missing.StatusCode)
	}
}

func TestTheGridAgreesWithWhatWillPlay(t *testing.T) {
	// Otherwise a restricted account browses tiles that 404 when opened, which
	// is worse than not seeing them and tells it exactly what it is missing.
	h := newHarness(t)
	h.secure(t, "a good long password")
	ratedFile(t, h, "grown-up.mkv", "R")
	ratedFile(t, h, "cartoon.mkv", "G")
	child := limitedMember(t, h, "kiddo", "PG")

	var page struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Total int `json:"total"`
	}
	resp := h.doAs(t, child, "GET", "/api/items?library_id=1", nil)
	decode(t, resp, &page)

	for _, it := range page.Items {
		if it.Title == "grown-up.mkv" {
			t.Error("an R film was listed to a PG account")
		}
	}
	if page.Total != len(page.Items) {
		t.Errorf("total = %d but %d items came back; the count must not describe a bigger library",
			page.Total, len(page.Items))
	}
}

func TestAnUnlimitedAccountIsUnaffected(t *testing.T) {
	// The ordinary case, and the one every existing account is in after an
	// upgrade. The column existing is not consent to use it.
	h := newHarness(t)
	h.secure(t, "a good long password")
	id := ratedFile(t, h, "grown-up.mkv", "R")

	resp := h.authed(t, "GET", fmtPath("/api/items/%d", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 for an account with no ceiling", resp.StatusCode)
	}
}

func TestOnlyAnAdministratorSetsACeiling(t *testing.T) {
	/*
	 * The rule that separates this from the sharing switch beside it. A limit
	 * the limited party can lift is not a limit, so the route is admin-only —
	 * where sharing has no admin route at all, because a switch somebody else
	 * can flip is not consent (ADR 0035).
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	child := limitedMember(t, h, "kiddo", "PG")

	resp := h.doAs(t, child, "PATCH", "/api/users/"+accountID(t, h, "kiddo"),
		map[string]any{"max_content_rating": ""})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403: an account cleared its own ceiling", resp.StatusCode)
	}
}

func TestACeilingThisServerCannotPlaceIsRefused(t *testing.T) {
	/*
	 * 400 rather than quietly stored. A ceiling nothing can place means "no
	 * limit", and a household believing a limit is in force that is not is the
	 * only failure here worse than being too strict.
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	h.addMember(t, "kiddo", "correct horse battery staple")

	resp := h.authed(t, "PATCH", "/api/users/"+accountID(t, h, "kiddo"),
		map[string]any{"max_content_rating": "Certificate 27"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

/*
 * The listings that build their own SQL.
 *
 * ListItems carries the predicate and GetItem is the chokepoint for bytes, so
 * these are the third shape: a shelf or a container's children, assembled by
 * hand and handed back. A restricted account shown a title it cannot open is
 * worse off than one not shown it — the tile names exactly what the household
 * is keeping from them.
 */

func TestAShelfDoesNotNameWhatCannotBeOpened(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	blocked := ratedFile(t, h, "grown-up.mkv", "R")
	allowed := ratedFile(t, h, "cartoon.mkv", "G")
	child := limitedMember(t, h, "kiddo", "PG")

	// Trending is built from what has been played, so both need a viewing.
	for _, id := range []int64{blocked, allowed} {
		resp := h.authed(t, "PUT", fmtPath("/api/items/%d/progress", id),
			map[string]any{"position_ms": 60000, "watched": true})
		resp.Body.Close()
	}

	var got struct {
		Items []struct {
			Item struct {
				Title string `json:"title"`
			} `json:"item"`
		} `json:"items"`
	}
	decode(t, h.doAs(t, child, "GET", "/api/libraries/1/trending", nil), &got)

	for _, e := range got.Items {
		if e.Item.Title == "grown-up.mkv" {
			t.Error("the trending shelf offered an R film to a PG account")
		}
	}

	/*
	 * The positive control, without which this passes on an empty shelf.
	 *
	 * The same request as an administrator has to show both films — otherwise
	 * the assertion above is satisfied by a shelf that was never populated,
	 * and a regression in the filter would look exactly like a regression in
	 * the fixture.
	 */
	var asAdmin struct {
		Items []struct {
			Item struct {
				Title string `json:"title"`
			} `json:"item"`
		} `json:"items"`
	}
	decode(t, h.authed(t, "GET", "/api/libraries/1/trending", nil), &asAdmin)
	titles := map[string]bool{}
	for _, e := range asAdmin.Items {
		titles[e.Item.Title] = true
	}
	if !titles["grown-up.mkv"] || !titles["cartoon.mkv"] {
		t.Fatalf("the shelf itself is empty for an unlimited account (%v); this test proves nothing", titles)
	}
}

func TestAContainersChildrenAreFilteredToo(t *testing.T) {
	/*
	 * A show under the ceiling can hold an episode above it — a late season
	 * rated harder than the programme it belongs to. The show's page opens,
	 * because the show is permitted, and the episode list is where the rule has
	 * to hold.
	 */
	h := newHarness(t)
	h.secure(t, "a good long password")
	ctx := context.Background()
	show, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: h.dir + "/Some Programme", Kind: "show",
		Title: "Some Programme", SortTitle: "Some Programme",
	})
	if err != nil {
		t.Fatal(err)
	}
	mild := ratedFile(t, h, "S01E01.mkv", "")
	harsh := ratedFile(t, h, "S01E02.mkv", "TV-MA")
	for _, ep := range []int64{mild, harsh} {
		if err := h.st.SetParent(ctx, ep, &show); err != nil {
			t.Fatal(err)
		}
	}
	rate(t, h, show, "TV-PG")
	child := limitedMember(t, h, "kiddo", "TV-PG")

	var page struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, h.doAs(t, child, "GET", fmtPath("/api/items?parent_id=%d", show), nil), &page)

	seen := map[int64]bool{}
	for _, it := range page.Items {
		seen[it.ID] = true
	}
	if seen[harsh] {
		t.Error("an episode rated above the ceiling was listed under a permitted show")
	}
	// And the one with no rating of its own is still there, inheriting the
	// show's — otherwise this "passes" by hiding the whole season.
	if !seen[mild] {
		t.Error("an episode inheriting a permitted show's rating was hidden")
	}
}
