package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/artwork"
	"lancast/internal/config"
	"lancast/internal/enrich"
	"lancast/internal/meta"
	"lancast/internal/scan"
	"lancast/internal/store"
)

type harness struct {
	srv      *httptest.Server
	st       *store.Store
	lib      *store.Library
	dir      string
	art      *artwork.Cache
	reg      *meta.Registry
	settings *config.SettingsStore
	enriched int
	cookie   *http.Cookie
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	dataDir := t.TempDir()
	st, err := store.Open(filepath.Join(dataDir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	settings, err := config.LoadSettings(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	art := artwork.New(filepath.Join(dataDir, "artwork"))
	reg := meta.NewRegistry()
	h := &harness{st: st, art: art, reg: reg, settings: settings}

	api := New(Deps{
		Store: st, Scanner: scan.New(st, log), Registry: reg, Artwork: art,
		Worker: enrich.New(st, reg, art, log), Settings: settings, Log: log,
		Enrich: func() { h.enriched++ },
	})

	h.srv = httptest.NewServer(api.Handler())
	t.Cleanup(h.srv.Close)

	h.dir = t.TempDir()
	lib, err := st.CreateLibrary(context.Background(), "Media", "movie", h.dir)
	if err != nil {
		t.Fatal(err)
	}
	h.lib = lib
	return h
}

// addFile writes a real file into the library and registers it, returning its id.
func (h *harness) addFile(t *testing.T, name string, body []byte) int64 {
	t.Helper()
	path := filepath.Join(h.dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: path, Kind: "movie",
		Title: name, SortTitle: name, Container: "mkv",
		SizeBytes: int64(len(body)), MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFiles(context.Background(), h.lib.ID)
	return known[path].ID
}

func (h *harness) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// wantError asserts the documented error envelope shape and code.
func wantError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Errorf("status = %d, want %d", resp.StatusCode, status)
	}
	var body struct {
		Error apiError `json:"error"`
	}
	decode(t, resp, &body)
	if body.Error.Code != code {
		t.Errorf("error code = %q, want %q", body.Error.Code, code)
	}
	if body.Error.Message == "" {
		t.Error("error message is empty")
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "GET", "/api/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	decode(t, resp, &body)
	if body["status"] != "ok" || body["version"] == "" {
		t.Errorf("body = %v, want ok status and a version", body)
	}
}

func TestCreateLibraryValidation(t *testing.T) {
	h := newHarness(t)
	existing := h.dir
	fileInLib := filepath.Join(h.dir, "a-file.mkv")
	os.WriteFile(fileInLib, []byte("x"), 0o644)

	tests := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"missing name", map[string]any{"kind": "movie", "path": existing}, 400, "bad_request"},
		{"missing path", map[string]any{"name": "x", "kind": "movie"}, 400, "bad_request"},
		{"bad kind", map[string]any{"name": "x", "kind": "bogus", "path": existing}, 400, "bad_request"},
		{"path absent", map[string]any{"name": "x", "kind": "movie", "path": filepath.Join(existing, "nope")}, 400, "bad_request"},
		{"path is a file", map[string]any{"name": "x", "kind": "movie", "path": fileInLib}, 400, "bad_request"},
		{"duplicate path", map[string]any{"name": "x", "kind": "movie", "path": existing}, 409, "conflict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, h.do(t, "POST", "/api/libraries", tt.body), tt.status, tt.code)
		})
	}
}

func TestCreateLibraryMalformedJSON(t *testing.T) {
	h := newHarness(t)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/libraries", bytes.NewReader([]byte("{not json")))
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, resp, 400, "bad_request")
}

func TestCreateAndListLibraries(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()

	resp := h.do(t, "POST", "/api/libraries", map[string]any{"name": "Films", "kind": "movie", "path": dir})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var created store.Library
	decode(t, resp, &created)
	if created.ID == 0 || created.Name != "Films" {
		t.Errorf("created = %+v", created)
	}

	var libs []store.Library
	decode(t, h.do(t, "GET", "/api/libraries", nil), &libs)
	if len(libs) != 2 {
		t.Errorf("got %d libraries, want 2", len(libs))
	}
}

func TestScanLifecycle(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(filepath.Join(h.dir, "movie.mkv"), make([]byte, 32), 0o644)

	resp := h.do(t, "POST", "/api/libraries/1/scan", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("scan start status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	// Status endpoint must be reachable regardless of scan phase.
	resp = h.do(t, "GET", "/api/libraries/1/scan", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("scan status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestScanUnknownLibrary(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "POST", "/api/libraries/9999/scan", nil), 404, "not_found")
	wantError(t, h.do(t, "GET", "/api/libraries/9999/scan", nil), 404, "not_found")
	wantError(t, h.do(t, "POST", "/api/libraries/abc/scan", nil), 400, "bad_request")
}

func TestListItems(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "one.mkv", make([]byte, 16))
	h.addFile(t, "two.mkv", make([]byte, 16))

	var body struct {
		Total int          `json:"total"`
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items?library_id=1", nil), &body)
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", body.Total, len(body.Items))
	}

	decode(t, h.do(t, "GET", "/api/items?library_id=1&q=one", nil), &body)
	if body.Total != 1 {
		t.Errorf("filtered total = %d, want 1", body.Total)
	}
}

// Server filesystem paths must never reach a client.
func TestItemResponseOmitsPath(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "secret.mkv", make([]byte, 16))

	resp := h.do(t, "GET", "/api/items/"+itoa(id), nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	for _, probe := range []string{`"path"`, h.dir} {
		if bytes.Contains(raw, []byte(probe)) {
			t.Errorf("item response leaks %q: %s", probe, raw)
		}
	}
}

func TestGetItemNotFound(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/items/9999", nil), 404, "not_found")
	wantError(t, h.do(t, "GET", "/api/items/abc", nil), 400, "bad_request")
}

func TestProgressRoundTrip(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	resp := h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": 4242, "watched": false})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	var it store.Item
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id), nil), &it)
	if it.Progress == nil || it.Progress.PositionMS != 4242 {
		t.Errorf("progress = %+v, want 4242", it.Progress)
	}
}

func TestProgressValidation(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "a.mkv", make([]byte, 16))

	wantError(t, h.do(t, "PUT", "/api/items/"+itoa(id)+"/progress",
		map[string]any{"position_ms": -5}), 400, "bad_request")
	wantError(t, h.do(t, "PUT", "/api/items/9999/progress",
		map[string]any{"position_ms": 5}), 404, "not_found")
}

func TestStreamFullAndRange(t *testing.T) {
	h := newHarness(t)
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	id := h.addFile(t, "movie.mkv", payload)

	t.Run("full body", func(t *testing.T) {
		resp := h.do(t, "GET", "/api/stream/"+itoa(id), nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if resp.Header.Get("Accept-Ranges") != "bytes" {
			t.Error("Accept-Ranges is not bytes — seeking will not work")
		}
		got, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(got, payload) {
			t.Errorf("body length %d, want %d", len(got), len(payload))
		}
	})

	// Seeking is the assertion that matters: a server that only streams from
	// byte zero looks fine until you drag the scrubber.
	t.Run("mid-file range", func(t *testing.T) {
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/stream/"+itoa(id), nil)
		req.Header.Set("Range", "bytes=1000-1099")
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", resp.StatusCode)
		}
		if cr := resp.Header.Get("Content-Range"); cr != "bytes 1000-1099/4096" {
			t.Errorf("Content-Range = %q, want bytes 1000-1099/4096", cr)
		}
		got, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(got, payload[1000:1100]) {
			t.Error("range body does not match the source bytes at that offset")
		}
	})
}

func TestStreamMissingFileIsUnavailable(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "vanishing.mkv", make([]byte, 16))
	if err := os.Remove(filepath.Join(h.dir, "vanishing.mkv")); err != nil {
		t.Fatal(err)
	}
	wantError(t, h.do(t, "GET", "/api/stream/"+itoa(id), nil), 503, "unavailable")
}

// The containment guard: a row whose path escapes its library root must not
// become arbitrary file read access, even though the database is trusted.
func TestStreamRejectsPathOutsideLibrary(t *testing.T) {
	h := newHarness(t)

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("TOP SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: outside, Kind: "movie",
		Title: "escape", SortTitle: "escape", Container: "txt",
		SizeBytes: 10, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFiles(context.Background(), h.lib.ID)
	id := known[outside].ID

	resp := h.do(t, "GET", "/api/stream/"+itoa(id), nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a path outside the library root", resp.StatusCode)
	}
	if bytes.Contains(body, []byte("TOP SECRET")) {
		t.Fatal("SECURITY: file outside the library root was served")
	}
}

func TestStreamTraversalPathRejected(t *testing.T) {
	h := newHarness(t)

	outside := filepath.Join(t.TempDir(), "escape.mkv")
	os.WriteFile(outside, []byte("nope"), 0o644)
	// A path that only escapes once resolved, as a hand-edited row might.
	sneaky := filepath.Join(h.dir, "..", filepath.Base(filepath.Dir(outside)), "escape.mkv")

	h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: sneaky, Kind: "movie",
		Title: "sneaky", SortTitle: "sneaky", Container: "mkv", SizeBytes: 4, MTime: 1,
	})
	known, _ := h.st.KnownFiles(context.Background(), h.lib.ID)

	resp := h.do(t, "GET", "/api/stream/"+itoa(known[sneaky].ID), nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("SECURITY: traversal path was served")
	}
}

func TestStreamInvalidID(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "GET", "/api/stream/abc", nil), 400, "bad_request")
	wantError(t, h.do(t, "GET", "/api/stream/9999", nil), 404, "not_found")
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
