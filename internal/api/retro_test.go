package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * Retro games at the API (ADR 0073): the library kind is accepted, the
 * console filter narrows a listing, and a ROM library never reaches a
 * paired server.
 */

func TestRetroLibraryCanBeCreated(t *testing.T) {
	h := newHarness(t)
	resp := h.do(t, "POST", "/api/libraries", map[string]any{
		"name": "Games", "kind": "retro", "path": t.TempDir(),
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func addROM(t *testing.T, h *harness, lib *store.Library, name, platform string) int64 {
	t.Helper()
	var p *string
	if platform != "" {
		p = &platform
	}
	id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib.ID, Path: filepath.Join(lib.Path, name), Kind: "rom",
		Title: name, SortTitle: name, Platform: p, Container: "z64", SizeBytes: 1, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestItemsFilterByPlatform(t *testing.T) {
	h := newHarness(t)
	lib, err := h.st.CreateLibrary(context.Background(), "Games", "retro", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	addROM(t, h, lib, "a.z64", "n64")
	addROM(t, h, lib, "b.sfc", "snes")
	addROM(t, h, lib, "c.gba", "gba")

	var page struct {
		Items []struct {
			Kind     string  `json:"kind"`
			Platform *string `json:"platform"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decode(t, h.do(t, "GET", "/api/items?library_id="+itoa(lib.ID)+"&platform=n64&platform=gba", nil), &page)
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("total %d, items %d, want 2", page.Total, len(page.Items))
	}
	for _, it := range page.Items {
		if it.Kind != "rom" || it.Platform == nil || (*it.Platform != "n64" && *it.Platform != "gba") {
			t.Errorf("unexpected item %+v", it)
		}
	}

	// An unknown console widens rather than empties: a bookmark should not
	// break the page.
	decode(t, h.do(t, "GET", "/api/items?library_id="+itoa(lib.ID)+"&platform=dreamcast", nil), &page)
	if page.Total != 3 {
		t.Errorf("unknown platform total %d, want 3", page.Total)
	}
}

// A ROM library is never shared with a paired server: the grant is refused,
// and a row that got there anyway puts nothing in the friend's scope.
func TestRetroLibraryIsNotShareable(t *testing.T) {
	f := newShareFixture(t)
	games, err := f.h.st.CreateLibrary(context.Background(), "Games", "retro", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	resp := f.h.authed(t, "PUT", "/api/peers/"+f.peerFP+"/shares/"+itoa(games.ID), map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("sharing a retro library: status %d, want 400", resp.StatusCode)
	}

	// Written behind the handler's back: the scope must still exclude it.
	ctx := context.Background()
	if err := f.h.st.ShareLibrary(ctx, f.peerFP, games.ID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.h.st.ShareLibrary(ctx, f.peerFP, f.videoID, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	scope, err := f.h.st.SharedLibraries(ctx, f.peerFP)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 1 || scope[0] != f.videoID {
		t.Errorf("scope = %v, want only the video library %d", scope, f.videoID)
	}
}

func TestRetroDatabaseStatus(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	var got struct {
		Installed  bool     `json:"installed"`
		Commit     string   `json:"commit"`
		Licence    string   `json:"licence"`
		BytesTotal int64    `json:"bytes_total"`
		Platforms  []string `json:"platforms"`
		Files      []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"files"`
		Job struct {
			Running bool `json:"running"`
		} `json:"job"`
	}
	decode(t, h.authed(t, "GET", "/api/retro/database", nil), &got)
	if got.Installed || got.Commit == "" || got.Licence == "" || got.BytesTotal <= 0 {
		t.Errorf("got %+v", got)
	}
	if len(got.Files) != 25 || len(got.Platforms) != 9 {
		t.Errorf("%d files, %d platforms", len(got.Files), len(got.Platforms))
	}
	if got.Job.Running {
		t.Error("a job is running before anybody asked for one")
	}
}

func TestRetroArtworkSetting(t *testing.T) {
	h := newHarness(t)
	var got map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &got)
	if got["retro_artwork"] != false {
		t.Errorf("retro_artwork default = %v, want false", got["retro_artwork"])
	}
	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"retro_artwork": true}), &got)
	if got["retro_artwork"] != true {
		t.Errorf("retro_artwork after PUT = %v", got["retro_artwork"])
	}
	if !h.settings.Get().RetroArtwork {
		t.Error("the setting was not stored")
	}
}
