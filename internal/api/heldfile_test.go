package api

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

/*
 * A film being streamed can still be deleted (R03-4).
 *
 * os.Open on Windows leaves out FILE_SHARE_DELETE, so for as long as a stream
 * held the file — a paused player, a stalled connection, a whole film on one
 * native-player connection — Explorer refused to delete or replace it, and an
 * in-app delete failed. The file is big enough that the handler is still
 * holding it when the first bytes arrive.
 */
func TestAFilmBeingStreamedCanBeDeleted(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Held.mkv", make([]byte, 32<<20))
	path := filepath.Join(h.dir, "Held.mkv")

	for _, route := range []string{"/api/stream/" + itoa(id), "/api/items/" + itoa(id) + "/download"} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, make([]byte, 32<<20), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		resp := h.do(t, "GET", route, nil)
		if resp.StatusCode != 200 {
			resp.Body.Close()
			t.Fatalf("%s: status %d", route, resp.StatusCode)
		}
		if _, err := io.ReadFull(resp.Body, make([]byte, 4096)); err != nil {
			t.Fatal(err)
		}
		// Mid-read: the handler has the file open and is blocked writing.
		if err := os.Remove(path); err != nil {
			resp.Body.Close()
			t.Fatalf("%s: deleting a film mid-stream: %v", route, err)
		}
		resp.Body.Close()
	}
}

/*
 * A delete that cannot remove the file keeps the row (R03-4).
 *
 * It used to log the failure and drop the row anyway, so the file stayed, the
 * entry went, and the next scan brought it back as a stranger with none of its
 * progress, edits or locks.
 */
func TestADeleteThatCannotRemoveTheFileKeepsTheRow(t *testing.T) {
	h := newHarness(t)
	stuck := h.addFile(t, "Stuck.mkv", make([]byte, 16))
	stuckPath := filepath.Join(h.dir, "Stuck.mkv")

	// Something else holds the file in a way that refuses a delete: on
	// Windows a plain os.Open, elsewhere a folder that cannot be written.
	if runtime.GOOS == "windows" {
		f, err := os.Open(stuckPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
	} else {
		if os.Geteuid() == 0 {
			t.Skip("root deletes from a read-only folder")
		}
		if err := os.Chmod(h.dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(h.dir, 0o755) })
	}

	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(stuck)+"?mode=delete", nil), 409, "not_deleted")
	if _, err := os.Stat(stuckPath); err != nil {
		t.Fatalf("the file went after all: %v", err)
	}
	if resp := h.do(t, "GET", "/api/items/"+itoa(stuck), nil); resp.StatusCode != 200 {
		t.Fatalf("the row went although the file stayed: %d", resp.StatusCode)
	}
}
