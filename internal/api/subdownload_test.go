package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

/*
 * Where a downloaded subtitle may be written (R07-1).
 *
 * The language a client sends becomes part of the file name. It used to pass
 * through NormalizeLanguage, which returns what it does not recognise
 * unchanged, so "x/../../../../../escaped" wrote outside the data directory
 * with whatever OpenSubtitles served. The refusal is checked twice: by the
 * status, and by looking for any file the request could have written.
 */

// fakeSubtitleProvider stands in for OpenSubtitles: /download answers with a
// link back to itself, and the link serves a small SubRip file. downloads
// counts the calls to /download, which is what spends an allowance.
func fakeSubtitleProvider(t *testing.T, h *harness) *atomic.Int32 {
	t.Helper()
	var downloads atomic.Int32
	var fake *httptest.Server
	fake = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			downloads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"link":"` + fake.URL + `/file.srt","file_name":"film.srt","remaining":10}`))
		case "/file.srt":
			_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	h.srvAPI.subtitleBaseURL = fake.URL
	set := h.settings.Get()
	set.OpenSubtitlesKey = "test-key"
	if err := h.settings.Set(set); err != nil {
		t.Fatal(err)
	}
	return &downloads
}

// srtFilesUnder lists every .srt beneath root, wherever it landed.
func srtFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".srt") {
			found = append(found, p)
		}
		return nil
	})
	return found
}

func TestASubtitleLanguageThatClimbsOutIsRefusedAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	downloads := fakeSubtitleProvider(t, h)
	id := h.addFile(t, "film.mkv", []byte("x"))

	// The data directory's parent, so a write that escaped by a few folders is
	// still found; t.TempDir gives each test its own, so nothing else is there.
	watch := filepath.Dir(h.dataDir)

	for _, lang := range []string{
		"x/../../../../../escaped",
		`x\..\..\..\..\..\escaped`,
		"../escaped",
		"en/../../x",
		"..",
		"c:",
		"en.srt",
		"english language", // a space is not in any tag
	} {
		resp := h.do(t, "POST", "/api/items/"+itoa(id)+"/subtitles/download",
			map[string]any{"file_id": 7, "language": lang})
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("language %q: status = %d, want 400", lang, resp.StatusCode)
		}
	}
	if got := srtFilesUnder(t, watch); len(got) != 0 {
		t.Errorf("a refused download wrote %v", got)
	}
	if n := downloads.Load(); n != 0 {
		t.Errorf("a refused download spent the allowance %d times; the language is checked first", n)
	}

	// The control: the same request with a real language writes exactly one
	// file, inside <data>/subtitles. Without it, a handler that refused
	// everything would pass the half above.
	resp := h.do(t, "POST", "/api/items/"+itoa(id)+"/subtitles/download",
		map[string]any{"file_id": 7, "language": "English"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("control: status = %d, want 200", resp.StatusCode)
	}
	want := filepath.Join(h.dataDir, "subtitles", "downloaded", itoa(id), "7.en.srt")
	if got := srtFilesUnder(t, watch); len(got) != 1 || got[0] != want {
		t.Errorf("control wrote %v, want only %s", got, want)
	}
}

// A tag the table does not know is still a language, and still allowed.
func TestASubtitleLanguageTagOutsideTheTableIsKept(t *testing.T) {
	h := newHarness(t)
	fakeSubtitleProvider(t, h)
	id := h.addFile(t, "film.mkv", []byte("x"))

	resp := h.do(t, "POST", "/api/items/"+itoa(id)+"/subtitles/download",
		map[string]any{"file_id": 9, "language": "PT-BR"})
	var body struct {
		Language string `json:"language"`
	}
	decode(t, resp, &body)
	if body.Language != "pt-br" {
		t.Errorf("language = %q, want pt-br", body.Language)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "subtitles", "downloaded", itoa(id), "9.pt-br.srt")); err != nil {
		t.Errorf("file not written: %v", err)
	}
}

// Downloading attaches a file to an item every viewer of it sees, so the route
// asks the same question playing does: may this account open the item? A
// member is refused a film in a library hidden from them, exactly as though
// it did not exist, and nothing is fetched or written.
func TestASubtitleDownloadIsRefusedForAHiddenLibrary(t *testing.T) {
	a := newAccessHarness(t)
	downloads := fakeSubtitleProvider(t, a.harness)
	if code := status(t, a.hide(t, a.private)); code != http.StatusOK {
		t.Fatalf("hide = %d", code)
	}

	path := "/api/items/" + itoa(a.privateFilm) + "/subtitles/download"
	body := map[string]any{"file_id": 7, "language": "en"}
	if code := status(t, a.doAs(t, a.member, "POST", path, body)); code != http.StatusNotFound {
		t.Errorf("member, hidden library: status = %d, want 404", code)
	}
	if n := downloads.Load(); n != 0 {
		t.Errorf("a refused download reached the provider %d times", n)
	}
	if got := srtFilesUnder(t, a.dataDir); len(got) != 0 {
		t.Errorf("a refused download wrote %v", got)
	}

	// The control: the administrator may.
	if code := status(t, a.authed(t, "POST", path, body)); code != http.StatusOK {
		t.Errorf("control: admin status = %d, want 200", code)
	}
}
