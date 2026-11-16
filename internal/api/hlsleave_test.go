package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/transcode"
)

/*
 * Leaving a film ends the caller's own encode, and only theirs (T-5).
 *
 * An HLS encode writes segments with nobody reading, so the ten minutes a
 * paused film is given were ten minutes of a full encode after the viewer had
 * gone. The player now says when it leaves.
 */

// encodingSession is a session whose ffmpeg is still writing: no ENDLIST.
func encodingSession(t *testing.T, s *Server, item int64, owner string) *transcode.Session {
	t.Helper()
	sess, err := s.trans.AdoptForTest(item, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sess.Dir, "index.m3u8"), []byte("#EXTM3U\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestLeavingAFilmEndsOnlyTheCallersEncode(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "film.mkv", []byte("x"))
	h.secure(t, "a good long password")

	mine := encodingSession(t, h.srvAPI, item, h.ownID(t))
	theirs := encodingSession(t, h.srvAPI, item, "u_other")

	resp := h.authed(t, "DELETE", "/api/stream/"+itoa64(item)+"/hls", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("leave: status = %d, want 204", resp.StatusCode)
	}
	if h.srvAPI.trans.Session(mine.ID) != nil {
		t.Error("the caller's encode kept running after they left")
	}
	if h.srvAPI.trans.Session(theirs.ID) == nil {
		t.Error("leaving ended another account's viewing of the same film")
	}

	// Leaving again, with nothing running, is still success.
	resp = h.authed(t, "DELETE", "/api/stream/"+itoa64(item)+"/hls", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("second leave: status = %d, want 204", resp.StatusCode)
	}
}

// Deleting the file stops whatever is reading it, so Windows can remove it.
func TestDeletingAFileStopsItsEncodes(t *testing.T) {
	h := newHarness(t)
	item := h.addFile(t, "film.mkv", []byte("x"))
	h.secure(t, "a good long password")
	sess := encodingSession(t, h.srvAPI, item, "u_someone")

	resp := h.authed(t, "DELETE", "/api/items/"+itoa64(item)+"?mode=delete", nil)
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("delete: status = %d", resp.StatusCode)
	}
	if h.srvAPI.trans.Session(sess.ID) != nil {
		t.Error("an encode of a deleted file is still running")
	}
}
