package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

// ---- the library response ---------------------------------------------------

// `path` is the first location, kept so clients that predate multi-root
// libraries keep working. Dropping it would break every one of them.
func TestLibraryCarriesBothPathAndRoots(t *testing.T) {
	h := newHarness(t)
	second := t.TempDir()
	if _, err := h.st.AddRoot(context.Background(), h.lib.ID, second); err != nil {
		t.Fatal(err)
	}

	var body []struct {
		Path  string `json:"path"`
		Roots []struct {
			Path      string `json:"path"`
			ItemCount int    `json:"item_count"`
		} `json:"roots"`
	}
	decode(t, h.do(t, http.MethodGet, "/api/libraries", nil), &body)
	if len(body) != 1 {
		t.Fatalf("libraries = %d, want 1", len(body))
	}
	if len(body[0].Roots) != 2 {
		t.Fatalf("roots = %d, want 2", len(body[0].Roots))
	}
	if body[0].Path != body[0].Roots[0].Path {
		t.Errorf("path = %q but first root = %q; they must agree",
			body[0].Path, body[0].Roots[0].Path)
	}
	if body[0].Roots[1].Path != second {
		t.Errorf("second root = %q, want %q", body[0].Roots[1].Path, second)
	}
}

// ---- adding -----------------------------------------------------------------

func TestAddRootEndpoint(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	res := h.do(t, http.MethodPost, "/api/libraries/"+itoa(h.lib.ID)+"/roots",
		map[string]any{"path": dir})
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}

	roots, err := h.st.ListRoots(context.Background(), h.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Errorf("roots = %d, want 2", len(roots))
	}
}

// Overlap is a conflict rather than a bad request: the path is well formed and
// the objection is about what is already there.
func TestAddRootRefusesAnOverlappingLocation(t *testing.T) {
	h := newHarness(t)
	nested := filepath.Join(h.dir, "inside")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, http.MethodPost, "/api/libraries/"+itoa(h.lib.ID)+"/roots",
		map[string]any{"path": nested})
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", res.StatusCode)
	}
}

// A location that is not there would be skipped by every scan while looking
// configured, so it is refused up front.
func TestAddRootRefusesAMissingDirectory(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, http.MethodPost, "/api/libraries/"+itoa(h.lib.ID)+"/roots",
		map[string]any{"path": filepath.Join(t.TempDir(), "nope")})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
}

// ---- removing ---------------------------------------------------------------

func TestRemoveRootEndpoint(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	dir := t.TempDir()
	root, err := h.st.AddRoot(ctx, h.lib.ID, dir)
	if err != nil {
		t.Fatal(err)
	}

	res := h.do(t, http.MethodDelete,
		"/api/libraries/"+itoa(h.lib.ID)+"/roots/"+itoa(root.ID), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	roots, _ := h.st.ListRoots(ctx, h.lib.ID)
	if len(roots) != 1 {
		t.Errorf("roots = %d, want 1", len(roots))
	}
}

// A library with no location cannot be scanned, resolved or repointed.
func TestRemoveRootRefusesTheLastOne(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	roots, err := h.st.ListRoots(ctx, h.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	res := h.do(t, http.MethodDelete,
		"/api/libraries/"+itoa(h.lib.ID)+"/roots/"+itoa(roots[0].ID), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", res.StatusCode)
	}
}

// A location belonging to another library is not-found rather than forbidden:
// the caller has no business knowing it exists.
func TestRootOfAnotherLibraryIsNotFound(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	otherDir := t.TempDir()
	other, err := h.st.CreateLibrary(ctx, "Other", "movie", otherDir)
	if err != nil {
		t.Fatal(err)
	}
	otherRoots, err := h.st.ListRoots(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}

	res := h.do(t, http.MethodDelete,
		"/api/libraries/"+itoa(h.lib.ID)+"/roots/"+itoa(otherRoots[0].ID), nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

// ---- repointing -------------------------------------------------------------

// One location moves; the other stays exactly where it is.
func TestPatchRootMovesOnlyThatLocation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	oldDir := t.TempDir()
	root, err := h.st.AddRoot(ctx, h.lib.ID, oldDir)
	if err != nil {
		t.Fatal(err)
	}
	newDir := t.TempDir()

	res := h.do(t, http.MethodPatch,
		"/api/libraries/"+itoa(h.lib.ID)+"/roots/"+itoa(root.ID),
		map[string]any{"path": newDir})
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	roots, _ := h.st.ListRoots(ctx, h.lib.ID)
	if roots[0].Path != h.dir {
		t.Errorf("the untouched location moved: %q", roots[0].Path)
	}
	if roots[1].Path != newDir {
		t.Errorf("location = %q, want %q", roots[1].Path, newDir)
	}
}

// A typo must not strand the rows that used to resolve, so the directory is
// checked before anything is rewritten.
func TestPatchRootRefusesAMissingDirectory(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	root, err := h.st.AddRoot(ctx, h.lib.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := h.do(t, http.MethodPatch,
		"/api/libraries/"+itoa(h.lib.ID)+"/roots/"+itoa(root.ID),
		map[string]any{"path": filepath.Join(t.TempDir(), "nope")})
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
}

// ---- creating with several locations ----------------------------------------

func TestCreateLibraryWithSeveralLocations(t *testing.T) {
	h := newHarness(t)
	a, b := t.TempDir(), t.TempDir()

	var body struct {
		ID    int64 `json:"id"`
		Roots []struct {
			Path string `json:"path"`
		} `json:"roots"`
	}
	res := h.do(t, http.MethodPost, "/api/libraries",
		map[string]any{"name": "Split", "kind": "movie", "roots": []string{a, b}})
	if res.StatusCode != http.StatusCreated {
		res.Body.Close()
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	decode(t, res, &body)
	if len(body.Roots) != 2 {
		t.Errorf("roots = %d, want 2", len(body.Roots))
	}
}

// `path` still means what it always did — every existing client sends it.
func TestCreateLibraryStillAcceptsASinglePath(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	var body struct {
		Path  string `json:"path"`
		Roots []struct {
			Path string `json:"path"`
		} `json:"roots"`
	}
	res := h.do(t, http.MethodPost, "/api/libraries",
		map[string]any{"name": "Single", "kind": "movie", "path": dir})
	if res.StatusCode != http.StatusCreated {
		res.Body.Close()
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	decode(t, res, &body)
	if body.Path != dir {
		t.Errorf("path = %q, want %q", body.Path, dir)
	}
	if len(body.Roots) != 1 {
		t.Errorf("roots = %d, want 1", len(body.Roots))
	}
}

// A bad location in the list must not leave a half-made library behind — that
// is the kind of thing somebody scans without noticing.
func TestCreateLibraryRollsBackOnABadSecondLocation(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	good := t.TempDir()

	before, err := h.st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// The second location is nested inside the first, which the store refuses.
	nested := filepath.Join(good, "inside")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, http.MethodPost, "/api/libraries",
		map[string]any{"name": "Bad", "kind": "movie", "roots": []string{good, nested}})
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", res.StatusCode)
	}

	after, err := h.st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("libraries = %d, want %d — a refused create left one behind",
			len(after), len(before))
	}
}

// The store's own guard, reached through the API: a library must always have a
// location, so a rolled-back create must not leave a rootless row either.
func TestRolledBackCreateLeavesNoRootlessLibrary(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	good := t.TempDir()
	nested := filepath.Join(good, "inside")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	res := h.do(t, http.MethodPost, "/api/libraries",
		map[string]any{"name": "Bad", "kind": "movie", "roots": []string{good, nested}})
	res.Body.Close()

	libs, err := h.st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range libs {
		roots, err := h.st.ListRoots(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(roots) == 0 {
			t.Errorf("library %d has no location", l.ID)
		}
	}
	_ = store.LibraryRoot{}
}
