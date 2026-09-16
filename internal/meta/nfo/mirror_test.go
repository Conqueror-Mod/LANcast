package nfo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/meta"
)

/*
 * A sidecar LANcast wrote must not read back as somebody's edit.
 *
 * Mirror detection is what stops our own output from being fed back in as
 * evidence: Read computes the digest of what it parsed and compares it to the
 * marker the writer stamped, and an unchanged file contributes nothing. The
 * whole design leans on that round trip being lossless.
 *
 * When it is not, the failure is silent and it is *worse than noise*. A file
 * that reads back as edited becomes authoritative, and an authoritative sidecar
 * outvotes the provider. So a title whose match was later corrected keeps
 * serving the old identity's data for ever, and the row and the file agree with
 * each other, which is exactly what makes it invisible from the UI.
 *
 * Found in a real library: four films — Batman & Robin, Batman, Steel and
 * Domino — each matched to a remake, written to disk, corrected to the right
 * film and locked, and still showing the remake's year and plot months later.
 * 204 sidecars in that library carry our marker; 193 happened to agree with
 * their match and so hid the fault.
 */

func TestOurOwnSidecarSaysNothingNew(t *testing.T) {
	/*
	 * The round trip, asserted directly. Write a record, read the file back,
	 * and the source must report *nothing* — not the same values again.
	 */
	dir := t.TempDir()
	path := filepath.Join(dir, "A Film (1999).mkv")
	if err := os.WriteFile(path, []byte("not really a film"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := &meta.Record{
		Source: "tmdb", ExternalID: "415", Kind: meta.KindMovie,
		Fields: meta.Fields{
			Title:      meta.S("Batman & Robin"),
			Year:       meta.I(1997),
			Overview:   meta.S("Batman and his sidekick Robin attempt to foil the schemes of new villains."),
			Rating:     meta.F(4.4),
			DurationMS: meta.I64(125 * 60 * 1000),
		},
		Genres:  []string{"Action", "Science Fiction", "Fantasy"},
		Credits: []meta.Credit{{Name: "George Clooney", Role: "actor", Character: "Batman"}},
	}

	s := New()
	if err := s.Write(path, meta.KindMovie, rec); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := s.Read(context.Background(), path, meta.KindMovie)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != nil {
		t.Errorf("our own sidecar read back as a user edit, carrying %+v.\n"+
			"An 'edited' sidecar is authoritative and outvotes the provider, so a "+
			"title whose match is later corrected keeps serving the old identity "+
			"for ever.", got.Fields)
	}
}

func TestARoundTripSurvivesEveryFieldWeWrite(t *testing.T) {
	/*
	 * The same claim, field by field, so a failure names the culprit instead of
	 * only saying the digests differ. Written as a table because the fault is
	 * almost certainly one field that does not survive being parsed back —
	 * and knowing *which* is the difference between a fix and a guess.
	 */
	cases := []struct {
		name string
		rec  *meta.Record
	}{
		{"a bare record", &meta.Record{
			Source: "tmdb", ExternalID: "1", Kind: meta.KindMovie,
			Fields: meta.Fields{Title: meta.S("Plain"), Year: meta.I(2001)},
		}},
		{"an apostrophe in the title", &meta.Record{
			Source: "tmdb", ExternalID: "2", Kind: meta.KindMovie,
			Fields: meta.Fields{Title: meta.S("A Bug's Life"), Year: meta.I(1998)},
		}},
		{"an ampersand in the title", &meta.Record{
			Source: "tmdb", ExternalID: "3", Kind: meta.KindMovie,
			Fields: meta.Fields{Title: meta.S("Batman & Robin"), Year: meta.I(1997)},
		}},
		{"a long overview", &meta.Record{
			Source: "tmdb", ExternalID: "4", Kind: meta.KindMovie,
			Fields: meta.Fields{
				Title:    meta.S("Wordy"),
				Year:     meta.I(2010),
				Overview: meta.S("This 15-chapter serial pits Batman and Robin against The Wizard, who uses a device that allows him to control machinery to hold the city hostage."),
			},
		}},
		{"genres and cast", &meta.Record{
			Source: "tmdb", ExternalID: "5", Kind: meta.KindMovie,
			Fields:  meta.Fields{Title: meta.S("Peopled"), Year: meta.I(1999)},
			Genres:  []string{"Action", "Crime", "Thriller"},
			Credits: []meta.Credit{{Name: "Robert Lowery", Role: "actor", Character: "Batman / Bruce Wayne"}},
		}},
		{"a duration and a rating", &meta.Record{
			Source: "tmdb", ExternalID: "6", Kind: meta.KindMovie,
			Fields: meta.Fields{
				Title: meta.S("Timed"), Year: meta.I(1949),
				DurationMS: meta.I64(263 * 60 * 1000), Rating: meta.F(5.7),
			},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "film.mkv")
			if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			s := New()
			if err := s.Write(path, meta.KindMovie, tc.rec); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := s.Read(context.Background(), path, meta.KindMovie)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got != nil {
				t.Errorf("read back as edited, reporting %+v", got.Fields)
			}
		})
	}
}

func TestRewritingASidecarTwiceIsStable(t *testing.T) {
	/*
	 * Writing over our own file must not make it look edited either. The writer
	 * preserves elements other tools put there and re-indents, and this is
	 * where the indentation-as-data bug lived before dropLayoutText — a file
	 * that grew escaped newlines on every pass would eventually differ from its
	 * own digest.
	 */
	dir := t.TempDir()
	path := filepath.Join(dir, "film.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &meta.Record{
		Source: "tmdb", ExternalID: "7", Kind: meta.KindMovie,
		Fields: meta.Fields{Title: meta.S("Twice"), Year: meta.I(2000),
			Overview: meta.S("Written more than once.")},
	}
	s := New()
	for i := 0; i < 3; i++ {
		if err := s.Write(path, meta.KindMovie, rec); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		got, err := s.Read(context.Background(), path, meta.KindMovie)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if got != nil {
			t.Fatalf("after %d writes the file read back as edited: %+v", i+1, got.Fields)
		}
	}
}

func TestTheRealStaleSidecarFromTheLibrary(t *testing.T) {
	/*
	 * The actual file, byte for byte, taken off the disk it misbehaved on.
	 *
	 * The synthetic round trips above all pass, so whatever went wrong is not
	 * in what this build writes. This fixture carries what an *older* build
	 * wrote: a marker with a hash and no per-field digests, and the escaped
	 * newline padding that predates dropLayoutText.
	 *
	 * The file describes the 1949 serial. The item it sits beside is the 1997
	 * film, matched to TMDB 415 and locked. If Read returns anything at all,
	 * that content is authoritative and outvotes the provider — which is
	 * exactly what was observed: the row showed 1949 and the serial's plot for
	 * months.
	 */
	dir := t.TempDir()
	path := filepath.Join(dir, "Batman and Robin.mp4")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "lancast-written-stale.nfo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Batman and Robin.nfo"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New().Read(context.Background(), path, meta.KindMovie)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != nil {
		var title, year, overview string
		if got.Fields.Title != nil {
			title = *got.Fields.Title
		}
		if got.Fields.Year != nil {
			year = intStr(got.Fields.Year)
		}
		if got.Fields.Overview != nil {
			overview = *got.Fields.Overview
		}
		t.Errorf("a sidecar LANcast wrote was treated as a person's edit and "+
			"became authoritative, contributing title=%q year=%s overview=%.50s...\n"+
			"That outvotes the provider, so the corrected match never reaches the row.",
			title, year, overview)
	}
}

func TestADurationThatIsNotAWholeNumberOfMinutes(t *testing.T) {
	/*
	 * The suspected mechanism, isolated.
	 *
	 * `<runtime>` is written in whole minutes. A real film is not a whole
	 * number of minutes — 125:18 is ordinary — so if the reader turns that back
	 * into milliseconds by multiplying, the record it parses cannot equal the
	 * record that was written, the digest differs, and mirror detection
	 * concludes a person edited the file.
	 *
	 * Every synthetic case above used an exact multiple of a minute, which is
	 * why they all passed. That is the fixture sharing an assumption with the
	 * code again.
	 */
	dir := t.TempDir()
	path := filepath.Join(dir, "film.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := &meta.Record{
		Source: "tmdb", ExternalID: "8", Kind: meta.KindMovie,
		Fields: meta.Fields{
			Title: meta.S("Odd Length"), Year: meta.I(1997),
			// 125 minutes and 18.123 seconds.
			DurationMS: meta.I64(125*60*1000 + 18_123),
		},
	}
	s := New()
	if err := s.Write(path, meta.KindMovie, rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(context.Background(), path, meta.KindMovie)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a duration of %d ms did not survive the round trip, so our own "+
			"sidecar reads as a user edit and becomes authoritative: %+v",
			*rec.Fields.DurationMS, got.Fields)
	}
}
