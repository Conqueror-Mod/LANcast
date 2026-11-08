package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"lancast/internal/store"
)

/*
 * On this day carries each photograph's thumbnail.
 *
 * It did not: the handler returned the rows without their artwork, so every
 * tile on the old shelf and the new On this day panel drew its filename over
 * an empty frame. Found on the live library after the Homepage 2.0 swap.
 */
func TestMemoriesCarryTheirThumbnails(t *testing.T) {
	h := newHarness(t)
	id := facePhoto(t, h, "lastyear.jpg", solidJPEG(t, fileRed), "")

	// Local noon a year ago today: the query decides the day by the server's
	// local clock, and noon keeps either side of UTC on the same date.
	n := time.Now()
	taken := time.Date(n.Year()-1, n.Month(), n.Day(), 12, 0, 0, 0, time.Local).Unix()
	if err := h.st.SetPhotoMeta(context.Background(), id, 100, 100, taken, ""); err != nil {
		t.Fatal(err)
	}

	resp := h.do(t, http.MethodGet, "/api/memories?limit=10", nil)
	var body struct {
		Items []struct {
			ID      int64             `json:"id"`
			Artwork map[string]string `json:"artwork"`
		} `json:"items"`
	}
	decode(t, resp, &body)

	if len(body.Items) != 1 || body.Items[0].ID != id {
		t.Fatalf("memories = %+v, want the one photograph from a year ago", body.Items)
	}
	if body.Items[0].Artwork["poster"] == "" {
		t.Error("the photograph came back with no thumbnail; its tile would draw only the filename")
	}
}

// The history half (ADR 0119 §3a) is always a list, and something finished
// today is not yet a memory. The dated cases are the store's tests: this
// package cannot write a viewing on any day but today.
func TestWatchedOnThisDayLeavesOutToday(t *testing.T) {
	h := newHarness(t)
	film := h.addFile(t, "today.mkv", []byte("x"))
	if err := h.st.SaveProgress(context.Background(), film, store.LocalUserID, 0, true); err != nil {
		t.Fatal(err)
	}
	resp := h.do(t, http.MethodGet, "/api/memories", nil)
	var body map[string]json.RawMessage
	decode(t, resp, &body)
	if string(body["watched"]) != "[]" {
		t.Errorf("watched = %s, want []", body["watched"])
	}
}
