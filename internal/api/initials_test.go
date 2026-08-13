package api

import (
	"testing"
)

/*
 * The A–Z rail: which letters exist, and what one of them selects.
 *
 * The rail is offered from the facets rather than assumed, because a strip of
 * twenty-six letters where nineteen do nothing is a control that lies about
 * what is there. So the two halves have to agree: every letter offered must
 * select something, and the "#" bucket must hold everything that is not a
 * Latin letter rather than quietly dropping it.
 */
func TestInitialsFacetAndFilterAgree(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "Alien.mkv", []byte("a"))
	h.addFile(t, "Aliens.mkv", []byte("b"))
	h.addFile(t, "Solaris.mkv", []byte("c"))
	h.addFile(t, "300.mkv", []byte("d"))
	h.addFile(t, "Ran.mkv", []byte("e"))

	var facets struct {
		Initials []string `json:"initials"`
	}
	decode(t, h.do(t, "GET", "/api/libraries/"+itoa(h.lib.ID)+"/facets", nil), &facets)

	// "#" first, then the letters in order — where a list sorted by sort_title
	// puts them.
	want := []string{"#", "A", "R", "S"}
	if len(facets.Initials) != len(want) {
		t.Fatalf("initials = %v, want %v", facets.Initials, want)
	}
	for i := range want {
		if facets.Initials[i] != want[i] {
			t.Fatalf("initials = %v, want %v", facets.Initials, want)
		}
	}

	count := func(initial string) int {
		resp := h.do(t, "GET",
			"/api/items?library_id="+itoa(h.lib.ID)+"&initial="+initial, nil)
		var body struct {
			Items []struct{} `json:"items"`
		}
		decode(t, resp, &body)
		return len(body.Items)
	}

	if got := count("A"); got != 2 {
		t.Errorf("initial=A returned %d items, want 2", got)
	}
	// The bucket for everything that does not start with a letter. A rail that
	// offered no home for "300" would leave it reachable only by scrolling.
	if got := count("%23"); got != 1 {
		t.Errorf("initial=# returned %d items, want 1 (the numeric title)", got)
	}
	// Every offered letter selects something. This is the property that makes
	// the rail honest, and it is checked rather than assumed because the facet
	// query and the filter are two pieces of SQL that could drift.
	for _, c := range facets.Initials {
		q := c
		if q == "#" {
			q = "%23"
		}
		if count(q) == 0 {
			t.Errorf("the rail offers %q and the filter finds nothing under it", c)
		}
	}
}

// Case is not a fact about a title. A library with "the matrix" and "The Thing"
// has one T, not two.
func TestInitialsAreCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	h.addFile(t, "the matrix.mkv", []byte("a"))
	h.addFile(t, "The Thing.mkv", []byte("b"))

	var facets struct {
		Initials []string `json:"initials"`
	}
	decode(t, h.do(t, "GET", "/api/libraries/"+itoa(h.lib.ID)+"/facets", nil), &facets)
	if len(facets.Initials) != 1 || facets.Initials[0] != "T" {
		t.Errorf("initials = %v, want [T]", facets.Initials)
	}
}
