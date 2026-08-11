package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/store"
)

// End to end, against a real store and a real directory: the walk finds the
// .m3u, the import resolves its lines to the tracks the same scan just wrote,
// and the playlist comes back in file order. The unit tests either side of this
// use a fake; this is the one that proves the two halves meet.

func writeM3U(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanImportsAnM3U(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeFile(t, root, "b.mp3", 2048)
	// Deliberately not alphabetical, and with a repeat: the order and the
	// duplicate are the two things a playlist must preserve.
	writeM3U(t, root, "Road Trip.m3u", "b.mp3\na.mp3\nb.mp3\n")

	p := scanAndWait(t, sc, lib)
	if p.PlaylistsImported != 1 {
		t.Fatalf("PlaylistsImported = %d, want 1 (issues: %+v)", p.PlaylistsImported, p.Issues)
	}

	pid, err := st.ItemIDByPath(ctx, filepath.Join(root, "Road Trip.m3u"))
	if err != nil || pid == 0 {
		t.Fatalf("no playlist row for the .m3u (id=%d err=%v)", pid, err)
	}
	entries, err := st.PlaylistEntries(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].Path != filepath.Join(root, "b.mp3") ||
		entries[1].Path != filepath.Join(root, "a.mp3") ||
		entries[2].Path != filepath.Join(root, "b.mp3") {
		t.Errorf("entries out of order: %v %v %v",
			entries[0].Path, entries[1].Path, entries[2].Path)
	}
}

// The .m3u must not become a track. It is not media, and a music library that
// lists "Road Trip.m3u" beside the songs has misunderstood what it read.
func TestPlaylistFileIsNotIndexedAsATrack(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeM3U(t, root, "p.m3u", "a.mp3\n")
	scanAndWait(t, sc, lib)

	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == "track" && filepath.Ext(it.Path) == ".m3u" {
			t.Errorf("the playlist file was indexed as a track: %s", it.Path)
		}
	}
}

// The rule from ADR 0030: a rescan reconciles files, it does not re-litigate
// decisions. Once a human has edited a playlist, the file on disk stops being
// allowed to overwrite it — the locked-fields rule, applied to membership.
func TestARescanDoesNotUndoAnEditedPlaylist(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeFile(t, root, "b.mp3", 2048)
	writeM3U(t, root, "p.m3u", "a.mp3\nb.mp3\n")
	scanAndWait(t, sc, lib)

	pid, err := st.ItemIDByPath(ctx, filepath.Join(root, "p.m3u"))
	if err != nil || pid == 0 {
		t.Fatalf("no playlist (id=%d err=%v)", pid, err)
	}

	// A person edits it down to one track, which locks membership.
	first, err := st.ItemIDByPath(ctx, filepath.Join(root, "a.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetPlaylistEntries(ctx, pid, []int64{first}); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, pid, membersLock); err != nil {
		t.Fatal(err)
	}

	scanAndWait(t, sc, lib)

	entries, err := st.PlaylistEntries(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the rescan restored %d entries, undoing the edit — membership lock ignored",
			len(entries))
	}
}

// An unedited playlist must still track its file. The lock is the exception,
// not the rule: without this, "do not clobber edits" quietly becomes "import
// once and never again", and a playlist someone adds a song to on disk stops
// updating.
func TestARescanUpdatesAnUneditedPlaylist(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeFile(t, root, "b.mp3", 2048)
	writeM3U(t, root, "p.m3u", "a.mp3\n")
	scanAndWait(t, sc, lib)

	writeM3U(t, root, "p.m3u", "a.mp3\nb.mp3\n")
	scanAndWait(t, sc, lib)

	pid, _ := st.ItemIDByPath(ctx, filepath.Join(root, "p.m3u"))
	entries, err := st.PlaylistEntries(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2 — an unedited playlist follows its file", len(entries))
	}
}

// Re-importing must update the one playlist, not accumulate a new one per scan.
func TestRescanDoesNotDuplicatePlaylists(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeM3U(t, root, "p.m3u", "a.mp3\n")
	scanAndWait(t, sc, lib)
	scanAndWait(t, sc, lib)

	items, _, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: lib.ID, Kind: "playlist", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("got %d playlists after two scans, want 1", len(items))
	}
}

// Our own HLS output must be ignored in silence — not imported, and not
// reported as a problem either. A user who sees "could not import stream.m3u8"
// goes looking for a fault that does not exist.
func TestHLSPlaylistIsIgnoredWithoutAnIssue(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeM3U(t, root, "stream.m3u8", "#EXTM3U\n#EXT-X-TARGETDURATION:6\nseg0.m4s\n")

	p := scanAndWait(t, sc, lib)
	if p.PlaylistsImported != 0 {
		t.Errorf("PlaylistsImported = %d, want 0", p.PlaylistsImported)
	}
	for _, iss := range p.Issues {
		if filepath.Ext(iss.Path) == ".m3u8" {
			t.Errorf("HLS playlist reported as an issue: %+v", iss)
		}
	}
}

// What could not be found is reported on the scan, not only in the log.
func TestMissingTracksAreReportedOnTheScan(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "a.mp3", 2048)
	writeM3U(t, root, "p.m3u", "a.mp3\nnot-here.mp3\ngone.mp3\n")

	p := scanAndWait(t, sc, lib)
	if p.PlaylistsImported != 1 {
		t.Fatalf("PlaylistsImported = %d, want 1", p.PlaylistsImported)
	}
	found := false
	for _, iss := range p.Issues {
		if filepath.Ext(iss.Path) == ".m3u" {
			found = true
			if want := "imported 1 of 3"; !strings.Contains(iss.Reason, want) {
				t.Errorf("Reason = %q, want it to contain %q", iss.Reason, want)
			}
		}
	}
	if !found {
		t.Errorf("nothing reported about the two missing tracks; issues = %+v", p.Issues)
	}
}
