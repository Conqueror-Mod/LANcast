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
	"lancast/internal/identity"
	"lancast/internal/meta"
	"lancast/internal/probe"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/transcode"
	"lancast/internal/update"
)

type harness struct {
	srv *httptest.Server
	// The server object behind srv, so a test can set the dependencies that are
	// facts about the host rather than about the request — whether this process
	// is a service, and whether it can relaunch itself.
	srvAPI   *Server
	st       *store.Store
	lib      *store.Library
	dir      string
	art      *artwork.Cache
	reg      *meta.Registry
	settings *config.SettingsStore
	dataDir  string
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
	// A real identity from the harness's own data directory, rather than a
	// zero value: the server always has one in production (main.go refuses to
	// start otherwise), and a test server without one would be a shape that
	// cannot occur, quietly passing tests for handlers that would panic.
	ident, err := identity.LoadOrCreate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	art := artwork.New(filepath.Join(dataDir, "artwork"))
	reg := meta.NewRegistry()
	h := &harness{st: st, art: art, reg: reg, settings: settings, dataDir: dataDir}

	api := New(Deps{
		Store: st, Scanner: scan.New(st, log), Registry: reg, Artwork: art,
		Worker:   enrich.New(st, reg, art, log),
		Probes:   probe.NewWorker(st, probe.New(), log),
		Trans:    transcode.NewManager(filepath.Join(dataDir, "transcode"), log),
		Updates:  update.New(Version),
		Settings: settings, Log: log, DataDir: dataDir, Identity: ident,
		ListenAddr: ":8080",
		Enrich:     func() { h.enriched++ },
	})

	h.srvAPI = api
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
	var body map[string]any
	decode(t, resp, &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	// health reports the injectable build version verbatim, so the binary,
	// /api/health, and the release tag cannot disagree (ADR 0016).
	if body["version"] != Version {
		t.Errorf("version = %v, want %q", body["version"], Version)
	}
	if body["api_version"] != float64(1) {
		t.Errorf("api_version = %v, want 1", body["api_version"])
	}
}

// A build injects Version via -ldflags -X; this proves health reflects whatever
// that var holds, so the injected value flows through.
func TestHealthReflectsVersionVar(t *testing.T) {
	orig := Version
	Version = "v9.9.9-test"
	defer func() { Version = orig }()

	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/health", nil), &body)
	if body["version"] != "v9.9.9-test" {
		t.Errorf("version = %v, want the injected value", body["version"])
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

// Facets carry the Phase-2 filter values (content ratings) and the has_watched
// hint, and the grid's repeatable filter params degrade sensibly: a blank value
// is a no-op, a non-numeric decade is a 400.
func TestBrowseFacetsAndFilterParams(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "one.mkv", make([]byte, 16))

	var facets struct {
		Genres         []string `json:"genres"`
		Decades        []int    `json:"decades"`
		ContentRatings []string `json:"content_ratings"`
		HasWatched     bool     `json:"has_watched"`
	}
	decode(t, h.do(t, "GET", "/api/libraries/1/facets", nil), &facets)
	if facets.ContentRatings == nil {
		t.Errorf("content_ratings should serialize as [], got nil")
	}
	if facets.HasWatched {
		t.Errorf("has_watched = true with nothing watched, want false")
	}

	// A non-numeric decade is rejected rather than silently ignored.
	wantError(t, h.do(t, "GET", "/api/items?library_id=1&decade=abc", nil), 400, "bad_request")

	// A blank repeated filter is a no-op, not a filter for the empty string.
	var body struct {
		Total int `json:"total"`
	}
	decode(t, h.do(t, "GET", "/api/items?library_id=1&genre=", nil), &body)
	if body.Total != 1 {
		t.Errorf("empty genre filter total = %d, want 1", body.Total)
	}
}

// The grid endpoint returns top-level items by default; a parented child is
// reached only through parent_id, never loose in the list (ADR 0010/0017).
func TestListItemsHierarchy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	movie := h.addFile(t, "Arrival.mkv", make([]byte, 16))
	show, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: filepath.Join(h.dir, "Show"), Kind: "show",
		Title: "Some Show", SortTitle: "Some Show",
	})
	if err != nil {
		t.Fatal(err)
	}
	ep := h.addFile(t, "S01E01.mkv", make([]byte, 16))
	if err := h.st.SetParent(ctx, ep, &show); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Total int          `json:"total"`
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items?library_id=1", nil), &body)
	got := map[int64]bool{}
	for _, it := range body.Items {
		got[it.ID] = true
	}
	if !got[movie] || !got[show] {
		t.Errorf("default listing = %v, want movie %d and show %d", got, movie, show)
	}
	if got[ep] {
		t.Errorf("default listing includes parented episode %d — should be hidden", ep)
	}

	decode(t, h.do(t, "GET", "/api/items?parent_id="+itoa(show), nil), &body)
	if len(body.Items) != 1 || body.Items[0].ID != ep {
		t.Errorf("parent_id listing = %v, want just episode %d", got, ep)
	}

	wantError(t, h.do(t, "GET", "/api/items?parent_id=abc", nil), 400, "bad_request")
}

// A collection's members are reached through collection_id (the join table),
// not parent_id — the bug that made every collection page open blank.
func TestListItemsCollectionMembers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	m1 := h.addFile(t, "Toy Story.mkv", make([]byte, 16))
	m2 := h.addFile(t, "Toy Story 2.mkv", make([]byte, 16))
	coll, err := h.st.UpsertItem(ctx, store.ScanFile{
		LibraryID: h.lib.ID, Path: "lancast:collection:test:1", Kind: "collection",
		Title: "Toy Story Collection", SortTitle: "toy story collection",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddToCollection(ctx, m1, coll, 0); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddToCollection(ctx, m2, coll, 1); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Total int          `json:"total"`
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", "/api/items?collection_id="+itoa(coll), nil), &body)
	got := map[int64]bool{}
	for _, it := range body.Items {
		got[it.ID] = true
	}
	if !got[m1] || !got[m2] || len(body.Items) != 2 {
		t.Errorf("collection members = %v, want the two films %d and %d", got, m1, m2)
	}
	// parent_id, by contrast, is empty for a collection.
	decode(t, h.do(t, "GET", "/api/items?parent_id="+itoa(coll), nil), &body)
	if len(body.Items) != 0 {
		t.Errorf("parent_id on a collection returned %d items, want 0", len(body.Items))
	}

	wantError(t, h.do(t, "GET", "/api/items?collection_id=abc", nil), 400, "bad_request")
}

// Deleting a title: 'delete' removes the file from disk, 'ignore' keeps it and
// only drops the row; a bad mode is rejected.
func TestDeleteItemModes(t *testing.T) {
	h := newHarness(t)

	// delete mode removes the file and the row.
	trash := h.addFile(t, "Trash.mkv", make([]byte, 16))
	trashPath := filepath.Join(h.dir, "Trash.mkv")
	if resp := h.do(t, "DELETE", "/api/items/"+itoa(trash)+"?mode=delete", nil); resp.StatusCode != 204 {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	if _, err := os.Stat(trashPath); !os.IsNotExist(err) {
		t.Errorf("file still on disk after delete: %v", err)
	}
	wantError(t, h.do(t, "GET", "/api/items/"+itoa(trash), nil), 404, "not_found")

	// ignore mode keeps the file, drops the row.
	keep := h.addFile(t, "Keep.mkv", make([]byte, 16))
	keepPath := filepath.Join(h.dir, "Keep.mkv")
	if resp := h.do(t, "DELETE", "/api/items/"+itoa(keep)+"?mode=ignore", nil); resp.StatusCode != 204 {
		t.Fatalf("ignore status = %d, want 204", resp.StatusCode)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("ignore removed the file from disk: %v", err)
	}
	wantError(t, h.do(t, "GET", "/api/items/"+itoa(keep), nil), 404, "not_found")

	// a missing mode is a bad request.
	x := h.addFile(t, "X.mkv", make([]byte, 16))
	wantError(t, h.do(t, "DELETE", "/api/items/"+itoa(x), nil), 400, "bad_request")
}

// Deleting a movie from disk sweeps its companion files — subtitles, nfo,
// artwork — but never a sibling's files or folder-level art.
func TestDeleteRemovesSidecars(t *testing.T) {
	h := newHarness(t)
	write := func(name string) string {
		p := filepath.Join(h.dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	id := h.addFile(t, "Film.mkv", make([]byte, 16))
	own := []string{
		write("Film.srt"), write("Film.en.srt"), write("Film.nfo"),
		write("Film-thumb.jpg"), write("Film.jpg"),
	}
	// A different title in the same folder, and shared folder art — must survive.
	survivors := []string{
		write("Film 2.srt"), // belongs to a hypothetical "Film 2"
		write("poster.jpg"), // folder-level, shared
	}

	if resp := h.do(t, "DELETE", "/api/items/"+itoa(id)+"?mode=delete", nil); resp.StatusCode != 204 {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	if _, err := os.Stat(filepath.Join(h.dir, "Film.mkv")); !os.IsNotExist(err) {
		t.Error("video not deleted")
	}
	for _, p := range own {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("sidecar not removed: %s", filepath.Base(p))
		}
	}
	for _, p := range survivors {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("unrelated file wrongly removed: %s", filepath.Base(p))
		}
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
