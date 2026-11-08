package api

import (
	"context"
	"net/http"
	"testing"
	"time"
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
