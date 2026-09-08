package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lancast/internal/meta"
)

// loadProviderFixture loads the same fixture module under a provider manifest.
// The HTTP grant is the same one loadFixture uses, which is what makes the
// artwork tests meaningful: example.test is granted, evil.test is not.
func loadProviderFixture(t *testing.T) *Plugin {
	t.Helper()
	p := loadFixture(t)
	p.Manifest.Kind = KindProvider
	p.Manifest.Caps = Caps{Movie: true, Artwork: true}
	return p
}

func newProvider(t *testing.T) meta.Provider {
	t.Helper()
	pv, err := NewProvider(loadProviderFixture(t))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return pv
}

// A provider plugin registered into the Registry is a provider like any other:
// ID, Caps, Search and Fetch all answer, and nothing downstream knows it is a
// plugin. This is the ADR 0007 promise at the second adapter seam.
func TestProviderThroughRegistry(t *testing.T) {
	pv := newProvider(t)
	if pv.ID() != "fixture" {
		t.Errorf("ID = %q, want fixture", pv.ID())
	}
	caps := pv.Caps()
	if !caps.Supports(meta.KindMovie) || caps.Supports(meta.KindEpisode) {
		t.Errorf("Caps = %+v, want movie only", caps)
	}

	got, err := pv.Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "Arrival", Year: 2016})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Three came back; the one with no external_id is not a candidate.
	if len(got) != 2 {
		t.Fatalf("candidates = %+v, want two", got)
	}
	c := got[0]
	if c.ExternalID != "ext-1" || c.Title != "Arrival" || c.Year != 2016 || c.Popularity != 3.5 {
		t.Errorf("candidate = %+v, want the query echoed back", c)
	}
	if c.Kind != meta.KindMovie {
		t.Errorf("kind = %q, want movie", c.Kind)
	}
}

// Ranking is the host's, and the wire shape is where that is enforced: a
// candidate arrives unscored no matter what the module said, because there is
// no field for a score to arrive in (ADR 0063).
func TestProviderCandidatesArriveUnscored(t *testing.T) {
	pv := newProvider(t)
	got, err := pv.Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "Arrival"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, c := range got {
		if c.Score != 0 || c.Breakdown != (meta.Breakdown{}) {
			t.Errorf("candidate %q arrived scored: %+v", c.ExternalID, c)
		}
		if c.Provider != "fixture" {
			t.Errorf("provider = %q, want the manifest name", c.Provider)
		}
	}
}

// Fetch carries a record across with its shape intact — and a field the module
// did not mention stays nil rather than becoming empty, which is what makes
// field-level merge precedence work (ADR 0008).
func TestProviderFetchRecord(t *testing.T) {
	pv := newProvider(t)
	rec, err := pv.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "ext-1"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if rec == nil {
		t.Fatal("Fetch returned no record")
	}
	// Source is the host's, not the module's: a plugin cannot attribute its
	// answers to somebody else.
	if rec.Source != "fixture" {
		t.Errorf("source = %q, want fixture", rec.Source)
	}
	if rec.ExternalID != "ext-1" || rec.IMDbID != "tt0000001" {
		t.Errorf("record = %+v, want ext-1 / tt0000001", rec)
	}
	if rec.Fields.Title == nil || *rec.Fields.Title != "A Fixture Film" {
		t.Errorf("title = %v, want A Fixture Film", rec.Fields.Title)
	}
	if rec.Fields.Year == nil || *rec.Fields.Year != 1999 {
		t.Errorf("year = %v, want 1999", rec.Fields.Year)
	}
	if rec.Fields.Overview != nil {
		t.Errorf("overview = %q, want nil — the module said nothing about it", *rec.Fields.Overview)
	}
	if len(rec.Genres) != 1 || rec.Genres[0] != "Drama" {
		t.Errorf("genres = %v, want [Drama]", rec.Genres)
	}
	// The nameless credit is dropped; the two named ones survive.
	if len(rec.Credits) != 2 {
		t.Fatalf("credits = %+v, want two", rec.Credits)
	}
	if rec.Collection == nil || rec.Collection.ExternalID != "col-1" {
		t.Fatalf("collection = %+v, want col-1", rec.Collection)
	}
	if len(rec.Keywords) != 1 || rec.Keywords[0].Name != "fixture" {
		t.Errorf("keywords = %+v, want one named fixture", rec.Keywords)
	}
}

/*
 * The second half of hole two (ADR 0063).
 *
 * An artwork URL is a fetch the *host* makes on the plugin's say-so. netguard
 * already refuses private and local addresses; this is the rest — a URL on a
 * host the plugin was never granted is dropped before anything fetches it.
 *
 * The fixture returns one granted and one ungranted URL in every place a URL
 * can appear, so a field added later without the check fails here.
 */
func TestProviderDropsArtworkOutsideItsGrant(t *testing.T) {
	pv := newProvider(t)
	rec, err := pv.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie, ExternalID: "ext-1"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(rec.Artwork) != 1 || rec.Artwork[0].URL != "https://example.test/poster.jpg" {
		t.Errorf("artwork = %+v, want only the granted host", rec.Artwork)
	}
	if len(rec.Collection.Artwork) != 0 {
		t.Errorf("collection artwork = %+v, want empty — evil.test was never granted", rec.Collection.Artwork)
	}
	for _, c := range rec.Credits {
		if strings.Contains(c.Image, "evil.test") {
			t.Errorf("credit %q kept an ungranted image %q", c.Name, c.Image)
		}
	}
	if rec.Credits[0].Image != "https://example.test/face.jpg" {
		t.Errorf("credit image = %q, want the granted one kept", rec.Credits[0].Image)
	}

	got, err := pv.Search(context.Background(), meta.Query{Kind: meta.KindMovie, Title: "Arrival"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got[0].PosterURL != "https://example.test/poster.jpg" {
		t.Errorf("poster = %q, want the granted one kept", got[0].PosterURL)
	}
	if got[1].PosterURL != "" {
		t.Errorf("poster = %q, want dropped", got[1].PosterURL)
	}
}

/*
 * The distinction ABI 1 could not make, and the reason the provider forced it.
 *
 * "No candidates" means this item is unmatched, which the enricher records and
 * stops re-asking. "The API is down" means ask again later. If these two ever
 * arrive as the same answer again, a temporary outage becomes a permanent
 * conclusion about somebody's library.
 */
func TestProviderTellsNothingFromFailure(t *testing.T) {
	pv := newProvider(t)
	ctx := context.Background()

	got, err := pv.Search(ctx, meta.Query{Kind: meta.KindMovie, Title: "nothing"})
	if err != nil {
		t.Fatalf("empty search returned an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("candidates = %+v, want none", got)
	}

	_, err = pv.Search(ctx, meta.Query{Kind: meta.KindMovie, Title: "fail"})
	if !errors.Is(err, ErrPluginRefused) {
		t.Fatalf("failed search error = %v, want ErrPluginRefused", err)
	}
	if !strings.Contains(err.Error(), "asked to fail") {
		t.Errorf("error = %q, want the guest's words in it", err)
	}

	rec, err := pv.Fetch(ctx, meta.Ref{Kind: meta.KindMovie, ExternalID: "missing"})
	if err != nil || rec != nil {
		t.Errorf("missing fetch = (%v, %v), want (nil, nil)", rec, err)
	}

	if _, err := pv.Fetch(ctx, meta.Ref{Kind: meta.KindMovie, ExternalID: "fail"}); !errors.Is(err, ErrPluginRefused) {
		t.Errorf("failed fetch error = %v, want ErrPluginRefused", err)
	}
}

func TestProviderRefusesEmptyRefAndWrongKind(t *testing.T) {
	pv := newProvider(t)
	rec, err := pv.Fetch(context.Background(), meta.Ref{Kind: meta.KindMovie})
	if err != nil || rec != nil {
		t.Errorf("empty ref Fetch = (%v, %v), want (nil, nil)", rec, err)
	}
	if _, err := NewProvider(loadFixture(t)); err == nil {
		t.Error("NewProvider accepted a rating_source plugin")
	}
}

// A provider that answers for no kind would be installed, listed, and silent
// for ever. The manifest refuses it where somebody is reading an error.
func TestManifestRefusesProviderWithNoCaps(t *testing.T) {
	_, err := ParseManifest([]byte(`{"name":"x","version":"1","abi":2,"kind":"provider"}`))
	if err == nil {
		t.Fatal("accepted a provider declaring no caps")
	}
	if !strings.Contains(err.Error(), "caps") {
		t.Errorf("error = %q, want it to name caps", err)
	}
	if _, err := ParseManifest([]byte(`{"name":"x","version":"1","abi":2,"kind":"provider","caps":{"movie":true}}`)); err != nil {
		t.Errorf("refused a provider declaring movie: %v", err)
	}
}
