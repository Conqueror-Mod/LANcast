package enrich

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

// fakeProvider serves canned candidates and records without a network.
type fakeProvider struct {
	id      string
	cands   []meta.Candidate
	record  *meta.Record
	searchN int
	fetchN  int
	err     error
}

func (f *fakeProvider) ID() string      { return f.id }
func (f *fakeProvider) Caps() meta.Caps { return meta.Caps{Movie: true, Show: true, Episode: true} }

func (f *fakeProvider) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
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
