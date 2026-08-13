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
 * Containment resolves against the item's own location (ADR 0034).
 *
 * These are the tests for the boundary, not for the feature. The feature is
 * that a library can live in two places; the boundary is that a row must only
 * ever reach files inside the location it was scanned under, and the way
 * multi-root gets that wrong is by asking "does *any* of this library's roots
 * contain the path" — which accepts a row pointing under the wrong one on the
 * strength of some root matching.
 *
 * So each of these gives a library two real locations and then tries to reach
 * across.
 */

// twoRootHarness gives h.lib a second location on disk, with a real file in it,
// and returns that root's directory plus the item id of the file.
func twoRootHarness(t *testing.T, h *harness) (string, int64) {
	t.Helper()
	ctx := context.Background()

	dir := t.TempDir()
	root, err := h.st.AddRoot(ctx, h.lib.ID, dir)
	if err != nil {
		t.Fatalf("AddRoot: %v", err)
	}

	path := filepath.Join(dir, "second.mkv")
	if err := os.WriteFile(path, []byte("second-root-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, RootID: root.ID, Path: path, Kind: "movie",
		Title: "second", SortTitle: "second", Container: "mkv",
		SizeBytes: 17, MTime: 1,
	}); err != nil {
		t.Fatalf("UpsertItem: %v", err)
	}
	known, _ := h.st.KnownFilesInRoot(ctx, root.ID)
	return dir, known[path].ID
}

// A file in the second location must stream. Resolving against the library's
// *first* root would fail containment and 404 it — the regression that would
// make multi-root libraries half-broken rather than insecure.
func TestStreamServesAFileFromASecondRoot(t *testing.T) {
	h := newHarness(t)
	_, id := twoRootHarness(t, h)

	res := h.do(t, http.MethodGet, "/api/stream/"+itoa(id), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 200/206 — a second location must be servable", res.StatusCode)
	}
}

// The boundary itself. A row whose path points into the *other* root of the
// same library must not resolve: it is outside the location that item belongs
// to, and "some root of this library contains it" is not the question.
func TestItemCannotReachAcrossIntoAnotherRoot(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	otherDir, _ := twoRootHarness(t, h)

	// A file that genuinely exists, in a root that genuinely belongs to this
	// same library — but not to this item.
	victim := filepath.Join(otherDir, "victim.mkv")
	if err := os.WriteFile(victim, []byte("not yours"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The row is registered under the *first* root while pointing at a file in
	// the second. This is the shape of a bad row, and the shape a search over
	// roots would accept.
	roots, err := h.st.ListRoots(ctx, h.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, RootID: roots[0].ID, Path: victim, Kind: "movie",
		Title: "victim", SortTitle: "victim", Container: "mkv",
		SizeBytes: 9, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFilesInRoot(ctx, roots[0].ID)
	bad := known[victim].ID
	if bad == 0 {
		t.Fatal("setup: bad row not registered")
	}

	res := h.do(t, http.MethodGet, "/api/stream/"+itoa(bad), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 — the row escaped its own location", res.StatusCode)
	}
}

// The classic traversal, still refused. Roots changed what the check resolves
// against, not what it enforces.
func TestItemCannotEscapeItsRootEntirely(t *testing.T) {
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

	res := h.do(t, http.MethodGet, "/api/stream/"+itoa(known[outside].ID), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}
