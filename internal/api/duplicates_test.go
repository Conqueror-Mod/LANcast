package api

import (
	"context"
	"net/http"
	"testing"

	"lancast/internal/store"
)

/*
 * GET /api/libraries/{id}/duplicates (ADR 0075): the groups survive the wire
 * with their albums, a picture library is the only kind it answers for, and an
 * empty library answers an empty list rather than null.
 */

func TestDuplicatesEndpointReturnsGroupsWithAlbums(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	lib := pictureLibrary(t, h)
	album, err := h.st.EnsureDerivedContainer(ctx, lib, "gallery", "/p/Nature", "Nature", "nature", nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, name := range []string{"/p/Nature/a.jpg", "/p/a.jpg"} {
		id := seedPhotoRow(t, h, lib, name)
		ids = append(ids, id)
		if err := h.st.SetPhotoMeta(ctx, id, 10, 10, 0, "9f2c"); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.st.SetParent(ctx, ids[0], &album); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, "GET", "/api/libraries/"+itoa(lib)+"/duplicates", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Groups []struct {
			SHA256 string `json:"sha256"`
			Copies []struct {
				Item  struct{ ID int64 } `json:"item"`
				Album *string            `json:"album"`
			} `json:"copies"`
		} `json:"groups"`
		ExtraCopies int `json:"extra_copies"`
	}
	decode(t, resp, &body)
	if len(body.Groups) != 1 || len(body.Groups[0].Copies) != 2 || body.ExtraCopies != 1 {
		t.Fatalf("body = %+v, want one group of two and one extra copy", body)
	}
	var sawAlbum, sawRoot bool
	for _, c := range body.Groups[0].Copies {
		if c.Album != nil && *c.Album == "Nature" {
			sawAlbum = true
		}
		if c.Album == nil {
			sawRoot = true
		}
	}
	if !sawAlbum || !sawRoot {
		t.Errorf("copies = %+v, want one in Nature and one at the root (null)", body.Groups[0].Copies)
	}
}

func seedPhotoRow(t *testing.T, h *harness, lib int64, path string) int64 {
	t.Helper()
	id, err := h.st.UpsertItem(context.Background(), store.ScanFile{
		LibraryID: lib, Path: path, Kind: "photo", Title: path, SortTitle: path,
		Container: "jpg", SizeBytes: 100, MTime: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDuplicatesEndpointRefusesOtherKinds(t *testing.T) {
	h := newHarness(t)
	lib, err := h.st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resp := h.do(t, "GET", "/api/libraries/"+itoa(lib.ID)+"/duplicates", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 wrong_kind", resp.StatusCode)
	}
}

// An array, never null: a client iterating `groups` must not have to guard.
func TestDuplicatesEndpointAnswersAnEmptyList(t *testing.T) {
	h := newHarness(t)
	lib := pictureLibrary(t, h)
	m := decodeMap(t, h.do(t, "GET", "/api/libraries/"+itoa(lib)+"/duplicates", nil))
	groups, ok := m["groups"].([]any)
	if !ok || len(groups) != 0 {
		t.Errorf("groups = %#v, want []", m["groups"])
	}
}
