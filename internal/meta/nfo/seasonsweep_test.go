package nfo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/meta"
)

func seasonRecord() *meta.Record {
	return &meta.Record{
		Kind: meta.KindSeason,
		Fields: meta.Fields{
			Title:    meta.S("Season 1"),
			Overview: meta.S("The first season."),
		},
	}
}

// R08-5: a season folder must not acquire a tvshow.nfo, or Kodi and Jellyfin
// read every season as a show of its own.
func TestWriteSeasonWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Season 01")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := New().Write(dir, meta.KindSeason, seasonRecord()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("writing a season left %s in its folder", e.Name())
	}
}

// A tvshow.nfo in a season folder describes a show, so it is not the season's.
func TestReadSeasonIgnoresTVShowNFO(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(`<tvshow><title>Andor</title><plot>A rebellion.</plot></tvshow>`), 0o644)
	rec, err := New().Read(context.Background(), dir, meta.KindSeason)
	if err != nil {
		t.Fatal(err)
	}
	if rec != nil {
		t.Errorf("a season read the show's file as its own: title %q", *rec.Fields.Title)
	}
}

// oldSeasonSidecar reproduces what the earlier build wrote: applyRecord's
// elements under a <movie> root, stamped with the marker, as tvshow.nfo.
func oldSeasonSidecar(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Season 01")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Written through the movie path, which builds the same file the season
	// path did (both rooted <movie>, neither episode-only element), then moved
	// to the name the season path gave it.
	video := filepath.Join(dir, "x.mkv")
	if err := New().Write(video, meta.KindMovie, seasonRecord()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "x.nfo"), filepath.Join(dir, "tvshow.nfo")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "tvshow.nfo"))
	if !strings.Contains(string(raw), "<movie>") || !strings.Contains(string(raw), "<lancast ") {
		t.Fatalf("fixture is not the old season sidecar:\n%s", raw)
	}
	return dir
}

func TestRemoveStraySeasonSidecarRemovesOurs(t *testing.T) {
	dir := oldSeasonSidecar(t)
	removed, err := RemoveStraySeasonSidecar(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("the sidecar LANcast wrote was not removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "tvshow.nfo")); !os.IsNotExist(err) {
		t.Errorf("tvshow.nfo still there: %v", err)
	}
}

func TestRemoveStraySeasonSidecarKeepsWhatIsNotProvablyOurs(t *testing.T) {
	cases := map[string]func(string) string{
		"edited by a person": func(s string) string {
			return strings.Replace(s, "<title>Season 1</title>", "<title>Book One: Water</title>", 1)
		},
		"another tool added an element": func(s string) string {
			return strings.Replace(s, "</movie>", "  <uniqueid type=\"tvdb\">1</uniqueid>\n</movie>", 1)
		},
		"no marker": func(s string) string { return removeMarker(s) },
		"a real show's file": func(s string) string {
			return strings.ReplaceAll(s, "movie>", "tvshow>")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			dir := oldSeasonSidecar(t)
			file := filepath.Join(dir, "tvshow.nfo")
			raw, _ := os.ReadFile(file)
			changed := mutate(string(raw))
			if changed == string(raw) {
				t.Fatal("mutation did not apply")
			}
			os.WriteFile(file, []byte(changed), 0o644)

			removed, err := RemoveStraySeasonSidecar(dir)
			if err != nil {
				t.Fatal(err)
			}
			if removed {
				t.Error("removed a file it could not prove LANcast wrote unchanged")
			}
			after, err := os.ReadFile(file)
			if err != nil || string(after) != changed {
				t.Errorf("file was touched: %v", err)
			}
		})
	}
}

func TestRemoveStraySeasonSidecarNoFile(t *testing.T) {
	removed, err := RemoveStraySeasonSidecar(t.TempDir())
	if err != nil || removed {
		t.Errorf("empty folder: removed=%v err=%v", removed, err)
	}
}
