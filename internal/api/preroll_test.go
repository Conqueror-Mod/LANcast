package api

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

type prerollResp struct {
	Clip *prerollClip `json:"clip"`
}

func askPreroll(t *testing.T, h *harness, id int64, fromStart bool) *prerollClip {
	t.Helper()
	q := "/api/preroll?item=" + itoa(id)
	if fromStart {
		q += "&from_start=true"
	}
	var got prerollResp
	decode(t, h.authed(t, "GET", q, nil), &got)
	return got.Clip
}

// prerollHarness: a secured server, a film, and a folder with one clip in it.
func prerollHarness(t *testing.T) (*harness, int64, string) {
	t.Helper()
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	film := h.addFile(t, "film.mkv", []byte("film"))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Cinema intro.mp4"), []byte("intro bytes"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a clip"), 0o644)
	return h, film, dir
}

func setPrerollDir(t *testing.T, h *harness, dir string) int {
	t.Helper()
	resp := h.authed(t, "PUT", "/api/settings", map[string]any{"preroll_dir": dir})
	resp.Body.Close()
	return resp.StatusCode
}

func TestPrerollIsOffUntilAFolderIsSet(t *testing.T) {
	h, film, _ := prerollHarness(t)
	if c := askPreroll(t, h, film, false); c != nil {
		t.Fatalf("a clip with no folder set: %+v", c)
	}
}

func TestPrerollPlaysBeforeAFilmFromTheStart(t *testing.T) {
	h, film, dir := prerollHarness(t)
	if code := setPrerollDir(t, h, dir); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("set folder = %d", code)
	}
	c := askPreroll(t, h, film, false)
	if c == nil || c.Title != "Cinema intro" {
		t.Fatalf("clip = %+v", c)
	}

	resp := h.authed(t, "GET", "/api/preroll/"+c.Key, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "intro bytes" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("stream = %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

// Never on a resume: a clip before minute 47 is only an obstacle. Play from
// start is a fresh start, and gets one.
func TestPrerollNeverPlaysOnAResume(t *testing.T) {
	h, film, dir := prerollHarness(t)
	setPrerollDir(t, h, dir)
	resp := h.authed(t, "PUT", "/api/items/"+itoa(film)+"/progress", map[string]any{"position_ms": 2_820_000})
	resp.Body.Close()
	if c := askPreroll(t, h, film, false); c != nil {
		t.Fatalf("a clip before a resume: %+v", c)
	}
	if c := askPreroll(t, h, film, true); c == nil {
		t.Fatal("no clip before Play from start")
	}
}

func TestPrerollIsForFilmsOnly(t *testing.T) {
	h, _, dir := prerollHarness(t)
	setPrerollDir(t, h, dir)
	ep, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Show S01E01.mkv"), Kind: "episode",
		Title: "ep", SortTitle: "ep", Container: "mkv", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c := askPreroll(t, h, ep, false); c != nil {
		t.Fatalf("a clip before an episode: %+v", c)
	}
}

func TestAPersonCanSkipPrerollForThemselves(t *testing.T) {
	h, film, dir := prerollHarness(t)
	setPrerollDir(t, h, dir)
	resp := h.authed(t, "PUT", "/api/profile/playback", map[string]any{"skip_preroll": true})
	var prefs store.PlaybackPrefs
	decode(t, resp, &prefs)
	// Changing one leaves the other alone.
	if !prefs.SkipPreroll || !prefs.SkipIntros {
		t.Fatalf("prefs = %+v, want skip_preroll on and skip_intros still at its default", prefs)
	}
	if c := askPreroll(t, h, film, false); c != nil {
		t.Fatalf("a clip for somebody who turned it off: %+v", c)
	}
	bad := h.authed(t, "PUT", "/api/profile/playback", map[string]any{})
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body = %d, want 400", bad.StatusCode)
	}
}

func TestAForgedClipKeyPlaysNothing(t *testing.T) {
	h, _, dir := prerollHarness(t)
	setPrerollDir(t, h, dir)
	for _, name := range []string{"notes.txt", "../film.mkv", "nope.mp4"} {
		resp := h.authed(t, "GET", "/api/preroll/"+base64.RawURLEncoding.EncodeToString([]byte(name)), nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%q answered %d", name, resp.StatusCode)
		}
	}
}

func TestThePrerollFolderIsValidated(t *testing.T) {
	h, _, _ := prerollHarness(t)
	for _, bad := range []string{"relative/folder", filepath.Join(t.TempDir(), "missing")} {
		if code := setPrerollDir(t, h, bad); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
	if code := setPrerollDir(t, h, ""); code != http.StatusOK && code != http.StatusNoContent {
		t.Errorf("clearing it: %d", code)
	}
}

// Settings reads back how many clips the folder holds, so a folder that is set
// but empty can be named as playing nothing (it was tried for real: a folder
// was saved, nothing said it held no video).
func TestSettingsSayHowManyClipsTheFolderHolds(t *testing.T) {
	h, _, dir := prerollHarness(t)
	count := func() int {
		var got struct {
			Clips *int `json:"preroll_clips"`
		}
		decode(t, h.authed(t, "GET", "/api/settings", nil), &got)
		if got.Clips == nil {
			t.Fatal("no preroll_clips in settings")
		}
		return *got.Clips
	}
	if n := count(); n != 0 {
		t.Fatalf("off: %d", n)
	}
	if code := setPrerollDir(t, h, dir); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("set: %d", code)
	}
	// The .mp4 and not the notes beside it.
	if n := count(); n != 1 {
		t.Fatalf("one clip: %d", n)
	}
	empty := t.TempDir()
	setPrerollDir(t, h, empty)
	if n := count(); n != 0 {
		t.Fatalf("empty folder: %d", n)
	}
}
