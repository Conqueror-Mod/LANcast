package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeDiscord counts what a report sends and keeps the last one's files.
func fakeDiscord(t *testing.T) (*httptest.Server, *int, *map[string]string) {
	t.Helper()
	n := 0
	files := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		}
		n++
		files = map[string]string{"payload": r.FormValue("payload_json")}
		for _, hs := range r.MultipartForm.File {
			f, _ := hs[0].Open()
			b, _ := io.ReadAll(f)
			f.Close()
			files[hs[0].Filename] = string(b)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &n, &files
}

func TestReportAnIssue(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	srv, sent, files := fakeDiscord(t)
	url := ""
	h.srvAPI.reportURL = func() string { return url }
	if err := os.WriteFile(filepath.Join(h.dataDir, "lancastd.log"),
		[]byte("time=… msg=\"started\"\ntime=… msg=\"comic reader blank\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info := func(resp *http.Response) map[string]bool {
		defer resp.Body.Close()
		var m map[string]bool
		json.NewDecoder(resp.Body).Decode(&m)
		return m
	}

	// A build with no webhook says so, and refuses to pretend it sent.
	if m := info(h.authed(t, "GET", "/api/report", nil)); m["can_send"] || !m["can_attach_log"] {
		t.Fatalf("no webhook, admin: %v", m)
	}
	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{"what": "x"}), 503, "unavailable")

	url = srv.URL
	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{"what": "  "}), 400, "bad_request")

	// An administrator's report, with the log.
	resp := h.authed(t, "POST", "/api/report", map[string]any{
		"what": "The reader shows a blank page", "client": "desktop", "include_log": true,
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || *sent != 1 {
		t.Fatalf("send: %d, %d sent", resp.StatusCode, *sent)
	}
	if !strings.Contains((*files)["lancastd-tail.log"], "comic reader blank") ||
		!strings.Contains((*files)["payload"], "The reader shows a blank page") {
		t.Fatalf("sent %v", *files)
	}

	// Straight away again: refused, and nothing sent.
	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{"what": "again"}), 429, "rate_limited")
	if *sent != 1 {
		t.Fatal("a rate-limited report was sent")
	}

	// A member may report, but not attach the server's log.
	h.srvAPI.reports = reportLimiter{}
	m := h.member(t, "Gerda", "another good password")
	r := h.asUser(t, m, "GET", "/api/report", nil)
	if got := info(r); !got["can_send"] || got["can_attach_log"] {
		t.Fatalf("member: %v", got)
	}
	wantError(t, h.asUser(t, m, "POST", "/api/report", map[string]any{"what": "x", "include_log": true}), 403, "forbidden")
	resp = h.asUser(t, m, "POST", "/api/report", map[string]any{"what": "Subtitles are late"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || *sent != 2 {
		t.Fatalf("member send: %d, %d sent", resp.StatusCode, *sent)
	}
	if _, has := (*files)["lancastd-tail.log"]; has || !strings.Contains((*files)["payload"], "member") {
		t.Fatalf("member report: %v", *files)
	}
}

// Five an hour, and one every twenty seconds.
func TestTheReportLimiter(t *testing.T) {
	var l reportLimiter
	now := time.Now()
	for i := 0; i < 5; i++ {
		if ok, _ := l.take(now.Add(time.Duration(i) * time.Minute)); !ok {
			t.Fatalf("report %d refused", i+1)
		}
	}
	if ok, _ := l.take(now.Add(5 * time.Minute)); ok {
		t.Fatal("a sixth in the hour was allowed")
	}
	if ok, _ := l.take(now.Add(61 * time.Minute)); !ok {
		t.Fatal("still refused once the first left the hour")
	}
	if ok, wait := l.take(now.Add(61*time.Minute + 5*time.Second)); ok || wait != 15*time.Second {
		t.Fatalf("within 20 s: %v %v", ok, wait)
	}
}

// Pictures go with a report as files; one that will not open is refused
// before it can use up the sender's turn, and so are more than four.
func TestReportPictures(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	srv, sent, files := fakeDiscord(t)
	h.srvAPI.reportURL = func() string { return srv.URL }

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	pic := buf.Bytes()

	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{
		"what": "x", "images": [][]byte{pic, []byte("not a picture")},
	}), 400, "bad_request")
	wantError(t, h.authed(t, "POST", "/api/report", map[string]any{
		"what": "x", "images": [][]byte{pic, pic, pic, pic, pic},
	}), 400, "bad_request")
	if *sent != 0 {
		t.Fatal("a refused report was sent")
	}

	resp := h.authed(t, "POST", "/api/report", map[string]any{
		"what": "The poster is cut off", "images": [][]byte{pic, pic},
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || *sent != 1 {
		t.Fatalf("send: %d, %d sent", resp.StatusCode, *sent)
	}
	for _, name := range []string{"picture-1.png", "picture-2.png"} {
		if _, ok := (*files)[name]; !ok {
			t.Errorf("%s not attached: %v", name, len(*files))
		}
	}
	if !strings.Contains((*files)["payload"], "2 pictures") {
		t.Errorf("payload: %s", (*files)["payload"])
	}
}
