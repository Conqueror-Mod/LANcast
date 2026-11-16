package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/optimize"
)

/*
 * The sweep deletes only copies (R03-3).
 *
 * It took any file whose name began with digits and ".mp4" for the copy of
 * that item id, so somebody's own 300.mp4 or 1917.mp4 in the folder went
 * within thirty seconds, and 12.mp4.bak took 12.mp4 with it. Copies now have
 * a name nobody else uses and the sweep matches it whole.
 */
func TestTheSweepLeavesFilesThatAreNotCopies(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	cur := h.settings.Get()
	cur.OptimizedDir = dir
	if err := h.settings.Set(cur); err != nil {
		t.Fatal(err)
	}
	write := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	theirs := []string{write("300.mp4"), write("1917.mp4"), write("0001.mp4"), write("12.mp4"), write("12.mp4.bak")}
	orphan := write(optimize.FileName(41)) // a copy whose row is gone
	orphanPart := write(optimize.FileName(42) + optimize.PartSuffix)

	h.srvAPI.sweepOptimized(context.Background())

	for _, p := range theirs {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("the sweep deleted %s, which is not a copy: %v", filepath.Base(p), err)
		}
	}
	for _, p := range []string{orphan, orphanPart} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("the sweep kept %s, a copy no row holds", filepath.Base(p))
		}
	}
}

// A copy written under the old name keeps working: its ready row proves
// LANcast wrote it, so it is renamed and still offered and served. The same
// old name with no row is left alone.
func TestACopyUnderTheOldNameIsAdoptedOnlyWhereARowVouchesForIt(t *testing.T) {
	h := langHarness(t)
	id := h.addFile(t, "uhd.mkv", []byte("original"))
	h.probeAsHEVCWithTwoAudioTracks(t, id)
	path := readyCopy(t, h, id, "the copy")
	legacy := filepath.Join(filepath.Dir(path), optimize.LegacyFileName(id))
	if err := os.Rename(path, legacy); err != nil {
		t.Fatal(err)
	}
	stranger := filepath.Join(filepath.Dir(path), optimize.LegacyFileName(id+1000))
	if err := os.WriteFile(stranger, []byte("not a copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	var a struct {
		Version string `json:"version"`
	}
	decode(t, h.authed(t, "GET", fmt.Sprintf("/api/items/%d/playback", id), nil), &a)
	if a.Version != "optimized" {
		t.Fatalf("a copy under the old name is no longer offered: %+v", a)
	}
	resp := h.authed(t, "GET", fmt.Sprintf("/api/stream/%d?version=optimized", id), nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("its stream = %d", resp.StatusCode)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("not renamed to the new name: %v", err)
	}

	h.srvAPI.sweepOptimized(context.Background())
	if _, err := os.Stat(stranger); err != nil {
		t.Errorf("an old-style name with no row was touched: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the sweep removed the adopted copy: %v", err)
	}
}

// A folder being chosen must be empty or hold only copies; the folder already
// in use is not re-checked, so saving other settings still works.
func TestANewOptimizedFolderMustHoldNothingButCopies(t *testing.T) {
	h := langHarness(t)
	put := func(dir string) int {
		return status(t, h.authed(t, "PUT", "/api/settings", map[string]any{"optimized_dir": dir}))
	}

	videos := t.TempDir()
	if err := os.WriteFile(filepath.Join(videos, "Holiday.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := put(videos); code != http.StatusBadRequest {
		t.Errorf("a folder holding somebody's video = %d, want 400", code)
	}

	copies := t.TempDir()
	if err := os.WriteFile(filepath.Join(copies, optimize.FileName(3)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := put(copies); code != http.StatusOK {
		t.Errorf("a folder holding only copies = %d, want 200", code)
	}
	if code := put(filepath.Join(t.TempDir(), "new")); code != http.StatusOK {
		t.Errorf("a folder that does not exist yet = %d, want 200", code)
	}

	// Something lands in the folder in use; saving it again is not refused.
	inUse := h.settings.Get().OptimizedDir
	if err := os.WriteFile(filepath.Join(inUse, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := put(inUse); code != http.StatusOK {
		t.Errorf("re-saving the folder in use = %d, want 200", code)
	}
}
