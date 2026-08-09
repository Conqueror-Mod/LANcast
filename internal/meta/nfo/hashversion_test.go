package nfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/meta"
)

// The digest carries the scheme that produced it. Without that, adding one field
// to FieldsHash would make every sidecar LANcast has ever written look edited —
// and every one of them would be promoted to authority over every provider,
// silently, on every machine, all at once.
func TestHashCarriesItsVersion(t *testing.T) {
	h := FieldsHash(movieRecord())
	if !strings.HasPrefix(h, "sha256:v") {
		t.Errorf("hash %q does not name its scheme", h)
	}
	v, ok := hashSchemeOf(h)
	if !ok || v != hashVersion {
		t.Errorf("hashSchemeOf(%q) = %d, %v; want %d, true", h, v, ok, hashVersion)
	}
}

// Files written before versioning carry "sha256:<hex>" and were produced by
// exactly the scheme now called v1, so reading them as v1 is a fact rather than
// an assumption — and those files must keep being recognised as ours.
func TestUnversionedHashIsReadAsVersionOne(t *testing.T) {
	v, ok := hashSchemeOf("sha256:d3ba091b1a7bd9b91aad6befc1f876e644397226608dc8ad51f65d39e8fc09ab")
	if !ok || v != 1 {
		t.Errorf("legacy hash read as %d, %v; want 1, true", v, ok)
	}
}

// The rule the whole change turns on: an edit must be *proven*, not assumed.
func TestOnlyAVerifiableMismatchProvesAnEdit(t *testing.T) {
	rec := movieRecord()
	mine := FieldsHash(rec)

	cases := []struct {
		name   string
		marker string
		want   bool
	}{
		{"our own current hash", mine, false},
		{"a real edit under a scheme we can check", "sha256:v1:0000", true},
		// The case that matters. A future build's scheme is not evidence a human
		// touched the file; treating it as one would re-pin every identity to a
		// stale sidecar.
		{"a scheme from a newer build", "sha256:v99:0000", false},
		// A v1 marker is still verifiable: the digest did not change when the
		// marker gained per-field information, and treating every older sidecar
		// as unverifiable would stop honouring edits on files already on disk.
		{"an older scheme we still implement", "sha256:v1:0000", true},
		{"malformed", "sha256:", false},
		{"not ours at all", "kodi-generated", false},
		{"absent", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := provesUserEdit(tc.marker, rec); got != tc.want {
				t.Errorf("provesUserEdit(%q) = %v, want %v", tc.marker, got, tc.want)
			}
		})
	}
}

// End to end: a sidecar LANcast wrote and nobody touched must stay silent, and
// the same file with a hand-edited title must speak.
func TestReadHonoursEditsAndIgnoresOurOwnOutput(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	if err := New().Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(dir, "Arrival.nfo")

	rec, err := New().Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("our own untouched sidecar was treated as authority: %+v", rec.Fields.Title)
	}

	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "<title>Arrival</title>",
		"<title>Arrival (Director's Cut)</title>", 1)
	if edited == string(raw) {
		t.Fatal("test did not actually change the file")
	}
	if err := os.WriteFile(sidecar, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err = New().Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("a hand-edited sidecar was ignored; the user's edit must win")
	}
	if got := deref(rec.Fields.Title); got != "Arrival (Director's Cut)" {
		t.Errorf("title = %q, want the edited one", got)
	}
}

// The case this whole change exists for, and the one that actually bit Chris.
//
// A sidecar LANcast wrote is corrected by hand — one field, the title. Before
// per-field digests the entire file became authoritative, so the plot, cast and
// rating beside it were promoted too, including anything LANcast had got wrong
// that nobody touched. Now the correction costs only the field corrected.
func TestOnlyTheEditedFieldIsAuthored(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	if err := New().Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(dir, "Arrival.nfo")

	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "<title>Arrival</title>",
		"<title>Arrival (1998)</title>", 1)
	if edited == string(raw) {
		t.Fatal("the test did not change the file")
	}
	if err := os.WriteFile(sidecar, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := New().Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("the edit was ignored; a hand-edited field must win")
	}

	if got := deref(rec.Fields.Title); got != "Arrival (1998)" {
		t.Errorf("title = %q, want the edited value", got)
	}
	// Everything else must be absent, so providers keep filling it. Present
	// here would mean LANcast's own output had been promoted to authority by
	// somebody fixing a typo.
	if rec.Fields.Overview != nil {
		t.Errorf("overview came back as authored (%q); only the title was edited",
			deref(rec.Fields.Overview))
	}
	if rec.Fields.Rating != nil {
		t.Error("rating came back as authored; only the title was edited")
	}
	if len(rec.Credits) != 0 {
		t.Errorf("%d credits came back as authored; only the title was edited", len(rec.Credits))
	}
	if len(rec.Genres) != 0 {
		t.Errorf("%d genres came back as authored; only the title was edited", len(rec.Genres))
	}
}

// A file nobody touched still says nothing at all, per-field digests or not.
func TestUneditedFileStillSaysNothing(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	if err := New().Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}
	rec, err := New().Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("our own untouched output was treated as authored: %+v", rec.Fields)
	}
}

// A sidecar from another tool has no marker and no digests, so all of it is
// authoritative — unchanged, and the reason LANcast can be a guest in a format
// it did not invent.
func TestForeignSidecarIsWhollyAuthoritative(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	foreign := `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Arrival</title>
  <plot>Written by another tool entirely.</plot>
</movie>`
	if err := os.WriteFile(filepath.Join(dir, "Arrival.nfo"), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	rec, err := New().Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("a foreign sidecar was ignored")
	}
	if deref(rec.Fields.Overview) != "Written by another tool entirely." {
		t.Errorf("overview = %q, want the foreign file's", deref(rec.Fields.Overview))
	}
}
