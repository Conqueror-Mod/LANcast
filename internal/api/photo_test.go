package api

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/store"
)

// addPhoto writes a real picture into the library and registers it as one.
func addPhoto(t *testing.T, h *harness, name string, w, hgt int) int64 {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, hgt))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return addPhotoBytes(t, h, name, buf.Bytes())
}

func addPhotoBytes(t *testing.T, h *harness, name string, body []byte) int64 {
	t.Helper()
	path := filepath.Join(h.dir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: path, Kind: "photo",
		Title: name, SortTitle: name, Container: filepath.Ext(name),
		SizeBytes: int64(len(body)), MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFiles(context.Background(), h.lib.ID)
	return known[path].ID
}

func TestPhotoServesTheOriginalFile(t *testing.T) {
	h := newHarness(t)
	id := addPhoto(t, h, "sunset.png", 12, 8)

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/photo", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("response is not an image: %v", err)
	}
	if format != "png" {
		t.Errorf("format = %q, want png — the original file, not a rendition", format)
	}
	if cfg.Width != 12 || cfg.Height != 8 {
		t.Errorf("served %dx%d, want the original 12x8", cfg.Width, cfg.Height)
	}
}

// The rule from CLAUDE.md: every handler turning a database row into a
// filesystem path re-verifies containment. This is the test that keeps it true.
func TestPhotoRefusesAPathOutsideTheLibrary(t *testing.T) {
	h := newHarness(t)

	// A row pointing outside the library root — what a hand-edited or corrupted
	// database looks like. The file genuinely exists, so only the containment
	// check stands between the request and it.
	outside := filepath.Join(t.TempDir(), "secret.png")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: h.lib.ID, Path: outside, Kind: "photo",
		Title: "secret", SortTitle: "secret", Container: ".png",
		SizeBytes: 10, MTime: 1,
	}); err != nil {
		t.Fatal(err)
	}
	known, _ := h.st.KnownFiles(context.Background(), h.lib.ID)
	id := known[outside].ID

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/photo", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — a row outside the library root must not be served",
			resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("PNG")) {
		t.Fatal("the file outside the library was served")
	}
}

func TestPhotoRefusesANonPhoto(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "film.mkv", []byte("not a picture"))

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/photo", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a film asked for as a photo", resp.StatusCode)
	}
}

// A format no browser renders falls back to the cached rendition. With no
// rendition yet, that is a stated "not ready", not a broken image — the two
// have different cures and only one of them is the user's problem.
func TestHEICWithNoRenditionSaysSoRatherThanServingBytes(t *testing.T) {
	h := newHarness(t)
	id := addPhotoBytes(t, h, "phone.heic", []byte("heic bytes a browser cannot draw"))

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/photo", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte("heic bytes")) {
		t.Error("served HEIC bytes to a browser that cannot decode them")
	}
}

func TestPhotoIsRevalidatedNotCachedForever(t *testing.T) {
	h := newHarness(t)
	id := addPhoto(t, h, "wall.png", 6, 6)

	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/photo", nil)
	if got := resp.Header.Get("Cache-Control"); got != "private, max-age=0, must-revalidate" {
		t.Errorf("Cache-Control = %q; a file behind an item id can be replaced on disk", got)
	}
}
