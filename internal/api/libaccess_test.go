package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/store"
)

/*
 * A library kept from an account (ADR 0084), at the boundary.
 *
 * Every surface stage 1 covers is asked the same question twice: by the member
 * the library is kept from, and by the administrator, who sees everything. The
 * second asking is the control. Without it, a test passes just as well when the
 * private film was never there at all.
 */

type accessHarness struct {
	*harness
	member      *http.Cookie
	private     int64 // the hidden library
	privateFilm int64
	openFilm    int64
}

func newAccessHarness(t *testing.T) accessHarness {
	t.Helper()
	h := langHarness(t)
	ctx := context.Background()
	a := accessHarness{harness: h}
	a.openFilm = h.addFile(t, "paddington.mkv", []byte("x"))

	dir := t.TempDir()
	lib, err := h.st.CreateLibrary(ctx, "Private", "movie", dir)
	if err != nil {
		t.Fatal(err)
	}
	a.private = lib.ID
	path := filepath.Join(dir, "home-video.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.privateFilm, err = h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: lib.ID, Path: path, Kind: "movie",
		Title: "Home Video", SortTitle: "Home Video", Container: "mkv", SizeBytes: 1, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	a.member = h.addMember(t, "kid", "correct horse battery")
	return a
}

func (a accessHarness) memberID(t *testing.T) string {
	t.Helper()
	users, err := a.st.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Name == "kid" {
			return u.ID
		}
	}
	t.Fatal("no member")
	return ""
}

func (a accessHarness) hide(t *testing.T, ids ...int64) *http.Response {
	t.Helper()
	if ids == nil {
		ids = []int64{}
	}
	return a.authed(t, "PUT", "/api/users/"+a.memberID(t)+"/libraries", map[string]any{"hidden": ids})
}

func status(t *testing.T, resp *http.Response) int {
	t.Helper()
	resp.Body.Close()
	return resp.StatusCode
}

func TestAHiddenLibraryIsGoneForTheMemberAndOnlyThem(t *testing.T) {
	a := newAccessHarness(t)
	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatalf("hide = %d", code)
	}

	asMember := func(method, path string) *http.Response { return a.doAs(t, a.member, method, path, nil) }
	asAdmin := func(method, path string) *http.Response { return a.authed(t, method, path, nil) }

	// The library list.
	libNames := func(resp *http.Response) string {
		var libs []struct {
			Name string `json:"name"`
		}
		decode(t, resp, &libs)
		var names []string
		for _, l := range libs {
			names = append(names, l.Name)
		}
		return strings.Join(names, ",")
	}
	if got := libNames(asMember("GET", "/api/libraries")); strings.Contains(got, "Private") {
		t.Errorf("member's library list = %s", got)
	}
	if got := libNames(asAdmin("GET", "/api/libraries")); !strings.Contains(got, "Private") {
		t.Errorf("control: admin's library list = %s", got)
	}

	// Browsing everything, with no library named.
	titles := func(resp *http.Response) string {
		var page struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		decode(t, resp, &page)
		var out []string
		for _, it := range page.Items {
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}
	if got := titles(asMember("GET", "/api/items?limit=100")); strings.Contains(got, "Home Video") ||
		!strings.Contains(got, "paddington") {
		t.Errorf("member's browse = %s", got)
	}
	// And asking for the hidden library by name.
	lib := fmt.Sprint(a.private)
	if got := titles(asMember("GET", "/api/items?library_id="+lib)); got != "" {
		t.Errorf("member browsing the hidden library by id got %s", got)
	}
	if got := titles(asAdmin("GET", "/api/items?library_id="+lib)); !strings.Contains(got, "Home Video") {
		t.Errorf("control: admin browsing it got %q", got)
	}

	// By id, and the bytes.
	film := fmt.Sprint(a.privateFilm)
	for _, path := range []string{"/api/items/" + film, "/api/stream/" + film, "/api/items/" + film + "/download"} {
		if code := status(t, asMember("GET", path)); code != http.StatusNotFound {
			t.Errorf("member GET %s = %d, want 404", path, code)
		}
	}
	if code := status(t, asAdmin("GET", "/api/items/"+film)); code != http.StatusOK {
		t.Errorf("control: admin GET item = %d", code)
	}

	// The library-scoped routes answer as for a library that does not exist.
	for _, sub := range []string{"facets", "cast", "timeline", "places", "people", "roots", "scan"} {
		path := "/api/libraries/" + lib + "/" + sub
		if code := status(t, asMember("GET", path)); code != http.StatusNotFound {
			t.Errorf("member GET %s = %d, want 404", path, code)
		}
	}
	if code := status(t, asAdmin("GET", "/api/libraries/"+lib+"/facets")); code == http.StatusNotFound {
		t.Error("control: admin was refused the library's facets")
	}

	// Lifting it gives everything back.
	if code := status(t, a.hide(t)); code != http.StatusOK {
		t.Fatalf("unhide = %d", code)
	}
	if code := status(t, asMember("GET", "/api/items/"+film)); code != http.StatusOK {
		t.Errorf("after lifting, member GET item = %d", code)
	}
}

func TestOnlyAnAdministratorDecidesWhoSeesALibrary(t *testing.T) {
	a := newAccessHarness(t)
	path := "/api/users/" + a.memberID(t) + "/libraries"
	if code := status(t, a.doAs(t, a.member, "PUT", path, map[string]any{"hidden": []int64{}})); code != http.StatusForbidden {
		t.Errorf("member PUT = %d, want 403", code)
	}
	if code := status(t, a.doAs(t, a.member, "GET", path, nil)); code != http.StatusForbidden {
		t.Errorf("member GET = %d, want 403", code)
	}
	// A body that names nothing is refused, not read as "lift everything".
	if code := status(t, a.authed(t, "PUT", path, map[string]any{})); code != http.StatusBadRequest {
		t.Errorf("PUT {} = %d, want 400", code)
	}
	if code := status(t, a.authed(t, "PUT", path, map[string]any{"hidden": []int64{9999}})); code != http.StatusBadRequest {
		t.Errorf("hiding a library that does not exist = %d, want 400", code)
	}
	var got struct {
		Hidden  []int64 `json:"hidden"`
		Applies bool    `json:"applies"`
	}
	decode(t, a.hide(t, a.private), &got)
	if len(got.Hidden) != 1 || got.Hidden[0] != a.private || !got.Applies {
		t.Errorf("after hiding = %+v", got)
	}
}

/*
 * Stage 2: the lists built from an account's own records. Each was missed by
 * the rating ceiling too (ADR 0084, Context), so the member builds real
 * history in the private library *before* it is hidden — the case that
 * matters is a film watched last month in a library hidden today.
 */
func TestAHiddenLibraryLeavesTheMembersOwnLists(t *testing.T) {
	a := newAccessHarness(t)
	ctx := context.Background()

	// A second private film, to finish, so it reaches the viewing log.
	finished, err := a.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: a.private, Path: filepath.Join(t.TempDir(), "finished.mkv"), Kind: "movie",
		Title: "Finished Private", SortTitle: "Finished Private", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	as := func(method, path string, body any) *http.Response { return a.doAs(t, a.member, method, path, body) }
	put := func(path string, body any) {
		t.Helper()
		if code := status(t, as("PUT", path, body)); code >= 300 {
			t.Fatalf("PUT %s = %d", path, code)
		}
	}
	put(fmt.Sprintf("/api/items/%d/progress", a.privateFilm), map[string]any{"position_ms": 60000, "watched": false})
	put(fmt.Sprintf("/api/items/%d/progress", a.openFilm), map[string]any{"position_ms": 60000, "watched": false})
	put(fmt.Sprintf("/api/items/%d/progress", finished), map[string]any{"position_ms": 0, "watched": true})
	put(fmt.Sprintf("/api/items/%d/rating", a.privateFilm), map[string]any{"score": 8})

	type counts struct{ continuing, viewings, ratings, yearTitles int }
	read := func() (counts, string) {
		var c counts
		var cont struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		decode(t, as("GET", "/api/continue", nil), &cont)
		var names []string
		for _, it := range cont.Items {
			names = append(names, it.Title)
		}
		c.continuing = len(cont.Items)
		var vs struct {
			Total int `json:"total"`
		}
		decode(t, as("GET", "/api/profile/viewings", nil), &vs)
		c.viewings = vs.Total
		var rs struct {
			Ratings []any `json:"ratings"`
		}
		decode(t, as("GET", "/api/profile/ratings", nil), &rs)
		c.ratings = len(rs.Ratings)
		var yr struct {
			Titles int `json:"titles"`
		}
		decode(t, as("GET", "/api/profile/year", nil), &yr)
		c.yearTitles = yr.Titles
		return c, strings.Join(names, ",")
	}

	// The control: before hiding, every list holds the private library.
	before, names := read()
	if before.continuing != 2 || before.viewings != 1 || before.ratings != 1 || before.yearTitles < 3 {
		t.Fatalf("before hiding = %+v (continue: %s); the fixture did not build the history", before, names)
	}

	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatal("hide failed")
	}
	after, names := read()
	if after.continuing != 1 || strings.Contains(names, "Home Video") {
		t.Errorf("Continue Watching after hiding = %s", names)
	}
	if after.viewings != 0 {
		t.Errorf("viewing history still counts %d from the hidden library", after.viewings)
	}
	if after.ratings != 0 {
		t.Errorf("ratings still list %d from the hidden library", after.ratings)
	}
	if after.yearTitles != before.yearTitles-2 {
		t.Errorf("year in review titles = %d, want %d", after.yearTitles, before.yearTitles-2)
	}

	// Kept, not deleted: lifting the restriction gives the history back.
	if code := status(t, a.hide(t)); code != http.StatusOK {
		t.Fatal("unhide failed")
	}
	if again, _ := read(); again != before {
		t.Errorf("after lifting = %+v, want %+v", again, before)
	}
}
