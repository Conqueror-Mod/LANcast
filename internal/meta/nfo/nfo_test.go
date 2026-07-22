package nfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/meta"
)

func movieRecord() *meta.Record {
	return &meta.Record{
		Kind: meta.KindMovie,
		Fields: meta.Fields{
			Title:      meta.S("Arrival"),
			Year:       meta.I(2016),
			Overview:   meta.S("A linguist makes contact."),
			Rating:     meta.F(7.6),
			DurationMS: meta.I64(116 * 60_000),
		},
		Genres:  []string{"Science Fiction", "Drama"},
		Credits: []meta.Credit{{Name: "Amy Adams", Role: "actor", Character: "Louise Banks"}},
	}
}

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadMovieNFO(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Arrival.nfo"), []byte(`<?xml version="1.0"?>
<movie>
  <title>Arrival</title>
  <year>2016</year>
  <plot>A linguist makes contact.</plot>
  <rating>7.6</rating>
  <mpaa>PG-13</mpaa>
  <runtime>116</runtime>
  <premiered>2016-11-11</premiered>
  <genre>Science Fiction</genre>
  <genre>Drama</genre>
  <actor><name>Amy Adams</name><role>Louise Banks</role></actor>
  <director>Denis Villeneuve</director>
</movie>`), 0o644)

	rec, err := New().Read(context.Background(), filepath.Join(dir, "Arrival.mkv"), meta.KindMovie)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rec == nil {
		t.Fatal("Read returned nil for a hand-written NFO")
	}

	if *rec.Fields.Title != "Arrival" {
		t.Errorf("Title = %q", *rec.Fields.Title)
	}
	if *rec.Fields.Year != 2016 {
		t.Errorf("Year = %d", *rec.Fields.Year)
	}
	if *rec.Fields.Rating != 7.6 {
		t.Errorf("Rating = %v", *rec.Fields.Rating)
	}
	if *rec.Fields.ContentRating != "PG-13" {
		t.Errorf("ContentRating = %q", *rec.Fields.ContentRating)
	}
	if *rec.Fields.DurationMS != 116*60_000 {
		t.Errorf("DurationMS = %d, want runtime converted from minutes", *rec.Fields.DurationMS)
	}
	if rec.Fields.ReleasedAt == nil {
		t.Error("premiered was not parsed")
	}
	if len(rec.Genres) != 2 {
		t.Errorf("Genres = %v", rec.Genres)
	}
	if len(rec.Credits) != 2 {
		t.Errorf("Credits = %+v, want actor and director", rec.Credits)
	}
	if rec.Source != ID {
		t.Errorf("Source = %q, want %q", rec.Source, ID)
	}
}

func TestReadEpisodeNFO(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Andor.S01E07.nfo"), []byte(`<episodedetails>
  <title>Announcement</title><showtitle>Andor</showtitle>
  <season>1</season><episode>7</episode><aired>2022-10-26</aired>
</episodedetails>`), 0o644)

	rec, err := New().Read(context.Background(), filepath.Join(dir, "Andor.S01E07.mkv"), meta.KindEpisode)
	if err != nil || rec == nil {
		t.Fatalf("Read = %v, %v", rec, err)
	}
	if *rec.Fields.Season != 1 || *rec.Fields.Episode != 7 {
		t.Errorf("numbering = %d/%d, want 1/7", *rec.Fields.Season, *rec.Fields.Episode)
	}
	if *rec.Fields.Series != "Andor" {
		t.Errorf("Series = %q", *rec.Fields.Series)
	}
}

func TestReadShowNFOFromDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(`<tvshow><title>Andor</title><plot>A rebellion.</plot></tvshow>`), 0o644)

	rec, err := New().Read(context.Background(), dir, meta.KindShow)
	if err != nil || rec == nil {
		t.Fatalf("Read = %v, %v", rec, err)
	}
	if *rec.Fields.Title != "Andor" {
		t.Errorf("Title = %q", *rec.Fields.Title)
	}
}

// Other tools write movie.nfo rather than <basename>.nfo.
func TestReadFallsBackToMovieNFO(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(`<movie><title>Fallback</title></movie>`), 0o644)

	rec, _ := New().Read(context.Background(), filepath.Join(dir, "Something.mkv"), meta.KindMovie)
	if rec == nil || *rec.Fields.Title != "Fallback" {
		t.Errorf("rec = %+v, want the movie.nfo fallback to be used", rec)
	}
}

func TestReadMissingSidecarIsNotAnError(t *testing.T) {
	rec, err := New().Read(context.Background(), filepath.Join(t.TempDir(), "nothing.mkv"), meta.KindMovie)
	if err != nil || rec != nil {
		t.Errorf("Read = %v, %v; want nil, nil", rec, err)
	}
}

// A malformed sidecar is the user's file, not our bug. It must not fail the
// enrichment run.
func TestMalformedNFOIsIgnored(t *testing.T) {
	path := writeTemp(t, "Broken.nfo", `<movie><title>Unclosed`)
	rec, err := New().Read(context.Background(), strings.TrimSuffix(path, ".nfo")+".mkv", meta.KindMovie)
	if err != nil {
		t.Errorf("malformed NFO returned an error: %v", err)
	}
	if rec != nil {
		t.Errorf("malformed NFO produced a record: %+v", rec)
	}
}

func TestEmptyNFOYieldsNoRecord(t *testing.T) {
	path := writeTemp(t, "Empty.nfo", `<movie></movie>`)
	rec, _ := New().Read(context.Background(), strings.TrimSuffix(path, ".nfo")+".mkv", meta.KindMovie)
	if rec != nil {
		t.Errorf("empty NFO produced a record: %+v", rec)
	}
}

// The core of ADR 0009: LANcast must recognize its own unmodified output and
// treat it as a mirror, so provider updates are not frozen forever.
func TestOwnOutputIsTreatedAsMirror(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	src := New()

	if err := src.Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rec, err := src.Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if rec != nil {
		t.Fatal("LANcast's own output was treated as authoritative — the item would be frozen against provider updates")
	}
}

// A human editing the sidecar is making a deliberate statement about their
// library, and it must win.
func TestHandEditedFileBecomesAuthoritative(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	src := New()

	if err := src.Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}

	sidecar := filepath.Join(dir, "Arrival.nfo")
	raw, _ := os.ReadFile(sidecar)
	edited := strings.Replace(string(raw), "<title>Arrival</title>", "<title>My Better Title</title>", 1)
	if edited == string(raw) {
		t.Fatal("test setup failed: the title was not replaced")
	}
	os.WriteFile(sidecar, []byte(edited), 0o644)

	rec, err := src.Read(context.Background(), media, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("a hand-edited NFO was still treated as a mirror")
	}
	if *rec.Fields.Title != "My Better Title" {
		t.Errorf("Title = %q, want the hand-edited value", *rec.Fields.Title)
	}
}

// A file from Kodi or another tool has no marker and is authoritative.
func TestUnmarkedFileIsAuthoritative(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Foreign.nfo"),
		[]byte(`<movie><title>From Kodi</title></movie>`), 0o644)

	rec, _ := New().Read(context.Background(), filepath.Join(dir, "Foreign.mkv"), meta.KindMovie)
	if rec == nil {
		t.Fatal("an unmarked third-party NFO was ignored")
	}
	if *rec.Fields.Title != "From Kodi" {
		t.Errorf("Title = %q", *rec.Fields.Title)
	}
}

// LANcast is a guest in a file format it did not invent; other tools' data is
// not ours to discard.
func TestUnknownElementsSurviveAWrite(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	sidecar := filepath.Join(dir, "Arrival.nfo")

	os.WriteFile(sidecar, []byte(`<movie>
  <title>Old</title>
  <customtag>keep me</customtag>
  <kodiaddonfield>also keep</kodiaddonfield>
</movie>`), 0o644)

	if err := New().Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}

	out, _ := os.ReadFile(sidecar)
	body := string(out)
	for _, want := range []string{"customtag", "keep me", "kodiaddonfield", "also keep"} {
		if !strings.Contains(body, want) {
			t.Errorf("write discarded %q from another tool:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "<title>Arrival</title>") {
		t.Errorf("the new title was not written:\n%s", body)
	}
}

func TestWriteThenReadRoundTripsValues(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	src := New()

	if err := src.Write(media, meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}

	// Strip the marker so Read parses it as a foreign file and returns values.
	sidecar := filepath.Join(dir, "Arrival.nfo")
	raw, _ := os.ReadFile(sidecar)
	stripped := removeMarker(string(raw))
	os.WriteFile(sidecar, []byte(stripped), 0o644)

	rec, err := src.Read(context.Background(), media, meta.KindMovie)
	if err != nil || rec == nil {
		t.Fatalf("Read = %v, %v", rec, err)
	}
	if *rec.Fields.Title != "Arrival" || *rec.Fields.Year != 2016 {
		t.Errorf("round trip lost values: %+v", rec.Fields)
	}
	if *rec.Fields.Rating != 7.6 {
		t.Errorf("Rating round trip = %v, want 7.6", *rec.Fields.Rating)
	}
	if len(rec.Genres) != 2 {
		t.Errorf("Genres = %v, want 2", rec.Genres)
	}
	if len(rec.Credits) != 1 || rec.Credits[0].Character != "Louise Banks" {
		t.Errorf("Credits = %+v", rec.Credits)
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Arrival.mkv")
	src := New()

	src.Write(media, meta.KindMovie, movieRecord())
	first, _ := os.ReadFile(filepath.Join(dir, "Arrival.nfo"))

	src.Write(media, meta.KindMovie, movieRecord())
	second, _ := os.ReadFile(filepath.Join(dir, "Arrival.nfo"))

	// Only the generated timestamp may differ; the element structure must not
	// accumulate duplicates.
	if strings.Count(string(second), "<genre>") != strings.Count(string(first), "<genre>") {
		t.Errorf("repeated writes duplicated elements:\n%s", second)
	}
	if strings.Count(string(second), "<title>") != 1 {
		t.Errorf("repeated writes duplicated <title>:\n%s", second)
	}
	if strings.Count(string(second), "<"+MarkerElement) != 1 {
		t.Errorf("repeated writes duplicated the marker:\n%s", second)
	}
}

func TestWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := New().Write(filepath.Join(dir, "Arrival.mkv"), meta.KindMovie, movieRecord()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".lancast-") {
			t.Errorf("atomic write left a temp file behind: %s", e.Name())
		}
	}
}

func TestFieldsHashIsStableAndSensitive(t *testing.T) {
	a := movieRecord()
	b := movieRecord()
	if FieldsHash(a) != FieldsHash(b) {
		t.Error("identical records hashed differently — every file would look edited")
	}

	b.Fields.Title = meta.S("Different")
	if FieldsHash(a) == FieldsHash(b) {
		t.Error("a changed title did not change the hash — edits would be invisible")
	}

	// Ordering must not matter, or a provider reordering genres would look like
	// a user edit.
	c := movieRecord()
	c.Genres = []string{"Drama", "Science Fiction"}
	if FieldsHash(a) != FieldsHash(c) {
		t.Error("genre order changed the hash")
	}

	if FieldsHash(nil) != "" {
		t.Error("nil record should hash to the empty string")
	}
}

func TestWriteShowUsesDirectory(t *testing.T) {
	dir := t.TempDir()
	rec := &meta.Record{Kind: meta.KindShow, Fields: meta.Fields{Title: meta.S("Andor")}}
	if err := New().Write(dir, meta.KindShow, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tvshow.nfo")); err != nil {
		t.Errorf("tvshow.nfo was not written into the show directory: %v", err)
	}
}

// removeMarker strips the whole provenance element — opening tag through
// closing tag — so a test can simulate a foreign file without hand-building
// one. Leaving a stray closing tag would produce malformed XML, which Read
// correctly ignores, making the test look like a code failure.
func removeMarker(s string) string {
	start := strings.Index(s, "<"+MarkerElement)
	if start < 0 {
		return s
	}
	closing := "</" + MarkerElement + ">"
	if end := strings.Index(s[start:], closing); end >= 0 {
		return s[:start] + s[start+end+len(closing):]
	}
	// Self-closing form.
	if end := strings.Index(s[start:], "/>"); end >= 0 {
		return s[:start] + s[start+end+2:]
	}
	return s
}
