package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

/*
 * Lyrics, at the boundary.
 *
 * internal/lyrics proves the parse. These prove the three things the handler
 * decides: which of the two sources wins, that a track without any is an answer
 * rather than a failure, and that a sidecar is found beside the *contained*
 * path rather than wherever a database row claims.
 */

func lyricsOf(t *testing.T, h *harness, id int64) map[string]any {
	t.Helper()
	var got map[string]any
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/lyrics", nil), &got)
	return got
}

func writeSidecar(t *testing.T, h *harness, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestASidecarBesideTheTrackIsFound(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Song.flac", []byte("not really audio"))
	writeSidecar(t, h, "Song.lrc", "[ti:A Song]\n[00:12.34]The first line\n")

	got := lyricsOf(t, h, id)
	if got["source"] != "sidecar" {
		t.Fatalf("source = %v, want sidecar", got["source"])
	}
	if got["synced"] != true {
		t.Error("a file with timestamps was not reported as synced")
	}
	lines, _ := got["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	first, _ := lines[0].(map[string]any)
	if first["text"] != "The first line" || first["at_ms"] != float64(12340) {
		t.Errorf("line = %v", first)
	}
}

func TestATrackWithNoLyricsIsAnAnswerRatherThanAFailure(t *testing.T) {
	/*
	 * 200 with source "none", not 404. The question is reasonable and the
	 * answer is none — and a panel that has to tell a 404 for "no such item"
	 * apart from a 404 for "no words" has been handed the same status for two
	 * different situations.
	 */
	h := newHarness(t)
	id := h.addFile(t, "Bare.flac", []byte("not really audio"))

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/lyrics", nil)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got map[string]any
	decode(t, resp, &got)
	if got["source"] != "none" {
		t.Errorf("source = %v, want none", got["source"])
	}
	lines, ok := got["lines"].([]any)
	if !ok || len(lines) != 0 {
		t.Errorf("lines = %v; an empty list is the honest answer, not null", got["lines"])
	}
}

func TestAnEmptySidecarIsNotLyrics(t *testing.T) {
	// A file that exists and holds nothing is not an answer, and "we found
	// lyrics" over a blank panel is worse than "none found".
	h := newHarness(t)
	id := h.addFile(t, "Blank.flac", []byte("not really audio"))
	writeSidecar(t, h, "Blank.lrc", "[ti:A Song]\n[ar:Somebody]\n")

	if got := lyricsOf(t, h, id)["source"]; got != "none" {
		t.Errorf("source = %v, want none", got)
	}
}

func TestASidecarBelongingToAnotherTrackIsNotBorrowed(t *testing.T) {
	/*
	 * The case an album makes. Twelve tracks share a folder and every one is a
	 * different song, so a rule loose enough to match a file that does not name
	 * its track hands the whole record the same words.
	 */
	h := newHarness(t)
	first := h.addFile(t, "01 First.flac", []byte("not really audio"))
	writeSidecar(t, h, "02 Second.lrc", "[00:01.00]words for the second song\n")
	writeSidecar(t, h, "lyrics.lrc", "[00:01.00]words for nothing in particular\n")

	if got := lyricsOf(t, h, first)["source"]; got != "none" {
		t.Errorf("source = %v, want none: a track was given another's lyrics", got)
	}
}

func TestAnUnsyncedSidecarIsShownAndSaysSo(t *testing.T) {
	/*
	 * Plenty of .lrc files are somebody's copy-and-paste. Showing the words and
	 * saying they do not follow the song beats showing nothing — those are two
	 * different statements and only one of them is true.
	 */
	h := newHarness(t)
	id := h.addFile(t, "Plain.flac", []byte("not really audio"))
	writeSidecar(t, h, "Plain.lrc", "First line\nSecond line\n")

	got := lyricsOf(t, h, id)
	if got["source"] != "sidecar" {
		t.Fatalf("source = %v, want sidecar", got["source"])
	}
	if got["synced"] != false {
		t.Error("a file with no timestamps was reported as synced")
	}
	if lines, _ := got["lines"].([]any); len(lines) != 2 {
		t.Errorf("lines = %d, want 2", len(lines))
	}
}

func TestAContainerHasNoWords(t *testing.T) {
	// A show or an album has no file, so the answer is none rather than an
	// error about a path that was never going to exist.
	h := newHarness(t)
	show, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "A Record"), Kind: "album",
		Title: "A Record", SortTitle: "A Record",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := lyricsOf(t, h, show)["source"]; got != "none" {
		t.Errorf("source = %v, want none", got)
	}
}

func TestLyricsForNothingIsNotFound(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/items/99999/lyrics", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
