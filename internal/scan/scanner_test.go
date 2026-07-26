package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/store"
)

func newScanner(t *testing.T) (*Scanner, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, log), st
}

// writeFile creates a file of n bytes at root/rel, making parents as needed.
func writeFile(t *testing.T, root, rel string, n int) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// scanAndWait runs a scan to completion and returns the final progress.
func scanAndWait(t *testing.T, sc *Scanner, lib store.Library) Progress {
	t.Helper()
	if _, err := sc.Start(lib); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p := sc.Status(lib.ID)
		if p.State != StateRunning {
			if p.State == StateFailed {
				t.Fatalf("scan failed: %s", p.Error)
			}
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not finish within 15s")
	return Progress{}
}

func fixture(t *testing.T, sc *Scanner, st *store.Store) (store.Library, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", root)
	if err != nil {
		t.Fatal(err)
	}
	return *lib, root
}

func TestScanFindsVideosAndSkipsOthers(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)

	writeFile(t, root, "Films/The.Matrix.1999.1080p.BluRay.x264.mkv", 10)
	writeFile(t, root, "Films/Blade Runner 2049 (2017).mkv", 10)
	writeFile(t, root, "Films/notes.txt", 5)
	writeFile(t, root, "Films/poster.jpg", 5)
	writeFile(t, root, "Andor/Season 01/Andor.S01E07.Announcement.mkv", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 3 {
		t.Errorf("FilesSeen = %d, want 3 (non-video files must be ignored)", p.FilesSeen)
	}
	if p.ItemsChanged != 3 {
		t.Errorf("ItemsChanged = %d, want 3", p.ItemsChanged)
	}

	items, total, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}

	byTitle := map[string]store.Item{}
	for _, it := range items {
		byTitle[it.Title] = it
	}

	// The regression the parser suite exists for, verified through the scanner.
	br, ok := byTitle["Blade Runner 2049"]
	if !ok {
		t.Fatalf("Blade Runner 2049 not parsed; got %v", keys(byTitle))
	}
	if br.Year == nil || *br.Year != 2017 {
		t.Errorf("Blade Runner year = %v, want 2017", br.Year)
	}

	ep, ok := byTitle["Announcement"]
	if !ok {
		t.Fatalf("episode not parsed; got %v", keys(byTitle))
	}
	if ep.Kind != "episode" || ep.Series == nil || *ep.Series != "Andor" {
		t.Errorf("episode = kind %q series %v, want episode/Andor", ep.Kind, ep.Series)
	}
	if ep.Season == nil || *ep.Season != 1 || ep.Episode == nil || *ep.Episode != 7 {
		t.Errorf("season/episode = %v/%v, want 1/7", ep.Season, ep.Episode)
	}
}

// Rescanning must skip unchanged files rather than re-parsing them. This is
// what keeps rescans cheap on a large library.
func TestRescanSkipsUnchangedFiles(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "a.mkv", 10)
	writeFile(t, root, "b.mkv", 10)

	first := scanAndWait(t, sc, lib)
	if first.ItemsChanged != 2 {
		t.Fatalf("first scan changed %d, want 2", first.ItemsChanged)
	}

	second := scanAndWait(t, sc, lib)
	if second.FilesSeen != 2 {
		t.Errorf("second scan saw %d files, want 2", second.FilesSeen)
	}
	if second.ItemsChanged != 0 {
		t.Errorf("second scan changed %d, want 0 (unchanged files must be skipped)", second.ItemsChanged)
	}

	_, total, _ := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if total != 2 {
		t.Errorf("total = %d after rescan, want 2 (no duplicates)", total)
	}
}

func TestRescanDetectsChangedFile(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	path := writeFile(t, root, "a.mkv", 10)

	scanAndWait(t, sc, lib)

	// Change size and mtime so the cheap comparison notices.
	if err := os.WriteFile(path, make([]byte, 999), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	p := scanAndWait(t, sc, lib)
	if p.ItemsChanged != 1 {
		t.Errorf("ItemsChanged = %d, want 1 (changed file must be re-read)", p.ItemsChanged)
	}
	_ = st
}

// A file that disappears is flagged, never deleted — an unmounted drive must
// not destroy library data.
func TestMissingFilesAreFlaggedNotDeleted(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	path := writeFile(t, root, "gone.mkv", 10)
	writeFile(t, root, "stays.mkv", 10)

	scanAndWait(t, sc, lib)
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[path].ID
	if err := st.SaveProgress(ctx, id, "local", 5150, false); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	p := scanAndWait(t, sc, lib)
	if p.ItemsMissing != 1 {
		t.Errorf("ItemsMissing = %d, want 1", p.ItemsMissing)
	}

	_, total, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if total != 2 {
		t.Errorf("total = %d, want 2 — the row must survive, not be deleted", total)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatalf("GetItem for removed file: %v", err)
	}
	if !it.Missing {
		t.Error("Missing = false, want true")
	}
	if it.Progress == nil || it.Progress.PositionMS != 5150 {
		t.Errorf("Progress = %+v, want 5150 preserved through a missing scan", it.Progress)
	}
}

func TestReappearingFileClearsMissing(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	path := writeFile(t, root, "flaky.mkv", 10)

	scanAndWait(t, sc, lib)
	known, _ := st.KnownFiles(ctx, lib.ID)
	id := known[path].ID

	os.Remove(path)
	scanAndWait(t, sc, lib)

	writeFile(t, root, "flaky.mkv", 10)
	scanAndWait(t, sc, lib)

	it, _ := st.GetItem(ctx, id, "local")
	if it.Missing {
		t.Error("Missing = true after the file returned, want false")
	}
}

// Scanning produces pending items and enrichment consumes them. Without this
// callback a fresh scan stays unenriched until the next restart, and metadata
// appears to simply not work.
func TestOnFinishFiresAfterScan(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "a.mkv", 10)

	done := make(chan struct{}, 1)
	sc.OnFinish(func() { done <- struct{}{} })

	scanAndWait(t, sc, lib)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnFinish was never called after a completed scan")
	}
}

// The callback must run outside the scanner's lock, or anything it does that
// touches scan state deadlocks.
func TestOnFinishCanReadScannerState(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "a.mkv", 10)

	done := make(chan Progress, 1)
	sc.OnFinish(func() { done <- sc.Status(lib.ID) })

	scanAndWait(t, sc, lib)

	select {
	case p := <-done:
		if p.State == StateRunning {
			t.Errorf("state seen by callback = %q, want a finished state", p.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback deadlocked reading scanner state")
	}
}

func TestStatusIdleBeforeAnyScan(t *testing.T) {
	sc, _ := newScanner(t)
	if p := sc.Status(42); p.State != StateIdle {
		t.Errorf("State = %q, want idle", p.State)
	}
}

func TestScanEmptyLibrary(t *testing.T) {
	sc, st := newScanner(t)
	lib, _ := fixture(t, sc, st)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 0 || p.ItemsChanged != 0 {
		t.Errorf("empty scan = %+v, want zeroes", p)
	}
	if p.State != StateIdle {
		t.Errorf("State = %q, want idle", p.State)
	}
}

func TestScanRecordsLibraryScannedAt(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "a.mkv", 10)

	scanAndWait(t, sc, lib)

	got, err := st.GetLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScannedAt == nil {
		t.Error("ScannedAt is nil after a completed scan")
	}
}

// TestRescanClearsStaleSidecarLink reproduces the shared-Subs/ bug's aftermath:
// two films in one directory sharing a language-only Subs/English.srt, where the
// old scanner had linked that file to a film it does not belong to. A rescan
// must remove the stale sidecar row (FindSidecars no longer claims it) while
// leaving a genuinely downloaded subtitle untouched.
func TestRescanClearsStaleSidecarLink(t *testing.T) {
	ctx := context.Background()
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)

	writeFile(t, root, "Film A (2019).mkv", 10)
	writeFile(t, root, "Film B (2020).mkv", 10)
	writeFile(t, root, "Subs/English.srt", 5)

	scanAndWait(t, sc, lib)

	items, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	var itemID int64
	for _, it := range items {
		if it.Title == "Film A" {
			itemID = it.ID
		}
	}
	if itemID == 0 {
		t.Fatalf("Film A not found among %v", func() []string {
			out := []string{}
			for _, it := range items {
				out = append(out, it.Title)
			}
			return out
		}())
	}

	// Simulate the old buggy state: the shared subtitle linked to Film A, plus a
	// legitimately downloaded subtitle that must survive the rescan.
	stalePath := filepath.Join(root, "Subs", "English.srt")
	if err := st.ReplaceSidecarSubtitles(ctx, itemID, []store.ExternalSubtitle{
		{ItemID: itemID, Path: stalePath, Language: "en", Format: "srt", Source: "sidecar"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSubtitle(ctx, store.ExternalSubtitle{
		ItemID: itemID, Path: filepath.Join(root, "downloaded.en.srt"),
		Language: "en", Format: "srt", Source: "downloaded",
	}); err != nil {
		t.Fatal(err)
	}

	// Rescan. syncSubtitles runs even for byte-identical files, so this exercises
	// the real reconciliation path.
	scanAndWait(t, sc, lib)

	subs, err := st.ExternalSubtitles(ctx, itemID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range subs {
		if s.Source == "sidecar" {
			t.Errorf("stale sidecar link survived rescan: %+v", s)
		}
	}
	var downloaded int
	for _, s := range subs {
		if s.Source == "downloaded" {
			downloaded++
		}
	}
	if downloaded != 1 {
		t.Errorf("downloaded subtitle count = %d, want 1 (rescan must not delete it)", downloaded)
	}
}

// Scan issues must carry a library-relative path (never the absolute server
// layout) and the recorded list must stay bounded while the count keeps rising.
func TestScanRecordsIssuesRelativeAndCapped(t *testing.T) {
	sc, _ := newScanner(t)
	p := &Progress{}
	root := filepath.Join("C:", "media")

	for i := 0; i < maxIssues+10; i++ {
		sc.recordIssue(p, root, filepath.Join(root, "sub", "clip.mkv"), "unreadable")
	}

	if p.Skipped != maxIssues+10 {
		t.Errorf("Skipped = %d, want %d (count keeps rising past the cap)", p.Skipped, maxIssues+10)
	}
	if len(p.Issues) != maxIssues {
		t.Errorf("Issues len = %d, want capped at %d", len(p.Issues), maxIssues)
	}
	want := filepath.Join("sub", "clip.mkv")
	if p.Issues[0].Path != want {
		t.Errorf("issue path = %q, want library-relative %q", p.Issues[0].Path, want)
	}
	if filepath.IsAbs(p.Issues[0].Path) {
		t.Errorf("issue leaked an absolute path: %q", p.Issues[0].Path)
	}
}

func keys(m map[string]store.Item) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
