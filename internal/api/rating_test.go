package api

import (
	"net/http"
	"testing"

	"lancast/internal/store"
)

type ratingBody struct {
	Rating *store.Rating `json:"rating"`
}

/*
 * "I have not rated this" and "I rated this badly" are different statements.
 *
 * An unrated item answers with a null rating and a 200, not a 404: the item
 * exists, your verdict does not. Conflating those would make the client unable
 * to tell "no opinion" from "no such film".
 */
func TestUnratedItemAnswersNull(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))

	var body ratingBody
	resp := h.do(t, "GET", "/api/items/"+itoa(id)+"/rating", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &body)
	if body.Rating != nil {
		t.Errorf("rating = %+v, want null for an item nobody has rated", body.Rating)
	}
}

func TestRateAndReadBack(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))

	var body ratingBody
	decode(t, h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating",
		map[string]any{"score": 8, "review": "better than I remembered"}), &body)
	if body.Rating == nil || body.Rating.Score != 8 {
		t.Fatalf("rating = %+v, want score 8", body.Rating)
	}
	if body.Rating.Review == nil || *body.Rating.Review != "better than I remembered" {
		t.Errorf("review = %v, want the note that was sent", body.Rating.Review)
	}

	// And it is still there on a fresh read.
	var again ratingBody
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/rating", nil), &again)
	if again.Rating == nil || again.Rating.Score != 8 {
		t.Errorf("rating = %+v after a re-read, want score 8", again.Rating)
	}
}

// One verdict per person per item, replaced when they change their mind —
// not a growing pile of opinions.
func TestRatingIsReplacedNotAccumulated(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))

	h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating", map[string]any{"score": 3}).Body.Close()
	h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating", map[string]any{"score": 9}).Body.Close()

	var body ratingBody
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/rating", nil), &body)
	if body.Rating == nil || body.Rating.Score != 9 {
		t.Errorf("rating = %+v, want the most recent score", body.Rating)
	}

	var list struct {
		Ratings []store.RatedItem `json:"ratings"`
	}
	decode(t, h.do(t, "GET", "/api/profile/ratings", nil), &list)
	if len(list.Ratings) != 1 {
		t.Errorf("ratings = %d, want 1 — changing your mind is not a second rating", len(list.Ratings))
	}
}

// Withdrawing a verdict is not scoring something 1. An interface that cannot
// say the first is one people stop trusting with the second.
func TestRatingCanBeWithdrawn(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))
	h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating", map[string]any{"score": 7}).Body.Close()

	resp := h.do(t, "DELETE", "/api/items/"+itoa(id)+"/rating", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	var body ratingBody
	decode(t, h.do(t, "GET", "/api/items/"+itoa(id)+"/rating", nil), &body)
	if body.Rating != nil {
		t.Errorf("rating = %+v after withdrawal, want null", body.Rating)
	}
}

func TestRatingRangeIsEnforced(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))

	for _, score := range []int{0, -1, 11, 100} {
		wantError(t, h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating",
			map[string]any{"score": score}),
			http.StatusBadRequest, "bad_request")
	}
}

func TestRatingAnUnknownItem(t *testing.T) {
	h := newHarness(t)
	wantError(t, h.do(t, "PUT", "/api/items/9999/rating", map[string]any{"score": 5}),
		http.StatusNotFound, "not_found")
}

// An empty review clears the note without touching the score: they are two
// things somebody may want to change independently.
func TestEmptyReviewClearsTheNoteNotTheScore(t *testing.T) {
	h := newHarness(t)
	id := h.addFile(t, "Arrival.mkv", []byte("x"))
	h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating",
		map[string]any{"score": 6, "review": "worth another look"}).Body.Close()

	var body ratingBody
	decode(t, h.do(t, "PUT", "/api/items/"+itoa(id)+"/rating",
		map[string]any{"score": 6, "review": ""}), &body)
	if body.Rating == nil || body.Rating.Score != 6 {
		t.Fatalf("rating = %+v, want the score kept", body.Rating)
	}
	if body.Rating.Review != nil {
		t.Errorf("review = %v, want it cleared", *body.Rating.Review)
	}
}

func TestRatingsListIsNewestFirst(t *testing.T) {
	h := newHarness(t)
	first := h.addFile(t, "First.mkv", []byte("x"))
	second := h.addFile(t, "Second.mkv", []byte("x"))
	h.do(t, "PUT", "/api/items/"+itoa(first)+"/rating", map[string]any{"score": 4}).Body.Close()
	h.do(t, "PUT", "/api/items/"+itoa(second)+"/rating", map[string]any{"score": 5}).Body.Close()

	var list struct {
		Ratings []store.RatedItem `json:"ratings"`
	}
	decode(t, h.do(t, "GET", "/api/profile/ratings", nil), &list)
	if len(list.Ratings) != 2 {
		t.Fatalf("ratings = %d, want 2", len(list.Ratings))
	}
	if list.Ratings[0].Item.ID != second {
		t.Errorf("first entry is %d, want the most recently rated (%d)",
			list.Ratings[0].Item.ID, second)
	}
}
