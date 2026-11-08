package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

/*
 * Home preferences per account (ADR 0120, revision 92): nil until chosen,
 * changed one field at a time, and hidden built-in shelves kept beside the
 * pinned ones without disturbing them.
 */

func strp(s string) *string { return &s }
func i64p(n int64) *int64   { return &n }

func TestHomePrefsStartUnchosen(t *testing.T) {
	st, me, _ := pinFixture(t)
	p, err := st.HomePrefsFor(context.Background(), me)
	if err != nil {
		t.Fatal(err)
	}
	if p.Spotlight != nil || p.SpotlightItem != nil || p.Spoilers != nil {
		t.Errorf("a new account has chosen nothing, got %+v", p)
	}
	if p.HiddenShelves == nil || len(p.HiddenShelves) != 0 {
		t.Errorf("hidden shelves = %#v, want an empty list (never null in JSON)", p.HiddenShelves)
	}
}

func TestHomePrefsChangeOneFieldAtATime(t *testing.T) {
	st, me, _ := pinFixture(t)
	ctx := context.Background()
	if err := st.SetHomePrefs(ctx, me, HomePrefsUpdate{Spotlight: strp("pinned"), SpotlightItem: i64p(42)}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetHomePrefs(ctx, me, HomePrefsUpdate{Spoilers: strp("all")}); err != nil {
		t.Fatal(err)
	}
	p, err := st.HomePrefsFor(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if p.Spotlight == nil || *p.Spotlight != "pinned" || p.SpotlightItem == nil || *p.SpotlightItem != 42 {
		t.Errorf("setting spoilers must leave the spotlight alone, got %+v", p)
	}
	if p.Spoilers == nil || *p.Spoilers != "all" {
		t.Errorf("spoilers = %v, want all", p.Spoilers)
	}
}

func TestHiddenShelvesLeavePinnedShelvesAlone(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	if _, err := st.AddHomeShelf(ctx, me, lib, "Mine", "genre=Drama"); err != nil {
		t.Fatal(err)
	}
	hide := []string{"rate", "unwatched:1", "rate"}
	if err := st.SetHomePrefs(ctx, me, HomePrefsUpdate{
		HiddenShelves: &hide, HiddenLibraries: map[string]int64{"unwatched:1": lib},
	}); err != nil {
		t.Fatal(err)
	}
	p, err := st.HomePrefsFor(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"rate", "unwatched:1"}; !reflect.DeepEqual(p.HiddenShelves, want) {
		t.Errorf("hidden = %v, want %v (duplicates once)", p.HiddenShelves, want)
	}
	if got := shelfTitles(t, st, me); !reflect.DeepEqual(got, []string{"Mine"}) {
		t.Errorf("pinned shelves = %v, want just Mine: a hidden built-in is not a pinned shelf", got)
	}
	// A pinned shelf can still be added up to the limit: hidden rows do not count.
	for i := 1; i < MaxHomeShelves; i++ {
		if _, err := st.AddHomeShelf(ctx, me, lib, "More", "genre=Drama"); err != nil {
			t.Fatalf("pin %d: %v", i+1, err)
		}
	}
	// Replacing the list replaces it.
	none := []string{}
	if err := st.SetHomePrefs(ctx, me, HomePrefsUpdate{HiddenShelves: &none}); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.HomePrefsFor(ctx, me); len(p.HiddenShelves) != 0 {
		t.Errorf("hidden after clearing = %v", p.HiddenShelves)
	}
}

func TestHiddenUnwatchedShelfGoesWithItsLibrary(t *testing.T) {
	st, me, lib := pinFixture(t)
	ctx := context.Background()
	hide := []string{"watchlist", "unwatched:x"}
	if err := st.SetHomePrefs(ctx, me, HomePrefsUpdate{
		HiddenShelves: &hide, HiddenLibraries: map[string]int64{"unwatched:x": lib},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteLibrary(ctx, lib); err != nil {
		t.Fatal(err)
	}
	p, err := st.HomePrefsFor(ctx, me)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"watchlist"}; !reflect.DeepEqual(p.HiddenShelves, want) {
		t.Errorf("hidden after deleting the library = %v, want %v", p.HiddenShelves, want)
	}
}

func TestHomePrefsForNobody(t *testing.T) {
	st, _, _ := pinFixture(t)
	if _, err := st.HomePrefsFor(context.Background(), "u_nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("read for a missing account: %v, want ErrNotFound", err)
	}
	if err := st.SetHomePrefs(context.Background(), "u_nobody", HomePrefsUpdate{Spoilers: strp("show")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("write for a missing account: %v, want ErrNotFound", err)
	}
}
