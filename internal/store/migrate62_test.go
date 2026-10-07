package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Revision 62 re-queues the episodes with no intro, and only those: a season
// whose every episode has one cannot be changed by a rule that runs only where
// nothing was decided, and re-decoding it would be ten minutes of audio per
// episode for nothing.
func TestRevision62RequeuesOnlyEpisodesWithoutAnIntro(t *testing.T) { checkIntroRequeue(t, "61") }

// Revision 63 does the same for the triangle rule, on a library that has
// already run 62 and stamped those episodes again.
func TestRevision63RequeuesOnlyEpisodesWithoutAnIntro(t *testing.T) { checkIntroRequeue(t, "62") }

func checkIntroRequeue(t *testing.T, from string) {
	t.Helper()
	ctx := context.Background()
	s := openTestStore(t)
	lib, err := s.CreateLibrary(ctx, "TV", "show", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "Show"
	var ids []int64
	for en := 1; en <= 2; en++ {
		sn, e := 1, en
		id, err := s.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(lib.Path, name, "e"+string(rune('0'+en))+".mkv"),
			Kind: "episode", Title: "Ep", SortTitle: name, Series: &name, Season: &sn, Episode: &e,
			Container: "mkv", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	withIntro, without := ids[0], ids[1]
	end := int64(30_000)
	if err := s.SaveMarkers(ctx, withIntro, []string{MarkerIntro},
		[]Marker{{Kind: MarkerIntro, StartMS: 0, EndMS: &end, Source: "fingerprint", Confidence: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkIntrosExamined(ctx, ids, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE meta SET value = ? WHERE key = 'schema_version'`, from); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	stamped := func(id int64) bool {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM media_item WHERE id = ? AND intros_at IS NOT NULL`, id).Scan(&n)
		return n == 1
	}
	if stamped(without) {
		t.Error("the episode with no intro was not re-queued")
	}
	if !stamped(withIntro) {
		t.Error("an episode that already has its intro was re-queued")
	}
}
