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
