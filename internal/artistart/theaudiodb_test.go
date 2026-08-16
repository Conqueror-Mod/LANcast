package artistart

import (
	"errors"
	"strings"
	"testing"
)

/*
 * The decisions, against fixtures.
 *
 * The request is one function and the rules are another, for the reason the
 * probe package splits the same way: these rules are where the bugs live, and a
 * rule that needs a live provider to exercise is a rule nobody exercises.
 */

const hit = `{"artists":[{"strArtist":"Chevelle",
  "strArtistThumb":"https://images.example/chevelle-thumb.jpg",
  "strArtistFanart":"https://images.example/chevelle-fan.jpg"}]}`

func TestFindsAnArtist(t *testing.T) {
	got, err := parseSearch(strings.NewReader(hit), "Chevelle")
	if err != nil {
		t.Fatalf("parseSearch: %v", err)
	}
	if got.ThumbURL != "https://images.example/chevelle-thumb.jpg" {
		t.Errorf("thumb = %q", got.ThumbURL)
	}
	if got.ProviderName != "Chevelle" {
		t.Errorf("provider name = %q", got.ProviderName)
	}
}

// TheAudioDB answers a miss with `{"artists": null}` rather than an empty list
// or a 404, which decodes cleanly into nothing and would otherwise read as a
// successful lookup of an artist with no picture.
func TestNullArtistsIsAMiss(t *testing.T) {
	_, err := parseSearch(strings.NewReader(`{"artists":null}`), "Nobody")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

/*
 * The failure this integration is most likely to produce: a near miss accepted
 * as a match, and a photograph of the wrong band on somebody's tile.
 *
 * A search endpoint returns neighbours. Taking the first row is how "Sun" gets
 * a picture of "Sunn O)))" — and unlike a missing image, a wrong one looks like
 * it worked.
 */
func TestANearMissIsRefused(t *testing.T) {
	const near = `{"artists":[{"strArtist":"Sunn O)))",
	  "strArtistThumb":"https://images.example/sunn.jpg"}]}`

	_, err := parseSearch(strings.NewReader(near), "Sun")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound — a wrong face is worse than a borrowed sleeve", err)
	}
}

// Case and a leading article are the only differences forgiven: which of "The
// Beatles" and "Beatles" a library carries depends on whoever tagged it.
func TestArticleAndCaseAreForgiven(t *testing.T) {
	const beatles = `{"artists":[{"strArtist":"The Beatles",
	  "strArtistThumb":"https://images.example/beatles.jpg"}]}`

	for _, asked := range []string{"Beatles", "beatles", "THE BEATLES", " The  Beatles "} {
		if _, err := parseSearch(strings.NewReader(beatles), asked); err != nil {
			t.Errorf("looking up %q: %v", asked, err)
		}
	}
}

/*
 * A record with no picture is not a find.
 *
 * Returning it would replace a working placeholder — the borrowed album cover —
 * with nothing, which is the one outcome worse than leaving the placeholder
 * alone. ADR 0025 exists because the fallback is good.
 */
func TestARecordWithNoImageIsNotAFind(t *testing.T) {
	const bare = `{"artists":[{"strArtist":"Chevelle","strArtistThumb":"","strArtistFanart":""}]}`

	_, err := parseSearch(strings.NewReader(bare), "Chevelle")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The right record among several is taken, not the first.
func TestPicksTheMatchingRecord(t *testing.T) {
	const several = `{"artists":[
	  {"strArtist":"Chevelle Tribute","strArtistThumb":"https://images.example/wrong.jpg"},
	  {"strArtist":"Chevelle","strArtistThumb":"https://images.example/right.jpg"}]}`

	got, err := parseSearch(strings.NewReader(several), "Chevelle")
	if err != nil {
		t.Fatal(err)
	}
	if got.ThumbURL != "https://images.example/right.jpg" {
		t.Errorf("thumb = %q, want the exactly-matching record", got.ThumbURL)
	}
}

func TestUnreadableResponse(t *testing.T) {
	if _, err := parseSearch(strings.NewReader("<html>502</html>"), "Anyone"); err == nil {
		t.Error("an HTML error page was accepted as a search result")
	}
}

// No key means the provider says so rather than silently doing nothing, which
// is the difference between "not configured" and "found nothing".
func TestNoKeyIsNamed(t *testing.T) {
	c := New("")
	if c.Available() {
		t.Error("a client with no key reports itself available")
	}
	if _, err := c.Lookup(nil, "Chevelle"); !errors.Is(err, ErrNoKey) { //nolint:staticcheck
		t.Errorf("err = %v, want ErrNoKey", err)
	}
}
