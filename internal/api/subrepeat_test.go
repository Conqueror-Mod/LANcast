package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

/*
 * Downloading a subtitle that is already there (R07-4).
 *
 * The same film fetching the same file_id again re-records the same path, and
 * the answer used to be "external-" plus whatever the connection last
 * inserted — another film's subtitle — so the player asked for a track this
 * item does not have and showed nothing. The sequence is the entry's: 7 for
 * A, 8 for B, 7 for A again.
 */
func TestARepeatSubtitleDownloadAnswersWithItsOwnKey(t *testing.T) {
	h := newHarness(t)
	var fake *httptest.Server
	fake = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"link":"` + fake.URL + `/file.srt","file_name":"film.srt","remaining":10}`))
		case "/file.srt":
			_, _ = w.Write([]byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	h.srvAPI.subtitleBaseURL = fake.URL
	set := h.settings.Get()
	set.OpenSubtitlesKey = "test-key"
	if err := h.settings.Set(set); err != nil {
		t.Fatal(err)
	}

	filmA := h.addFile(t, "a.mkv", []byte("x"))
	filmB := h.addFile(t, "b.mkv", []byte("x"))
	download := func(item int64, fileID int) string {
		t.Helper()
		resp := h.do(t, "POST", "/api/items/"+itoa(item)+"/subtitles/download",
			map[string]any{"file_id": fileID, "language": "en"})
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			t.Fatalf("download %d for item %d: status = %d", fileID, item, resp.StatusCode)
		}
		var body struct {
			Key string `json:"key"`
		}
		decode(t, resp, &body)
		return body.Key
	}

	first := download(filmA, 7)
	unrelated := download(filmB, 8)
	again := download(filmA, 7)
	if again != first {
		t.Errorf("first=%s unrelated=%s again=%s; the repeat must answer with the first key", first, unrelated, again)
	}

	// The key must also name this film's track. Removing it goes through the
	// same item-scoped lookup serving does (ExternalSubtitle(item, id)), and
	// answers 204 only for a track this film has. (Serving is not used here:
	// the harness wires no subtitle extractor.)
	resp := h.do(t, "DELETE", "/api/items/"+itoa(filmA)+"/subtitles/"+again, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE the repeat's key %s on its own film: status = %d, want 204", again, resp.StatusCode)
	}
}
