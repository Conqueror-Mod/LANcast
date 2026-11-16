package enrich

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/meta/nfo"
)

// R08-5: the first run removes the tvshow.nfo an earlier build wrote into a
// season folder, and leaves the show's own and anyone else's alone.
func TestRunSweepsStraySeasonSidecars(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)

	show := filepath.Join(t.TempDir(), "Andor")
	season := filepath.Join(show, "Season 01")
	other := filepath.Join(show, "Season 02")
	for _, d := range []string{season, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	showID, _, err := st.EnsureShow(ctx, lib.ID, show, "Andor", "Andor")
	if err != nil {
		t.Fatal(err)
	}
	for n, d := range map[int]string{1: season, 2: other} {
		if _, _, err := st.EnsureSeason(ctx, lib.ID, showID, n, d, "Season", "Season"); err != nil {
			t.Fatal(err)
		}
	}

	// What the earlier build wrote: a marked <movie> sidecar named tvshow.nfo.
	theirs := `<tvshow><title>Andor</title></tvshow>`
	showFile := filepath.Join(show, "tvshow.nfo")
	os.WriteFile(showFile, []byte(theirs), 0o644)
	os.WriteFile(filepath.Join(other, "tvshow.nfo"), []byte(theirs), 0o644)
	stray := filepath.Join(season, "tvshow.nfo")
	writeOldSeasonSidecar(t, season)

	w := New(st, meta.NewRegistry(), &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("the stray season sidecar was not removed: %v", err)
	}
	for _, f := range []string{showFile, filepath.Join(other, "tvshow.nfo")} {
		if b, err := os.ReadFile(f); err != nil || string(b) != theirs {
			t.Errorf("%s was touched: %v", f, err)
		}
	}
}

func writeOldSeasonSidecar(t *testing.T, dir string) {
	t.Helper()
	rec := &meta.Record{Kind: meta.KindSeason, Fields: meta.Fields{Title: meta.S("Season 1")}}
	video := filepath.Join(dir, "x.mkv")
	if err := nfo.New().Write(video, meta.KindMovie, rec); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "x.nfo"), filepath.Join(dir, "tvshow.nfo")); err != nil {
		t.Fatal(err)
	}
}
