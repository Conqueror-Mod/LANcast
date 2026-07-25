package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

// addDownloadedSub registers a downloaded subtitle with a real file under the
// server's data directory, mirroring what downloadSubtitle writes.
func (h *harness) addDownloadedSub(t *testing.T, itemID int64) (subID int64, path string) {
	t.Helper()
	dir := filepath.Join(h.dataDir, "subtitles", "downloaded", itoa(itemID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "555.en.srt")
	if err := os.WriteFile(path, []byte("WEBVTT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subID, err := h.st.AddSubtitle(context.Background(), store.ExternalSubtitle{
		ItemID: itemID, Path: path, Language: "en", Format: "srt", Source: "downloaded",
	})
	if err != nil {
		t.Fatal(err)
	}
	return subID, path
}

func TestDeleteDownloadedSubtitleRemovesRowAndFile(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Movie.mkv", make([]byte, 16))
	subID, path := h.addDownloadedSub(t, id)

	resp := h.do(t, "DELETE", "/api/items/"+itoa(id)+"/subtitles/external-"+itoa(subID), nil)
	if resp.StatusCode != 204 {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still present after delete: %v", err)
	}
	subs, err := h.st.ExternalSubtitles(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Errorf("row survived delete: %+v", subs)
	}
}

// A sidecar lives in the user's library; deleting it is the line the scanner
// refuses to cross, so the endpoint must refuse too and leave the row intact.
func TestDeleteRefusesSidecar(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Movie.mkv", make([]byte, 16))
	sidecar := filepath.Join(h.dir, "Movie.en.srt")
	os.WriteFile(sidecar, []byte("x"), 0o644)
	subID, err := h.st.AddSubtitle(context.Background(), store.ExternalSubtitle{
		ItemID: id, Path: sidecar, Language: "en", Format: "srt", Source: "sidecar",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(id)+"/subtitles/external-"+itoa(subID), nil),
		403, "forbidden")

	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("sidecar file was touched: %v", err)
	}
	subs, _ := h.st.ExternalSubtitles(context.Background(), id)
	if len(subs) != 1 {
		t.Errorf("sidecar row was removed: %+v", subs)
	}
}

// An embedded track lives inside the video and has no row to delete.
func TestDeleteRejectsEmbeddedKey(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Movie.mkv", make([]byte, 16))
	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(id)+"/subtitles/embedded-0", nil),
		400, "bad_request")
}

// A subtitle id from another item must not be deletable through this item.
func TestDeleteSubtitleScopedToItem(t *testing.T) {
	h := newHarness(t)
	a := h.addFile(t, "A.mkv", make([]byte, 16))
	b := h.addFile(t, "B.mkv", make([]byte, 16))
	subID, path := h.addDownloadedSub(t, a)

	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(b)+"/subtitles/external-"+itoa(subID), nil),
		404, "not_found")

	if _, err := os.Stat(path); err != nil {
		t.Errorf("cross-item delete removed the file: %v", err)
	}
}
