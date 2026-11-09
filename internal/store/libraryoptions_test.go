package store

import (
	"context"
	"testing"
)

/*
 * A library's own options (ADR 0123): null by default, set and read back,
 * cleared by "", and a series override that is per library.
 */

func TestALibraryHasNoOptionsUntilSet(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, _, _, _ := seedLibrary(t, st)
	l, err := st.GetLibrary(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if l.MetadataLanguage != nil || l.ReadingDirection != nil {
		t.Errorf("options = %v, %v; want both null, which is the behaviour before ADR 0123", l.MetadataLanguage, l.ReadingDirection)
	}
	if code, err := st.LibraryLanguage(ctx, lib); err != nil || code != "" {
		t.Errorf("LibraryLanguage = %q, %v; want \"\" (follow the server)", code, err)
	}
}

func TestLibraryOptionsAreStoredAndCleared(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, _, _, _ := seedLibrary(t, st)
	if err := st.SetLibraryLanguage(ctx, lib, "ja-JP"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibraryDirection(ctx, lib, "rtl"); err != nil {
		t.Fatal(err)
	}
	libs, err := st.ListLibraries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got *Library
	for i := range libs {
		if libs[i].ID == lib {
			got = &libs[i]
		}
	}
	if got == nil || got.MetadataLanguage == nil || *got.MetadataLanguage != "ja-JP" ||
		got.ReadingDirection == nil || *got.ReadingDirection != "rtl" {
		t.Fatalf("listed library = %+v; want ja-JP and rtl", got)
	}
	if code, _ := st.LibraryLanguage(ctx, lib); code != "ja-JP" {
		t.Errorf("LibraryLanguage = %q, want ja-JP", code)
	}

	if err := st.SetLibraryLanguage(ctx, lib, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetLibraryDirection(ctx, lib, ""); err != nil {
		t.Fatal(err)
	}
	l, _ := st.GetLibrary(ctx, lib)
	if l.MetadataLanguage != nil || l.ReadingDirection != nil {
		t.Errorf("after clearing: %v, %v; want null", l.MetadataLanguage, l.ReadingDirection)
	}
	if err := st.SetLibraryDirection(ctx, 9999, "rtl"); err != ErrNotFound {
		t.Errorf("no such library: %v, want ErrNotFound", err)
	}
}

func TestASeriesOverrideIsPerLibraryAndRemovable(t *testing.T) {
	ctx := context.Background()
	st := openTestStore(t)
	lib, _, _, _ := seedLibrary(t, st)
	if err := st.SetSeriesDirection(ctx, lib, "Akira", "rtl"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSeriesDirection(ctx, lib, "Akira", "ltr"); err != nil {
		t.Fatal(err)
	}
	if dir, _ := st.SeriesDirection(ctx, lib, "Akira"); dir != "ltr" {
		t.Errorf("Akira = %q, want ltr (the second write replaces the first)", dir)
	}
	if dir, _ := st.SeriesDirection(ctx, lib, "Saga"); dir != "" {
		t.Errorf("Saga = %q, want none", dir)
	}
	if err := st.SetSeriesDirection(ctx, lib, "Akira", ""); err != nil {
		t.Fatal(err)
	}
	if dir, _ := st.SeriesDirection(ctx, lib, "Akira"); dir != "" {
		t.Errorf("after removing: %q, want none", dir)
	}
}
