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

/*
 * GET /api/libraries/{id}/near-copies (ADR 0075, 2026-10-06 amendment): a
 * resized copy is grouped with the larger one kept, the other kinds are
 * refused, and the empty answers are arrays.
 */
func TestNearCopiesEndpointGroupsAResizedCopy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	lib := pictureLibrary(t, h)
	big := seedPhotoRow(t, h, lib, "/p/big.jpg")
	small := seedPhotoRow(t, h, lib, "/p/small.jpg")
	for _, p := range []struct {
		id   int64
		w, h int
		sha  string
	}{{big, 3648, 2736, "aa"}, {small, 720, 540, "bb"}} {
		if err := h.st.SetPhotoMeta(ctx, p.id, p.w, p.h, 0, p.sha); err != nil {
			t.Fatal(err)
		}
		if err := h.st.SetPhotoDHash(ctx, p.id, 0x0F0F); err != nil {
			t.Fatal(err)
		}
		if err := h.st.SavePhotoEmbedding(ctx, p.id, "clip", []float32{1, 0.1, 0, 0}); err != nil {
			t.Fatal(err)
		}
	}

	resp := h.do(t, "GET", "/api/libraries/"+itoa(lib)+"/near-copies", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Groups []struct {
			Keep   int64 `json:"keep"`
			Copies []struct {
				Item struct{ ID int64 } `json:"item"`
			} `json:"copies"`
		} `json:"groups"`
		ExtraCopies int `json:"extra_copies"`
		Pending     int `json:"pending"`
	}
	decode(t, resp, &body)
	if len(body.Groups) != 1 || body.Groups[0].Keep != big || body.ExtraCopies != 1 || body.Pending != 0 {
		t.Fatalf("body = %+v, want one group keeping %d", body, big)
	}
}

func TestNearCopiesEndpointRefusesOtherKinds(t *testing.T) {
	h := newHarness(t)
	lib, err := h.st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, h.do(t, "GET", "/api/libraries/"+itoa(lib.ID)+"/near-copies", nil), 400, "wrong_kind")
}

func TestNearCopiesEndpointAnswersAnEmptyList(t *testing.T) {
	h := newHarness(t)
	lib := pictureLibrary(t, h)
	m := decodeMap(t, h.do(t, "GET", "/api/libraries/"+itoa(lib)+"/near-copies", nil))
	groups, ok := m["groups"].([]any)
	if !ok || len(groups) != 0 {
		t.Errorf("groups = %#v, want []", m["groups"])
	}
}
