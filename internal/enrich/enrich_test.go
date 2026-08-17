package enrich

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

// fakeProvider serves canned candidates and records without a network.
type fakeProvider struct {
	id        string
	cands     []meta.Candidate
	record    *meta.Record
	searchN   int
	fetchN    int
	lastFetch meta.Ref
	err       error

	// queries records every search the worker actually issued, so a test can
	// assert on what was asked rather than only on how often. The worker runs
	// several items concurrently, hence the lock.
	mu      sync.Mutex
	queries []meta.Query
}

func (f *fakeProvider) ID() string      { return f.id }
func (f *fakeProvider) Caps() meta.Caps { return meta.Caps{Movie: true, Show: true, Episode: true} }

func (f *fakeProvider) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
	f.mu.Lock()
	f.queries = append(f.queries, q)
	f.mu.Unlock()
	f.searchN++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]meta.Candidate, len(f.cands))
	copy(out, f.cands)
	return out, nil
}

func (f *fakeProvider) Fetch(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	f.fetchN++
	f.lastFetch = ref
	if f.err != nil {
		return nil, f.err
	}
	return f.record, nil
}

// fakeLocal is a LocalSource returning a fixed record.
type fakeLocal struct {
	id  string
	rec *meta.Record
}

func (f *fakeLocal) ID() string { return f.id }
func (f *fakeLocal) Read(ctx context.Context, path string, kind meta.Kind) (*meta.Record, error) {
	return f.rec, nil
}

// fakeArt records downloads without touching the network.
type fakeArt struct{ downloaded []string }

func (a *fakeArt) Download(ctx context.Context, url string) (string, int, int, int64, error) {
	a.downloaded = append(a.downloaded, url)
	h := "0000000000000000000000000000000000000000000000000000000000000000"
	return h, 342, 513, 1024, nil
}
func (a *fakeArt) Stored(hash string) bool { return false }

func harness(t *testing.T) (*store.Store, *store.Library) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	lib, err := st.CreateLibrary(context.Background(), "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st, lib
}

func addItem(t *testing.T, st *store.Store, lib *store.Library, path, title string, year int) int64 {
	t.Helper()
	f := store.ScanFile{
		LibraryID: lib.ID, Path: path, Kind: "movie",
		Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
	}
	if year > 0 {
		f.Year = &year
	}
	if _, err := st.UpsertItem(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownFiles(context.Background(), lib.ID)
	return known[path].ID
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func arrivalRecord() *meta.Record {
	return &meta.Record{
		Source: "fake", ExternalID: "329865", Kind: meta.KindMovie,
		Fields: meta.Fields{
			Title:    meta.S("Arrival"),
			Year:     meta.I(2016),
			Overview: meta.S("A linguist makes contact."),
			Rating:   meta.F(7.6),
		},
		Genres:  []string{"Science Fiction", "Drama"},
		Credits: []meta.Credit{{Name: "Amy Adams", Role: "actor", Character: "Louise Banks"}},
		Artwork: []meta.ArtRef{{Kind: meta.ArtPoster, URL: "https://img/p.jpg"}},
	}
}

func TestEnrichAppliesProviderMetadata(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	p := &fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	art := &fakeArt{}
	w := New(st, reg, art, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Overview == nil || *it.Overview != "A linguist makes contact." {
		t.Errorf("Overview = %v", it.Overview)
	}
	if it.MatchState != meta.StateMatched {
		t.Errorf("MatchState = %q, want matched", it.MatchState)
	}
	if it.Provider == nil || *it.Provider != "fake" {
		t.Errorf("Provider = %v", it.Provider)
	}

	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if len(it.Genres) != 2 {
		t.Errorf("Genres = %v", it.Genres)
	}
	if len(it.Credits) != 1 {
		t.Errorf("Credits = %+v", it.Credits)
	}
	if it.Artwork == nil || it.Artwork.Poster == "" {
		t.Errorf("Artwork = %+v, want a poster hash", it.Artwork)
	}
	if len(art.downloaded) != 1 {
		t.Errorf("downloads = %v, want one poster", art.downloaded)
	}

	// The queue must drain.
	pending, _ := st.PendingEnrichment(ctx, 10)
	if len(pending) != 0 {
		t.Errorf("%d items still pending after enrichment", len(pending))
	}
}

// fakeRatingSource returns canned scores for an imdb id, recording the id it was
// asked about so a test can assert it was keyed off the resolved imdb id.
type fakeRatingSource struct {
	ratings  []meta.Rating
	askedFor string
	err      error
}

func (f *fakeRatingSource) ID() string { return "fake-ratings" }
func (f *fakeRatingSource) Ratings(ctx context.Context, imdbID string) ([]meta.Rating, error) {
	f.askedFor = imdbID
	return f.ratings, f.err
}

// When a provider resolves an imdb id and a rating source is configured, the
// enricher fetches and stores third-party scores keyed on that id (ADR 0019).
func TestEnrichFetchesExternalRatings(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	rec := arrivalRecord()
	rec.IMDbID = "tt2543164"
	p := &fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: rec,
	}
	rs := &fakeRatingSource{ratings: []meta.Rating{
		{Source: "rotten_tomatoes", Score: 9.4, Display: "94%"},
		{Source: "imdb", Score: 7.9, Display: "7.9", Votes: 700000},
	}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	reg.AddRatingSource(rs)

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if rs.askedFor != "tt2543164" {
		t.Errorf("rating source asked for %q, want the resolved imdb id tt2543164", rs.askedFor)
	}
	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.IMDbID == nil || *it.IMDbID != "tt2543164" {
		t.Errorf("imdb_id = %v, want tt2543164 stored on the item", it.IMDbID)
	}
	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if len(it.Ratings) != 2 || it.Ratings[0].Source != "rotten_tomatoes" {
		t.Fatalf("ratings = %+v, want RT + IMDb with RT first", it.Ratings)
	}
}

// Without a rating source the pass is skipped cleanly — no imdb id lookup, no
// stored ratings — and enrichment still completes.
func TestEnrichSkipsRatingsWithoutSource(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	rec := arrivalRecord()
	rec.IMDbID = "tt2543164"
	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: rec,
	})

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if err := st.LoadDetail(ctx, it); err != nil {
		t.Fatal(err)
	}
	if len(it.Ratings) != 0 {
		t.Errorf("ratings = %+v, want none without a source", it.Ratings)
	}
	// The imdb id is still recorded — it is useful beyond ratings (subtitle search).
	if it.IMDbID == nil || *it.IMDbID != "tt2543164" {
		t.Errorf("imdb_id = %v, want it stored regardless of the rating pass", it.IMDbID)
	}
}

// A movie whose provider record names a collection is linked into it, the
// collection media_item is created once with its own artwork, and it never
// enters the enrichment queue (ADR 0017).
func TestEnrichIngestsCollection(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id1 := addItem(t, st, lib, `C:\m\lotr1.mkv`, "Fellowship", 2001)
	id2 := addItem(t, st, lib, `C:\m\lotr2.mkv`, "Two Towers", 2002)

	rec := func(extID, title string) *meta.Record {
		return &meta.Record{
			Source: "fake", ExternalID: extID, Kind: meta.KindMovie,
			Fields: meta.Fields{Title: meta.S(title), Year: meta.I(2001)},
			Collection: &meta.CollectionRef{
				ExternalID: "119", Name: "The Lord of the Rings Collection",
				Artwork: []meta.ArtRef{{Kind: meta.ArtPoster, URL: "https://img/coll.jpg"}},
			},
		}
	}
	p := &fakeProvider{id: "fake", record: rec("120", "Fellowship"),
		cands: []meta.Candidate{{Provider: "fake", ExternalID: "120", Kind: meta.KindMovie, Title: "Fellowship", Year: 2001, Popularity: 40}}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	art := &fakeArt{}
	// Two movies enriched; the fake provider returns the same collection for
	// both. Enrich them one at a time so each Fetch can return its own record.
	w := New(st, reg, art, quietLog())
	w.Concurrency = 1
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cols, err := st.CollectionsOf(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Kind != "collection" || cols[0].Title != "The Lord of the Rings Collection" {
		t.Fatalf("CollectionsOf(id1) = %+v, want one LOTR collection", cols)
	}

	members, err := st.CollectionMembers(ctx, cols[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, m := range members {
		got[m.ID] = true
	}
	if !got[id1] || !got[id2] {
		t.Errorf("members = %v, want both movies %d and %d", got, id1, id2)
	}

	// The collection is resolved at birth and must not sit in the queue.
	pending, _ := st.PendingEnrichment(ctx, 10)
	for _, it := range pending {
		if it.Kind == "collection" {
			t.Errorf("collection %d is pending enrichment — it should be stamped resolved", it.ID)
		}
	}
	// The collection is not top-level? It is: a collection is a browse entry.
	top, _, _ := st.ListItems(ctx, store.ItemFilter{LibraryID: lib.ID, Kind: "collection", TopLevel: true})
	if len(top) != 1 {
		t.Errorf("top-level collections = %d, want 1", len(top))
	}
}

// The regression that defines the milestone, exercised end to end through the
// worker rather than only in the merge unit test.
func TestLockedFieldSurvivesEnrichment(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	// The user corrects the title and it locks.
	myTitle := "My Corrected Title"
	if err := st.UpdateItemMetadata(ctx, id, store.ItemMetadata{Title: &myTitle}); err != nil {
		t.Fatal(err)
	}
	if err := st.LockField(ctx, id, meta.FieldTitle); err != nil {
		t.Fatal(err)
	}
	// Requeue, as a refresh would.
	st.ClearMetadataStamp(ctx, 0, id)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.Title != myTitle {
		t.Fatalf("Title = %q — a locked field was overwritten by enrichment", it.Title)
	}
	// Locking one field must not freeze the rest.
	if it.Overview == nil {
		t.Error("Overview is nil — unlocked fields must still be filled in")
	}
}

func TestLocalSourceOutranksProvider(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry()
	reg.AddLocal(&fakeLocal{id: "nfo", rec: &meta.Record{
		Source: "nfo",
		Fields: meta.Fields{Title: meta.S("Title From NFO")},
	}})
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.Title != "Title From NFO" {
		t.Errorf("Title = %q, want the NFO value", it.Title)
	}
	// Fields the NFO is silent about still come from the provider.
	if it.Overview == nil {
		t.Error("Overview is nil — the provider should fill gaps the NFO leaves")
	}
}

// An item fully resolved from a sidecar is not "unmatched" — the user already
// said what it is, and listing it for review would bury real problems.
func TestLocalOnlyResolutionIsNotFlaggedForReview(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry() // no providers at all
	reg.AddLocal(&fakeLocal{id: "nfo", rec: &meta.Record{
		Source: "nfo",
		Fields: meta.Fields{Title: meta.S("Arrival"), Overview: meta.S("From the sidecar.")},
	}})

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	it, _ := st.GetItem(ctx, id, "local")
	if it.MatchState != meta.StateLocal {
		t.Errorf("MatchState = %q, want %q", it.MatchState, meta.StateLocal)
	}
	queue, _ := st.ReviewQueue(ctx, lib.ID, 10)
	if len(queue) != 0 {
		t.Errorf("review queue = %d, want 0 — a sidecar-resolved item needs no review", len(queue))
	}
}

// A weak match is recorded as uncertain rather than applied as fact.
func TestLowConfidenceGoesToReviewQueue(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\solaris.mkv`, "Solaris", 0) // no year: genuinely ambiguous

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Solaris", Year: 1972, Popularity: 12}},
		record: &meta.Record{Source: "fake", ExternalID: "1", Fields: meta.Fields{Title: meta.S("Solaris")}},
	})

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	queue, err := st.ReviewQueue(ctx, lib.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 {
		t.Fatalf("review queue = %d, want the ambiguous item flagged", len(queue))
	}
	if queue[0].MatchState != meta.StateReview {
		t.Errorf("MatchState = %q, want review", queue[0].MatchState)
	}
}

func TestNoMatchIsRecordedAsUnmatched(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\x.mkv`, "Totally Unknown Thing", 1999)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{id: "fake"}) // no candidates

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatal(err)
	}

	queue, _ := st.ReviewQueue(ctx, lib.ID, 10)
	if len(queue) != 1 || queue[0].MatchState != meta.StateUnmatched {
		t.Errorf("queue = %+v, want one unmatched item", queue)
	}
}

// With no provider configured, enrichment must be a quiet no-op that leaves
// items pending — not an error, and not a false "done" stamp.
func TestUnconfiguredProviderLeavesItemsPending(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\a.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry() // nothing registered

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatalf("Run with no providers returned an error: %v", err)
	}

	pending, _ := st.PendingEnrichment(ctx, 10)
	if len(pending) != 1 {
		t.Errorf("pending = %d, want the item still queued for a later run", len(pending))
	}
	_ = lib
}

// One item failing must not sink the batch.
func TestProviderErrorDoesNotFailTheRun(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\a.mkv`, "A", 2000)
	addItem(t, st, lib, `C:\m\b.mkv`, "B", 2001)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{id: "fake", err: context.DeadlineExceeded})

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Items stay pending and are retried on the next pass.
	pending, _ := st.PendingEnrichment(ctx, 10)
	if len(pending) != 2 {
		t.Errorf("pending = %d, want both items retried later", len(pending))
	}
}

func TestNFOWriteHookIsCalled(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	var wrote []string
	w := New(st, reg, &fakeArt{}, quietLog(), WithNFOWriter(
		func(path string, kind meta.Kind, rec *meta.Record) error {
			wrote = append(wrote, path)
			return nil
		}))

	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(wrote) != 1 || wrote[0] != `C:\m\arrival.mkv` {
		t.Errorf("nfo writer calls = %v, want the item path once", wrote)
	}
}

// Writing a sidecar is best-effort; the database is the working record.
func TestNFOWriteFailureDoesNotFailEnrichment(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	w := New(st, reg, &fakeArt{}, quietLog(), WithNFOWriter(
		func(string, meta.Kind, *meta.Record) error { return context.Canceled }))

	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	it, _ := st.GetItem(ctx, id, "local")
	if it.Overview == nil {
		t.Error("metadata was not applied despite only the sidecar write failing")
	}
}

// Remaining must be the real outstanding count, not the last batch size, and
// it must be recomputed when the run ends. Reporting "25 left" after the job
// is finished is how a progress display stops being believable.
func TestStatsRemainingIsAccurate(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	for i := 0; i < 3; i++ {
		addItem(t, st, lib, `C:\m\`+string(rune('a'+i))+`.mkv`, "Arrival", 2016)
	}

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}

	s := w.Stats()
	if s.Total != 3 {
		t.Errorf("Total = %d, want 3 — the job size when it started", s.Total)
	}
	if s.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0 once the queue is drained", s.Remaining)
	}
	if s.Enriched != 3 {
		t.Errorf("Enriched = %d, want 3", s.Enriched)
	}
}

// A run that stops early must still report what is genuinely left.
func TestStatsRemainingAfterNoProgress(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\a.mkv`, "Arrival", 2016)
	addItem(t, st, lib, `C:\m\b.mkv`, "Arrival", 2016)

	// No providers: nothing can be enriched, so nothing drains.
	w := New(st, meta.NewRegistry(), &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}

	s := w.Stats()
	if s.Remaining != 2 {
		t.Errorf("Remaining = %d, want 2 — the work is still outstanding", s.Remaining)
	}
	if s.Enriched != 0 {
		t.Errorf("Enriched = %d, want 0", s.Enriched)
	}
}

func TestStatsReported(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\a.mkv`, "Arrival", 2016)

	reg := meta.NewRegistry()
	reg.AddProvider(&fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "1", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	})

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}

	s := w.Stats()
	if s.Running {
		t.Error("Running = true after Run returned")
	}
	if s.Enriched != 1 {
		t.Errorf("Enriched = %d, want 1", s.Enriched)
	}
}

func TestCancelledContextStopsRun(t *testing.T) {
	st, lib := harness(t)
	addItem(t, st, lib, `C:\m\a.mkv`, "Arrival", 2016)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := meta.NewRegistry()
	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err == nil {
		t.Error("Run ignored a cancelled context")
	}
}

// ApplyMatch honours a candidate kind that differs from the item's own — a
// movie-scanned miniseries corrected to its TV entry fetches from /tv, and the
// TV metadata lands on the item even though it stays a movie.
func TestApplyMatchCrossKind(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\storm.mkv`, "Storm of the Century", 1999)

	p := &fakeProvider{id: "fake", record: &meta.Record{
		Source: "fake", ExternalID: "60622", Kind: meta.KindShow,
		Fields: meta.Fields{Title: meta.S("Storm of the Century"), Year: meta.I(1999),
			Overview: meta.S("A miniseries.")},
	}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	w := New(st, reg, &fakeArt{}, quietLog())

	it, _ := st.GetItem(ctx, id, "local")
	if err := w.ApplyMatch(ctx, *it, "fake", "60622", meta.KindShow); err != nil {
		t.Fatalf("ApplyMatch: %v", err)
	}
	// Fetched from the TV endpoint, not the item's movie kind.
	if p.lastFetch.Kind != meta.KindShow {
		t.Errorf("fetched kind = %q, want show", p.lastFetch.Kind)
	}
	got, _ := st.GetItem(ctx, id, "local")
	if got.Overview == nil || *got.Overview != "A miniseries." {
		t.Errorf("overview = %v, want the TV record applied", got.Overview)
	}
	if got.MatchState != meta.StateLocked {
		t.Errorf("match state = %q, want locked", got.MatchState)
	}
}

// ---------------------------------------------------------------- seasons

// seasonHarness builds a show with one season under it, and returns both ids.
// matched controls whether the show carries a provider identity, which is the
// only thing a season's own identity can be derived from.
func seasonHarness(t *testing.T, st *store.Store, lib *store.Library, seasonNum int, matched bool) (showID, seasonID int64) {
	t.Helper()
	ctx := context.Background()

	showID, _, err := st.EnsureShow(ctx, lib.ID, `C:\tv\The League`, "The League", "league")
	if err != nil {
		t.Fatal(err)
	}
	if matched {
		state, score := meta.StateMatched, 1.0
		provider, external := "fake", "31497"
		if err := st.UpdateItemMetadata(ctx, showID, store.ItemMetadata{
			Provider: &provider, ExternalID: &external,
			MatchState: &state, MatchScore: &score,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The folder name a real library actually carries, which is exactly the
	// string that used to be sent to the provider as a search query.
	title := "The League S0" + string(rune('0'+seasonNum))
	seasonID, _, err = st.EnsureSeason(ctx, lib.ID, showID, seasonNum,
		`C:\tv\The League\`+title, title, title)
	if err != nil {
		t.Fatal(err)
	}
	// EnsureSeason stamps a season resolved at birth so it never queues, which
	// is why this bug needed a trigger to surface at all: refreshing a
	// library's metadata clears every stamp in it, seasons included, and hands
	// them to the worker. That is the real path, so the test uses it.
	if err := st.ClearMetadataStamp(ctx, lib.ID, 0); err != nil {
		t.Fatal(err)
	}
	return showID, seasonID
}

// assertNoSeasonSearch fails if any search the worker issued was for a season.
// The show in the same library is searched normally; only the season must not
// be.
func assertNoSeasonSearch(t *testing.T, p *fakeProvider) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, q := range p.queries {
		if q.Kind == meta.KindSeason {
			t.Errorf("a season was searched for by name: %+v", q)
		}
	}
}

// The regression this whole path exists for. A season's name is a position
// inside a show, so searching for it returns whatever real show happens to
// carry "Season 2" in its title — an exact normalized title match that clears
// the auto-apply threshold. Because the query depends only on the season
// number, the same wrong show won for every show in the library: one Thai
// drama became the poster for season 2 of nine unrelated series.
func TestSeasonIsResolvedFromItsShowNeverSearched(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	_, seasonID := seasonHarness(t, st, lib, 2, true)

	p := &fakeProvider{
		id: "fake",
		// What a name search would have returned. If any of it lands on the
		// season row, the search happened.
		cands: []meta.Candidate{{
			Provider: "fake", ExternalID: "92299", Kind: meta.KindShow,
			Title: "Some Drama season 2", Year: 2019, Popularity: 1.1,
		}},
		record: &meta.Record{
			Source: "fake", ExternalID: "31497", Kind: meta.KindSeason,
			Fields: meta.Fields{
				Title:  meta.S("Season 2"),
				Season: meta.I(2),
			},
			Artwork: []meta.ArtRef{{Kind: meta.ArtPoster, URL: "https://img/s2.jpg"}},
		},
	}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertNoSeasonSearch(t, p)
	if p.lastFetch.Kind != meta.KindSeason {
		t.Errorf("fetch kind = %q, want season", p.lastFetch.Kind)
	}
	// The show's id with the season number applied to it — not an id a search
	// picked.
	if p.lastFetch.ExternalID != "31497" || p.lastFetch.Season != 2 {
		t.Errorf("fetch ref = %+v, want the show 31497 season 2", p.lastFetch)
	}

	it, err := st.GetItem(ctx, seasonID, "local")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Season 2" {
		t.Errorf("Title = %q, want the season's own name", it.Title)
	}
	if it.ExternalID == nil || *it.ExternalID != "31497" {
		t.Errorf("ExternalID = %v, want the show's", it.ExternalID)
	}
	if it.MatchState != meta.StateMatched {
		t.Errorf("MatchState = %q, want matched", it.MatchState)
	}
}

// A season under an unmatched show has nothing to be resolved from. It must
// stay pending rather than be stamped with a verdict — enriching the show later
// is what brings its seasons along, and recording "unmatched" here would strand
// them permanently.
func TestSeasonUnderUnmatchedShowStaysPending(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	_, seasonID := seasonHarness(t, st, lib, 2, false)

	p := &fakeProvider{id: "fake", record: &meta.Record{
		Source: "fake", ExternalID: "31497", Kind: meta.KindSeason,
		Fields: meta.Fields{Title: meta.S("Season 2")},
	}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	if err := New(st, reg, &fakeArt{}, quietLog()).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertNoSeasonSearch(t, p)
	if p.fetchN != 0 {
		t.Errorf("provider fetched %d times with no show id, want 0", p.fetchN)
	}

	pending, err := st.PendingEnrichment(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, it := range pending {
		if it.ID == seasonID {
			found = true
		}
	}
	if !found {
		t.Error("season left the pending queue; it must wait for its show")
	}
}
