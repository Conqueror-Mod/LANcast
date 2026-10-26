package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
	"lancast/internal/subtitle"
)

/*
 * Containment follows links (internal/contain).
 *
 * Found by an outside review and confirmed on 2026-10-25: the check was
 * lexical, the scanner imported a symlinked file like any other, and so
 * `film.mkv` linking to any file on the machine passed and was served by a
 * service running as LocalSystem. These build that library for real and ask
 * for the bytes.
 */

// linkOrSkip makes a symlink, or skips: on Windows that needs Developer Mode
// or elevation. Linux CI can always, so the test always runs somewhere.
func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

// addLink registers a link already on disk in the harness library, the shape
// a scan would have produced before the scanner refused such links.
func (h *harness) addLink(t *testing.T, link string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: link, Kind: "movie",
		Title: filepath.Base(link), SortTitle: filepath.Base(link), Container: "mkv",
		SizeBytes: 1, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, err := h.st.KnownFiles(ctx, h.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := known[link]
	if !ok || f.ID == 0 {
		t.Fatalf("setup: %s not registered", link)
	}
	return f.ID
}

func TestStreamRefusesALinkOutOfTheLibrary(t *testing.T) {
	h := newHarness(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("the machine's secrets"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(h.dir, "film.mkv")
	linkOrSkip(t, secret, link)
	id := h.addLink(t, link)

	res := h.do(t, http.MethodGet, "/api/stream/"+itoa(id), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — a link out of the library was served", res.StatusCode)
	}
}

// Positive control: a link to another file in the same library plays. Without
// this the test above passes for a server that refuses every link, or every
// file.
func TestStreamServesALinkInsideTheLibrary(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "real.mkv", []byte("real-bytes"))
	link := filepath.Join(h.dir, "same film.mkv")
	linkOrSkip(t, filepath.Join(h.dir, "real.mkv"), link)
	id := h.addLink(t, link)

	res := h.do(t, http.MethodGet, "/api/stream/"+itoa(id), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 200/206 — a link inside the library must play", res.StatusCode)
	}
}

// A sidecar subtitle went from its row to a file read with no check at all.
// It is held to the item's location now, links followed.
func TestSubtitleRefusesALinkOutOfTheLibrary(t *testing.T) {
	h := newHarness(t)
	// The harness has no extractor; serving a sidecar needs one.
	h.srvAPI.subs = subtitle.NewExtractor(t.TempDir())
	id := h.addFile(t, "film.mkv", []byte("film"))
	secret := filepath.Join(t.TempDir(), "secret.srt")
	if err := os.WriteFile(secret, []byte("1\n00:00:01,000 --> 00:00:02,000\nsecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(h.dir, "film.en.srt")
	if err := os.WriteFile(inside, []byte("1\n00:00:01,000 --> 00:00:02,000\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(h.dir, "film.fr.srt")
	linkOrSkip(t, secret, link)

	ctx := context.Background()
	if err := h.st.ReplaceSidecarSubtitles(ctx, id, []store.ExternalSubtitle{
		{ItemID: id, Path: inside, Language: "en", Format: "srt"},
		{ItemID: id, Path: link, Language: "fr", Format: "srt"},
	}); err != nil {
		t.Fatal(err)
	}
	subs, err := h.st.ExternalSubtitles(ctx, id)
	if err != nil || len(subs) != 2 {
		t.Fatalf("setup: subtitles = %v, %v", subs, err)
	}

	for _, sub := range subs {
		res := h.do(t, http.MethodGet, "/api/items/"+itoa(id)+"/subtitles/external-"+itoa(sub.ID)+".vtt", nil)
		res.Body.Close()
		want := http.StatusOK
		if sub.Path == link {
			want = http.StatusNotFound
		}
		if res.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", filepath.Base(sub.Path), res.StatusCode, want)
		}
	}
}
