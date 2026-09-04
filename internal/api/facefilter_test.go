package api

import (
	"testing"
)

/*
 * face_cluster on the browse grid.
 *
 * The store owns the interesting properties — one photograph per person rather
 * than one per face, and the marked-folder exclusion — and proves them in
 * internal/store/facefilter_test.go against real clustering. What is left here
 * is the contract at the edge: a malformed id is refused rather than widened,
 * and an id nobody has narrows to nothing rather than to everything.
 *
 * That second one is the failure worth having a test for. Every filter in this
 * handler that widens on a value it cannot use does so deliberately —
 * `resolution` ignores an unknown tier because a renamed one arriving from a
 * bookmark should restore the grid rather than break the page. An id is the
 * opposite case: it is machine-generated, so a value that matches nothing means
 * the caller is confused, and answering with the whole library would look like
 * the person was in every photograph ever taken.
 */

func TestFaceClusterRefusesAMalformedID(t *testing.T) {
	h := newHarness(t)

	for _, bad := range []string{"abc", "1.5", "3,4"} {
		resp := h.do(t, "GET", "/api/items?library_id="+itoa(h.lib.ID)+"&face_cluster="+bad, nil)
		if resp.StatusCode != 400 {
			t.Errorf("face_cluster=%q answered %d, want 400 — a malformed id must "+
				"not widen the grid to everything", bad, resp.StatusCode)
		}
	}
}

// An id that parses and matches nobody is an empty grid, not the whole library.
func TestFaceClusterWithNoFacesReturnsNothing(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "film.mkv", []byte("f"))

	resp := h.do(t, "GET", "/api/items?library_id="+itoa(h.lib.ID)+"&face_cluster=999999", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("well-formed unknown id answered %d, want 200", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decode(t, resp, &body)
	if len(body.Items) != 0 || body.Total != 0 {
		t.Errorf("a face group nobody is in returned %d items (total %d), want none — "+
			"widening here would read as \"this person is in everything\"",
			len(body.Items), body.Total)
	}
}

// Absent behaves exactly as before the parameter existed. It is additive under
// ADR 0018, so a client that never learns about it is unaffected.
func TestNoFaceClusterIsUnchanged(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "film.mkv", []byte("f"))

	resp := h.do(t, "GET", "/api/items?library_id="+itoa(h.lib.ID), nil)
	var body struct {
		Total int `json:"total"`
	}
	decode(t, resp, &body)
	if body.Total == 0 {
		t.Error("the unfiltered grid is empty; the filter is affecting a request that did not ask for it")
	}
}
