package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/store"
)

// The whole point of the route: the same bytes as a stream, marked as something
// to save rather than something to play.
func TestDownloadIsAnAttachment(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.2016.mkv", []byte("0123456789"))

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/download", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("Content-Disposition = %q, want the RFC 5987 form too", cd)
	}
}

// Range still works, because the file this is most useful for is the one big
// enough that an interrupted transfer matters.
func TestDownloadSupportsRange(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Big.mkv", []byte("0123456789"))

	req, err := http.NewRequest("GET", h.srv.URL+"/api/items/"+itoa(id)+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=4-6")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "456" {
		t.Errorf("body = %q, want %q", got, "456")
	}
}

func TestDownloadUnknownItem(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/items/9999/download", nil),
		http.StatusNotFound, "not_found")
}

// The filename is what the item is called, not what its file is called. A
// download named `tt0111161.2160p.WEB-DL.mkv` is a download nobody can identify
// three weeks later, and the database already knows the answer.
func TestDownloadNameFromMetadata(t *testing.T) {
	year := 2016
	season, episode := 2, 7
	series := "Storm of the Century"

	cases := []struct {
		name string
		it   store.Item
		want string
	}{
		{"movie with year", store.Item{Title: "Arrival", Year: &year}, "Arrival (2016).mkv"},
		{"episode names its series", store.Item{
			Title: "Pilot", Series: &series, Season: &season, Episode: &episode,
		}, "Storm of the Century - S02E07 - Pilot.mkv"},
		// A colon is legal in a title and illegal in a Windows filename, and a
		// slash is a directory on every system there is.
		{"illegal characters folded", store.Item{Title: "AC/DC: Live"}, "AC-DC- Live.mkv"},
		{"untitled falls back", store.Item{Title: ""}, "download.mkv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := tc.it
			if got := downloadName(&it, ".MKV"); got != tc.want {
				t.Errorf("downloadName = %q, want %q", got, tc.want)
			}
		})
	}
}

// A title outside ASCII survives in `filename*` and is not mangled into the
// quoted parameter, where its bytes would be decoded by guesswork.
func TestDownloadNameEncoding(t *testing.T) {
	if got := asciiFold("Amélie"); got != "Am_lie" {
		t.Errorf("asciiFold = %q, want %q", got, "Am_lie")
	}
	// A space must be %20 — '+' means a space only in a query string, and this
	// is a header parameter.
	if got := urlEncodePath("Am lie"); got != "Am%20lie" {
		t.Errorf("urlEncodePath = %q, want %q", got, "Am%20lie")
	}
}

// A download must not escape the library root any more than a stream may. Same
// boundary, same rule (CLAUDE.md) — asserted here because a second route to the
// filesystem is a second place to forget it, and the check is per-handler.
func TestDownloadRefusesEscapingPath(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	outside := filepath.Join(t.TempDir(), "outside.mkv")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	roots, err := h.st.ListRoots(ctx, h.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, RootID: roots[0].ID, Path: outside, Kind: "movie",
		Title: "outside", SortTitle: "outside", Container: "mkv",
		SizeBytes: 7, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFilesInRoot(ctx, roots[0].ID)

	wantError(t, h.do(t, "GET", "/api/items/"+itoa(known[outside].ID)+"/download", nil),
		http.StatusNotFound, "not_found")
}
