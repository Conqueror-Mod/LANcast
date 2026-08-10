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
	// Three files, plus the show and season media_item rows reconciliation
	// creates for the one episode (ADR 0010).
	if total != 5 {
		t.Fatalf("total = %d, want 5 (3 files + Andor show + season)", total)
	}

	byTitle := map[string]store.Item{}
	for _, it := range items {
		byTitle[it.Title] = it
	}

	// The episode is nested: a show and a season exist, and the episode points
	// at the season, so it is no longer loose in the grid.
	show, ok := byTitle["Andor"]
	if !ok || show.Kind != "show" {
		t.Fatalf("no Andor show row; got %v", keys(byTitle))
	}
	if ep := byTitle["Announcement"]; ep.ParentID == nil {
		t.Error("episode has no parent after reconciliation")
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

// The hierarchy is built once and stays stable across rescans: the show and
// season rows are not duplicated, and — the subtle part — they are never marked
// missing even though the walk only ever sees the episode file, not the
// directories those rows are keyed on.
func TestHierarchyStableAcrossRescans(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "Andor/Season 01/Andor.S01E01.mkv", 10)
	writeFile(t, root, "Andor/Season 01/Andor.S01E02.mkv", 10)
	// An episode loose in the show folder, no Season directory.
	writeFile(t, root, "Firefly/Firefly.S01E01.mkv", 10)

	first := scanAndWait(t, sc, lib)
	if first.ItemsMissing != 0 {
		t.Fatalf("first scan missing = %d, want 0", first.ItemsMissing)
	}

	countKind := func(kind string) int {
		items, _, err := st.ListItems(context.Background(),
			store.ItemFilter{LibraryID: lib.ID, Kind: kind})
		if err != nil {
			t.Fatal(err)
		}
		return len(items)
	}
	if got := countKind("show"); got != 2 {
		t.Errorf("shows = %d, want 2 (Andor, Firefly)", got)
	}
	if got := countKind("season"); got != 2 {
		t.Errorf("seasons = %d, want 2 (one each; Firefly's is synthetic)", got)
	}

	second := scanAndWait(t, sc, lib)
	if second.ItemsMissing != 0 {
		t.Errorf("rescan marked %d missing — show/season rows must not be swept", second.ItemsMissing)
	}
	if got := countKind("show"); got != 2 {
		t.Errorf("shows after rescan = %d, want 2 (no duplicates)", got)
	}
	if got := countKind("season"); got != 2 {
		t.Errorf("seasons after rescan = %d, want 2 (no duplicates)", got)
	}

	// Every episode is parented; none is loose at the top level.
	top, _, _ := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	for _, it := range top {
		if it.Kind == "episode" {
			t.Errorf("episode %q is top-level after reconciliation", it.Title)
		}
	}
}

// Two files that share a work title and carry explicit part markers group into
// a single multi-part work: a container 'movie' parent with 'part' children,
// ordered, and dropped from the top-level grid (ADR 0017). A standalone film is
// left alone.
func TestMultiPartGrouping(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	writeFile(t, root, "Baahubali/Baahubali Part 1.mkv", 10)
	writeFile(t, root, "Baahubali/Baahubali Part 2.mkv", 10)
	// A lone "Part 1" with no sibling must stay an ordinary movie.
	writeFile(t, root, "Solo/Some Film Part 1.mkv", 10)
	// An ordinary film with no part marker.
	writeFile(t, root, "Arrival (2016).mkv", 10)

	scanAndWait(t, sc, lib)
	ctx := context.Background()

	kinds := map[string]int{}
	all, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]store.Item{}
	for _, it := range all {
		kinds[it.Kind]++
		byTitle[it.Title] = it
	}
	// One work container, two parts, two standalone movies (lone part + Arrival).
	if kinds["part"] != 2 {
		t.Errorf("part rows = %d, want 2 (%v)", kinds["part"], keys(byTitle))
	}
	work, ok := byTitle["Baahubali"]
	if !ok || work.Kind != "movie" {
		t.Fatalf("no Baahubali work container; got %v", keys(byTitle))
	}

	// The work is top-level; its parts are nested and ordered.
	top, _, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	for _, it := range top {
		if it.Kind == "part" {
			t.Errorf("part %q is top-level, should be nested", it.Title)
		}
	}
	parts, err := st.Children(ctx, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Episode == nil || *parts[0].Episode != 1 {
		t.Errorf("parts = %+v, want two ordered by part number", parts)
	}

	// The lone part and Arrival stayed ordinary top-level movies.
	if it := byTitle["Some Film Part 1"]; it.Kind != "movie" {
		t.Errorf("lone part became %q, want movie (needs a sibling to group)", it.Kind)
	}

	// The work is a container: AttachChildCounts reports its parts.
	counts := []store.Item{work}
	if err := st.AttachChildCounts(ctx, counts); err != nil {
		t.Fatal(err)
	}
	if counts[0].ChildCount != 2 {
		t.Errorf("work ChildCount = %d, want 2", counts[0].ChildCount)
	}
}

// Chaptered files group into a serial: a 'serial' container with ordered
// 'chapter' children (ADR 0017), distinct from the 'movie'/'part' shape a
// multi-part film gets. A single library can hold both without them colliding.
func TestSerialGrouping(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	for _, c := range []string{"1", "2", "3"} {
		writeFile(t, root, "Batman/Batman Chapter "+c+".mkv", 10)
	}
	// A multi-part film alongside, to prove the two passes stay separate.
	writeFile(t, root, "Baahubali/Baahubali Part 1.mkv", 10)
	writeFile(t, root, "Baahubali/Baahubali Part 2.mkv", 10)

	scanAndWait(t, sc, lib)
	ctx := context.Background()

	all, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	byTitle := map[string]store.Item{}
	for _, it := range all {
		kinds[it.Kind]++
		byTitle[it.Title] = it
	}
	if kinds["serial"] != 1 || kinds["chapter"] != 3 {
		t.Errorf("kinds = %v, want 1 serial + 3 chapters", kinds)
	}
	if kinds["movie"] != 1 || kinds["part"] != 2 {
		t.Errorf("kinds = %v, want the Baahubali work (1 movie) + 2 parts", kinds)
	}

	serial, ok := byTitle["Batman"]
	if !ok || serial.Kind != "serial" {
		t.Fatalf("no Batman serial; got %v", keys(byTitle))
	}
	chapters, err := st.Children(ctx, serial.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters) != 3 || chapters[0].Kind != "chapter" || chapters[0].Episode == nil || *chapters[0].Episode != 1 {
		t.Errorf("chapters = %+v, want three ordered chapter rows", chapters)
	}

	// Neither container is loose in the top-level grid.
	top, _, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	tops := map[string]bool{}
	for _, it := range top {
		tops[it.Kind] = true
	}
	if tops["chapter"] || tops["part"] {
		t.Error("a chapter or part leaked into the top-level grid")
	}
}

// In a show library, a miniseries named with bare episode markers ("Storm of
// the Century E2/E3") becomes a real show with episodes — the shape that matches
// against TMDB TV. In a movie library the same files would stay films. This is
// the library-kind fix for the Nat-Geo-mismatch bug.
func TestShowLibraryGroupsBareEpisodesIntoShow(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "TV", "show", root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "Storm of the Century/Storm of the Century E2.mkv", 10)
	writeFile(t, root, "Storm of the Century/Storm of the Century E3.mkv", 10)

	scanAndWait(t, sc, *lib)
	ctx := context.Background()

	all, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	byTitle := map[string]store.Item{}
	for _, it := range all {
		kinds[it.Kind]++
		byTitle[it.Title] = it
	}
	// A show, a season, and two episodes — the enrichable TV shape.
	if kinds["show"] != 1 || kinds["episode"] != 2 {
		t.Errorf("kinds = %v, want 1 show + 2 episodes", kinds)
	}
	show, ok := byTitle["Storm of the Century"]
	if !ok || show.Kind != "show" {
		t.Fatalf("no Storm of the Century show; got %v", keys(byTitle))
	}
	// Top-level shows the show only; episodes are nested and match against TV.
	top, _, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, TopLevel: true})
	for _, it := range top {
		if it.Kind == "episode" {
			t.Errorf("episode %q is loose at top level", it.Title)
		}
	}
}

// An ignored path is skipped by the scanner: the file stays on disk, but a
// rescan never re-adds it to the library. This is the non-destructive removal.
func TestScanSkipsIgnoredPaths(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)
	ctx := context.Background()
	writeFile(t, root, "Keep.mkv", 10)
	ignorePath := writeFile(t, root, "Ignore Me.mkv", 10)

	scanAndWait(t, sc, lib)
	known, _ := st.KnownFiles(ctx, lib.ID)
	if len(known) != 2 {
		t.Fatalf("first scan found %d files, want 2", len(known))
	}

	// Remove the title the way the ignore mode does: record the path, drop the
	// row. The file is left on disk.
	if err := st.IgnorePaths(ctx, lib.ID, []string{ignorePath}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteItems(ctx, []int64{known[ignorePath].ID}); err != nil {
		t.Fatal(err)
	}

	scanAndWait(t, sc, lib)
	after, _, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if len(after) != 1 || after[0].Title != "Keep" {
		t.Errorf("after ignore + rescan = %v, want only Keep", titlesOf(after))
	}
	if _, err := os.Stat(ignorePath); err != nil {
		t.Errorf("ignored file was removed from disk: %v", err)
	}
}

func titlesOf(items []store.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// A rescan re-interprets unchanged files whose classification has moved
// families — the retroactive case. Content scanned before a library was typed as
// shows (so it became a movie work with parts) is converted to a show with
// episodes on the next scan, without a byte changing, and the now-empty work is
// pruned.
func TestRescanReinterpretsChangedKind(t *testing.T) {
	sc, st := newScanner(t)
	ctx := context.Background()
	root := t.TempDir()
	lib, err := st.CreateLibrary(ctx, "TV", "show", root)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the pre-fix state: the two files stored as a movie work's parts.
	work, _, err := st.EnsureWork(ctx, lib.ID, "storm", "Storm of the Century", "storm of the century")
	if err != nil {
		t.Fatal(err)
	}
	for i, rel := range []string{"Storm/Storm of the Century Part 1.mkv", "Storm/Storm of the Century Part 2.mkv"} {
		path := writeFile(t, root, rel, 10)
		info, _ := os.Stat(path)
		id, err := st.UpsertItem(ctx, store.ScanFile{
			LibraryID: lib.ID, Path: path, Kind: "movie", Title: "Storm of the Century Part",
			SortTitle: "storm", Container: "mkv", SizeBytes: info.Size(), MTime: info.ModTime().Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.PromoteToChild(ctx, id, work, "part", i+1); err != nil {
			t.Fatal(err)
		}
	}

	// Rescan: bytes are unchanged, but a show library reads "Part N" as an
	// episode, so the files are reinterpreted and regrouped.
	scanAndWait(t, sc, *lib)

	all, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, it := range all {
		kinds[it.Kind]++
	}
	if kinds["episode"] != 2 || kinds["show"] != 1 {
		t.Errorf("kinds = %v, want 2 episodes + 1 show after reinterpretation", kinds)
	}
	if kinds["part"] != 0 {
		t.Errorf("still %d part rows, want 0", kinds["part"])
	}
	// The old movie work is orphaned and pruned.
	if kinds["movie"] != 0 {
		t.Errorf("empty movie work not pruned: %v", kinds)
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

// musicFixture is fixture() for a music library (ADR 0024).
func musicFixture(t *testing.T, st *store.Store) (store.Library, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Music", "music", root)
	if err != nil {
		t.Fatal(err)
	}
	return *lib, root
}

// A music library indexes audio and nothing else. Before ADR 0024 the API
// accepted kind "music" and the scanner gated on IsVideo, so the library
// scanned successfully and stayed permanently empty.
func TestScanMusicLibraryFindsAudio(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := musicFixture(t, st)

	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/01 Mr Self Destruct.mp3", 10)
	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/02 Piggy.flac", 10)
	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/03 Heresy.m4a", 10)
	writeFile(t, root, "Portishead/Dummy/01 Mysterons.opus", 10)

	// Not music: artwork, notes, and the concert video a band ships with an
	// album. The video is the interesting one — it is a file the *movie*
	// scanner would happily index.
	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/cover.jpg", 5)
	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/notes.txt", 5)
	writeFile(t, root, "Nine Inch Nails/The Downward Spiral/live at wembley.mkv", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 4 {
		t.Errorf("FilesSeen = %d, want 4 — only the audio files", p.FilesSeen)
	}

	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]bool{}
	for _, it := range items {
		byTitle[it.Title] = true
	}
	for _, want := range []string{"Mr Self Destruct", "Piggy", "Heresy", "Mysterons"} {
		if !byTitle[want] {
			t.Errorf("track %q missing from the library; got %v", want, titles(items))
		}
	}
	for _, unwanted := range []string{"live at wembley", "cover", "notes"} {
		if byTitle[unwanted] {
			t.Errorf("%q was indexed into a music library", unwanted)
		}
	}
}

// The mirror image: a movie library must not absorb the soundtrack sitting
// beside the film.
func TestScanMovieLibrarySkipsAudio(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)

	writeFile(t, root, "Aladdin (1992)/Aladdin (1992).mkv", 10)
	writeFile(t, root, "Aladdin (1992)/soundtrack/01 A Whole New World.mp3", 10)
	writeFile(t, root, "Aladdin (1992)/soundtrack/02 Friend Like Me.flac", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 1 {
		t.Errorf("FilesSeen = %d, want 1 — the soundtrack is not part of a movie library", p.FilesSeen)
	}
}

func titles(items []store.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// The bug this exists for: a music library created with the wrong kind discards
// every track and the scan reports zero items, indistinguishable from an empty
// folder. The count is what makes that state explain itself.
func TestAudioInAMovieLibraryIsCountedNotSilent(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st) // kind: movie

	writeFile(t, root, "A's/ABBA/Arrival/01 Dancing Queen.flac", 10)
	writeFile(t, root, "A's/ABBA/Arrival/02 Money.mp3", 10)
	writeFile(t, root, "B's/Beatles/Abbey Road/01 Come Together.m4a", 10)

	p := scanAndWait(t, sc, lib)

	if p.FilesSeen != 0 || p.ItemsChanged != 0 {
		t.Fatalf("FilesSeen = %d, ItemsChanged = %d, want 0 and 0 — a movie library must not index audio",
			p.FilesSeen, p.ItemsChanged)
	}
	if p.SkippedKind != 3 {
		t.Errorf("SkippedKind = %d, want 3 — the whole point is that this number is not silence", p.SkippedKind)
	}
	// Not a failure, so it must not inflate the diagnostic that means "something
	// went wrong reading these".
	if p.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0 — excluded-by-kind is not a processing failure", p.Skipped)
	}
	if len(p.Issues) != 0 {
		t.Errorf("Issues = %v, want none — 1,592 of these would bury the real ones", p.Issues)
	}
	_ = st
}

// The counter names media of the other sort, not everything a library ignores.
// Artwork and sidecars are ignored by every library, and counting them would
// report a large number on a perfectly healthy library.
func TestNonMediaFilesAreNotCountedAsExcludedByKind(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := fixture(t, sc, st)

	writeFile(t, root, "Films/The.Matrix.1999.mkv", 10)
	writeFile(t, root, "Films/The.Matrix.1999.nfo", 5)
	writeFile(t, root, "Films/poster.jpg", 5)
	writeFile(t, root, "Films/The.Matrix.1999.en.srt", 5)
	writeFile(t, root, "Films/notes.txt", 5)

	p := scanAndWait(t, sc, lib)

	if p.FilesSeen != 1 {
		t.Fatalf("FilesSeen = %d, want 1", p.FilesSeen)
	}
	if p.SkippedKind != 0 {
		t.Errorf("SkippedKind = %d, want 0 — artwork, sidecars and subtitles are not excluded media", p.SkippedKind)
	}
	_ = st
}

// The mirror case: a music library ignoring the video a band shipped with an
// album. Same mechanism, and the one that proves the count is not "audio".
func TestVideoInAMusicLibraryIsCounted(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Music", "music", root)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "Album/01 Track.flac", 10)
	writeFile(t, root, "Album/making-of.mkv", 10)

	p := scanAndWait(t, sc, *lib)

	if p.FilesSeen != 1 {
		t.Fatalf("FilesSeen = %d, want 1 (the track)", p.FilesSeen)
	}
	if p.SkippedKind != 1 {
		t.Errorf("SkippedKind = %d, want 1 (the video)", p.SkippedKind)
	}
}

// pictureFixture is the reference library's shape: flat folders of images with
// meaningless filenames, plus the junk that sits beside real photos.
func pictureFixture(t *testing.T, st *store.Store) (store.Library, string) {
	t.Helper()
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Pictures", "picture", root)
	if err != nil {
		t.Fatal(err)
	}
	return *lib, root
}

func TestPictureLibraryGroupsFoldersIntoGalleries(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	writeFile(t, root, "AI Art/0f3be347-6bdf-4f4f-880d-93a5c24d8eda.png", 10)
	writeFile(t, root, "AI Art/openart-f81b7650ced542cdb5b37d8916f0bc92_raw.jpg", 10)
	writeFile(t, root, "Wallpaper/4k-mountain.webp", 10)
	// Not an image, and not media of any other sort either: ignored silently
	// and not counted, exactly as artwork is in a movie library.
	writeFile(t, root, "AI Art/archive.7z", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 3 {
		t.Fatalf("FilesSeen = %d, want 3", p.FilesSeen)
	}
	if p.SkippedKind != 0 {
		t.Errorf("SkippedKind = %d, want 0 — a .7z is not media of the other sort", p.SkippedKind)
	}

	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	galleries := map[string]store.Item{}
	photos := 0
	for _, it := range items {
		switch it.Kind {
		case "gallery":
			galleries[it.Title] = it
		case "photo":
			photos++
		default:
			t.Errorf("unexpected kind %q for %q", it.Kind, it.Title)
		}
	}
	if photos != 3 {
		t.Errorf("photos = %d, want 3", photos)
	}
	if len(galleries) != 2 {
		t.Fatalf("galleries = %v, want AI Art and Wallpaper", galleries)
	}
	if _, ok := galleries["AI Art"]; !ok {
		t.Error(`no gallery titled "AI Art"`)
	}
}

// The filename is the title, untouched. `clean` strips years, quality markers
// and release-group noise — all video conventions — and running a UUID through
// it produces a different meaningless string with less in it.
func TestPhotoTitleIsTheFilenameVerbatim(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	writeFile(t, root, "Wallpaper/openart-f81b7650ced542cdb5b37d8916f0bc92_raw.jpg", 10)
	// Contains a year and a quality marker. In a movie library both would be
	// stripped; here they are part of the name someone gave the file.
	writeFile(t, root, "Wallpaper/sunset 2019 1080p.png", 10)

	scanAndWait(t, sc, lib)

	items, _, err := st.ListItems(context.Background(),
		store.ItemFilter{LibraryID: lib.ID, Kind: "photo"})
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, it := range items {
		titles[it.Title] = true
		if it.Year != nil {
			t.Errorf("%q has year %d — a picture library does not read years from names", it.Title, *it.Year)
		}
	}
	for _, want := range []string{
		"openart-f81b7650ced542cdb5b37d8916f0bc92_raw",
		"sunset 2019 1080p",
	} {
		if !titles[want] {
			t.Errorf("missing title %q; got %v", want, titles)
		}
	}
}

// A photo loose in the library root is already top-level. Wrapping the root in
// a gallery would add a level of navigation that contains everything and
// separates nothing.
func TestPhotosInTheLibraryRootGetNoGallery(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	writeFile(t, root, "loose.jpg", 10)
	writeFile(t, root, "Folder/inside.jpg", 10)

	scanAndWait(t, sc, lib)

	items, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == "photo" && it.Title == "loose" && it.ParentID != nil {
			t.Error("a photo in the library root was given a parent")
		}
		if it.Kind == "photo" && it.Title == "inside" && it.ParentID == nil {
			t.Error("a photo inside a folder was left without a gallery")
		}
	}
}

// A rescan must be a no-op. Galleries are derived every pass, so a reconciler
// that re-parented or re-created them would churn ids on every scan.
func TestRescanOfAPictureLibraryChangesNothing(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	writeFile(t, root, "AI Art/one.png", 10)
	writeFile(t, root, "AI Art/two.png", 10)

	scanAndWait(t, sc, lib)
	before, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}

	p := scanAndWait(t, sc, lib)
	if p.ItemsChanged != 0 {
		t.Errorf("ItemsChanged = %d on a rescan, want 0", p.ItemsChanged)
	}
	after, _, err := st.ListItems(context.Background(), store.ItemFilter{LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("item count moved from %d to %d across a rescan", len(before), len(after))
	}
	ids := map[int64]bool{}
	for _, it := range before {
		ids[it.ID] = true
	}
	for _, it := range after {
		if !ids[it.ID] {
			t.Errorf("%q (%s) has a new id after a rescan", it.Title, it.Kind)
		}
	}
}

// The mirror of the music case: video dropped in a picture library is media of
// the other sort, so it is counted rather than discarded in silence.
func TestVideoInAPictureLibraryIsCounted(t *testing.T) {
	sc, st := newScanner(t)
	lib, root := pictureFixture(t, st)

	writeFile(t, root, "Holiday/beach.jpg", 10)
	writeFile(t, root, "Holiday/clip.mp4", 10)

	p := scanAndWait(t, sc, lib)
	if p.FilesSeen != 1 {
		t.Fatalf("FilesSeen = %d, want 1 (the photo)", p.FilesSeen)
	}
	if p.SkippedKind != 1 {
		t.Errorf("SkippedKind = %d, want 1 (the video)", p.SkippedKind)
	}
}

// Found by the picture rescan test, and it was never about pictures: the
// reinterpretation check had explicit cases for episode and movie and a default
// that assumed anything else was "other". A track parses as KindTrack, hits the
// default, and "track" != "other" — so every rescan re-recorded every track and
// cleared its metadata stamp, re-queueing the whole library for enrichment.
func TestRescanOfAMusicLibraryChangesNothing(t *testing.T) {
	sc, st := newScanner(t)
	root := t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Music", "music", root)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "ABBA/Arrival/01 Dancing Queen.flac", 10)
	writeFile(t, root, "ABBA/Arrival/02 Money.mp3", 10)

	scanAndWait(t, sc, *lib)
	p := scanAndWait(t, sc, *lib)

	if p.ItemsChanged != 0 {
		t.Errorf("ItemsChanged = %d on a rescan, want 0 — every track was re-recorded", p.ItemsChanged)
	}
}
