package enrich

import (
	"context"
	"sync"
	"testing"

	"lancast/internal/meta"
)

/*
 * A library fetched in a language of its own (ADR 0123): every provider call
 * the worker makes for one of its items carries that language on its
 * context, and an item in a library with none carries nothing.
 */

// languageSpy records the language each call's context carried.
type languageSpy struct {
	fakeProvider
	mu    sync.Mutex
	langs []string
}

func (l *languageSpy) Search(ctx context.Context, q meta.Query) ([]meta.Candidate, error) {
	l.mu.Lock()
	l.langs = append(l.langs, "search:"+meta.LanguageFrom(ctx))
	l.mu.Unlock()
	return l.fakeProvider.Search(ctx, q)
}

func (l *languageSpy) Fetch(ctx context.Context, ref meta.Ref) (*meta.Record, error) {
	l.mu.Lock()
	l.langs = append(l.langs, "fetch:"+meta.LanguageFrom(ctx))
	l.mu.Unlock()
	return l.fakeProvider.Fetch(ctx, ref)
}

func TestEveryFetchCarriesItsLibrarysLanguage(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\m\arrival.mkv`, "Arrival", 2016)

	p := &languageSpy{fakeProvider: fakeProvider{
		id:     "fake",
		cands:  []meta.Candidate{{Provider: "fake", ExternalID: "329865", Kind: meta.KindMovie, Title: "Arrival", Year: 2016, Popularity: 40}},
		record: arrivalRecord(),
	}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	w := New(st, reg, &fakeArt{}, quietLog())
	w.LibraryLanguage = func(_ context.Context, libraryID int64) string {
		if libraryID == lib.ID {
			return "ja-JP"
		}
		return ""
	}
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(p.langs) == 0 {
		t.Fatal("the provider was never asked")
	}
	for _, l := range p.langs {
		if l != "search:ja-JP" && l != "fetch:ja-JP" {
			t.Errorf("calls = %v: one did not carry the library's language", p.langs)
			break
		}
	}

	// The by-id refresh goes through the same door.
	it, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	p.langs = nil
	if _, err := w.RefetchSettled(ctx, *it); err != nil {
		t.Fatal(err)
	}
	if len(p.langs) != 1 || p.langs[0] != "fetch:ja-JP" {
		t.Errorf("refetch calls = %v, want one fetch in ja-JP", p.langs)
	}
}
