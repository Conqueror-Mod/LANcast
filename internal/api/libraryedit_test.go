package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

/*
 * Editing a library.
 *
 * The rename is the easy half. The path is the half worth testing: it exists so
 * a drive letter change does not cost every match, every piece of artwork,
 * every watch position and every playlist that referenced the library — which
 * is what deleting and re-adding it would cost. So what is asserted is that the
 * *contents move with it*, and that the things which must not change do not.
 */

func TestRenameALibrary(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"name": "Films"})
	var lib struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	decode(t, resp, &lib)
	if lib.Name != "Films" {
		t.Errorf("name = %q, want Films", lib.Name)
	}
	if lib.Path != h.dir {
		t.Errorf("renaming moved the library: path = %q, want %q", lib.Path, h.dir)
	}
}

// The one thing that is not editable, and why: a kind decides which scanner
// runs, which provider is asked, and what the top level of the browse is.
// Changing it would leave a library describing itself as something its rows are
// not.
func TestALibrarysKindCannotBeChanged(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"kind": "music"}), 400, "bad_request")

	// Sending the kind it already has is not an attempt to change anything.
	resp := h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"kind": h.lib.Kind, "name": "Still Films"})
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200 — the kind was unchanged", resp.StatusCode)
	}
}

// The drive-letter case, which is the whole reason a path is editable.
func TestRepointingALibraryCarriesItsItems(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	id := h.addFile(t, "film.mkv", []byte("f"))

	// Somewhere real to move to, holding the same file: repointing at a folder
	// that is not there is refused, on the grounds that a typo should not mark
	// a whole library missing.
	newRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(newRoot, "film.mkv"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"path": newRoot})
	var lib struct {
		Path string `json:"path"`
	}
	decode(t, resp, &lib)
	if lib.Path != newRoot {
		t.Fatalf("library path = %q, want %q", lib.Path, newRoot)
	}

	// The item is the same row — same id, same metadata, same watch state —
	// pointing at the new place. A delete-and-re-add would have produced a new
	// id and lost everything hanging off the old one.
	it, err := h.st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(newRoot, "film.mkv")
	if it.Path != want {
		t.Errorf("item path = %q, want %q", it.Path, want)
	}
	if it.Missing {
		t.Error("the item was marked missing; repointing tells the scanner where to look, it does not reconcile files")
	}
}

func TestRepointRefusesAPathThatIsNotThere(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"path": filepath.Join(t.TempDir(), "nowhere")}), 400, "bad_request")

	// And a file is not a library root.
	f := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantError(t, h.do(t, "PATCH", "/api/libraries/"+itoa(h.lib.ID),
		map[string]any{"path": f}), 400, "bad_request")
}

func TestPatchingALibraryThatIsNotThere(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PATCH", "/api/libraries/99999",
		map[string]any{"name": "Ghost"}), 404, "not_found")
}
