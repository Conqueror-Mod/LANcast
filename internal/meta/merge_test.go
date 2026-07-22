package meta

import "testing"

func rec(source string, f Fields) Record {
	return Record{Source: source, Fields: f}
}

func TestMergePrecedenceLocalBeatsRemote(t *testing.T) {
	current := rec("db", Fields{Title: S("guess from filename")})
	local := rec("nfo", Fields{Title: S("From NFO")})
	remote := rec("tmdb", Fields{Title: S("From TMDB"), Overview: S("synopsis")})

	out := Merge(current, nil, []Record{local}, []Record{remote})

	if *out.Fields.Title != "From NFO" {
		t.Errorf("Title = %q, want the NFO value — local sources outrank providers", *out.Fields.Title)
	}
	// A field the local source says nothing about still comes from the provider.
	if out.Fields.Overview == nil || *out.Fields.Overview != "synopsis" {
		t.Errorf("Overview = %v, want the provider value", out.Fields.Overview)
	}
}

// The regression that defines the milestone: a locked field is never
// overwritten, however good the incoming data is.
func TestMergeNeverOverwritesLockedField(t *testing.T) {
	current := rec("db", Fields{Title: S("My Correction"), Year: I(1999)})
	locked := LockedSet([]string{FieldTitle})

	out := Merge(current, locked,
		[]Record{rec("nfo", Fields{Title: S("NFO Title"), Year: I(2001)})},
		[]Record{rec("tmdb", Fields{Title: S("TMDB Title"), Year: I(2002)})})

	if *out.Fields.Title != "My Correction" {
		t.Fatalf("Title = %q — a locked field was overwritten", *out.Fields.Title)
	}
	// Locking one field must not freeze the rest. That is the whole reason
	// locking is per-field rather than per-item (ADR 0008).
	if *out.Fields.Year != 2001 {
		t.Errorf("Year = %d, want 2001 — unlocked fields must still update", *out.Fields.Year)
	}
}

func TestMergeLockedListFields(t *testing.T) {
	current := Record{Genres: []string{"Kept"}, Credits: []Credit{{Name: "Kept"}}}
	locked := LockedSet([]string{FieldGenres})

	out := Merge(current, locked, nil, []Record{{
		Genres:  []string{"New"},
		Credits: []Credit{{Name: "New"}},
	}})

	if len(out.Genres) != 1 || out.Genres[0] != "Kept" {
		t.Errorf("Genres = %v, want the locked value", out.Genres)
	}
	if len(out.Credits) != 1 || out.Credits[0].Name != "New" {
		t.Errorf("Credits = %v, want the provider value (unlocked)", out.Credits)
	}
}

func TestMergeKeepsCurrentWhenNoSourceHasField(t *testing.T) {
	current := rec("db", Fields{Title: S("Only Value"), Year: I(1988)})
	out := Merge(current, nil, nil, []Record{rec("tmdb", Fields{Overview: S("x")})})

	if *out.Fields.Title != "Only Value" {
		t.Errorf("Title = %q, want the existing value preserved", *out.Fields.Title)
	}
	if *out.Fields.Year != 1988 {
		t.Errorf("Year = %d, want the existing value preserved", *out.Fields.Year)
	}
}

func TestMergeFirstSourceInTierWins(t *testing.T) {
	out := Merge(Record{}, nil, nil, []Record{
		rec("first", Fields{Title: S("First")}),
		rec("second", Fields{Title: S("Second")}),
	})
	if *out.Fields.Title != "First" {
		t.Errorf("Title = %q, want First — priority order must be respected", *out.Fields.Title)
	}
}

func TestMergeRecordsProvenance(t *testing.T) {
	out := Merge(Record{}, nil,
		[]Record{{Source: "nfo", Fields: Fields{Title: S("T")}}},
		[]Record{{Source: "tmdb", ExternalID: "335984", Fields: Fields{Overview: S("o")}}})

	if out.Source != "tmdb" || out.ExternalID != "335984" {
		t.Errorf("provenance = %s/%s, want tmdb/335984 — needed to refresh or re-match later",
			out.Source, out.ExternalID)
	}
}

func TestMergeEmptySourcesIsIdentity(t *testing.T) {
	current := rec("db", Fields{Title: S("Unchanged"), Year: I(2000)})
	out := Merge(current, nil, nil, nil)
	if *out.Fields.Title != "Unchanged" || *out.Fields.Year != 2000 {
		t.Errorf("merge with no sources changed the record: %+v", out.Fields)
	}
}

func TestMergeAllFieldsLockedIsNoOp(t *testing.T) {
	current := rec("db", Fields{Title: S("A"), Year: I(1), Overview: S("B"), Rating: F(5)})
	out := Merge(current, LockedSet(AllFields), nil,
		[]Record{rec("tmdb", Fields{Title: S("X"), Year: I(2), Overview: S("Y"), Rating: F(9)})})

	if *out.Fields.Title != "A" || *out.Fields.Year != 1 ||
		*out.Fields.Overview != "B" || *out.Fields.Rating != 5 {
		t.Errorf("a fully locked item was modified: %+v", out.Fields)
	}
}

func TestLockedSetIgnoresUnknownFields(t *testing.T) {
	got := LockedSet([]string{FieldTitle, "not_a_field", "'; DROP TABLE media_item; --"})
	if len(got) != 1 || !got[FieldTitle] {
		t.Errorf("LockedSet = %v, want only the valid field", got)
	}
}

func TestIsField(t *testing.T) {
	if !IsField(FieldTitle) {
		t.Error("title should be a valid field")
	}
	if IsField("path") {
		t.Error("path must not be lockable — it is not user metadata")
	}
}
