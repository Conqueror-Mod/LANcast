package store

import (
	"context"
	"testing"
	"time"
)

/*
 * The photo timeline.
 *
 * A folder grid answers "where did I put it"; a timeline answers "when was
 * that", and a holiday spread across three folders is one week. These cover the
 * three things that make the answer trustworthy: the months are the *capture*
 * months rather than the import months, undated photographs are a bucket rather
 * than an error, and a sensitive folder is not in it at all.
 */

func atLocal(t *testing.T, y int, m time.Month, d int) int64 {
	t.Helper()
	return time.Date(y, m, d, 12, 0, 0, 0, time.Local).Unix()
}

type shots struct {
	library int64
	folder  int64
}

func makeShots(t *testing.T, s *Store) shots {
	t.Helper()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", `C:\pics`)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := s.EnsureDerivedContainer(ctx, lib.ID, "gallery",
		`C:\pics\Holiday`, "Holiday", "holiday", nil)
	if err != nil {
		t.Fatal(err)
	}
	return shots{library: lib.ID, folder: folder}
}

// photo inserts one, with an optional capture time and parent.
func (sh shots) photo(t *testing.T, s *Store, name string, taken *int64, parent *int64) int64 {
	t.Helper()
	res, err := s.db.Exec(`
		INSERT INTO media_item (library_id, kind, path, title, sort_title,
			parent_id, taken_at, added_at, updated_at, missing)
		VALUES (?, 'photo', ?, ?, ?, ?, ?, 1, 1, 0)`,
		sh.library, `C:\pics\`+name, name, name, parent, taken)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func find(buckets []TimelineBucket, y, m int) (TimelineBucket, bool) {
	for _, b := range buckets {
		if b.Year == y && b.Month == m && !b.Undated {
			return b, true
		}
	}
	return TimelineBucket{}, false
}

// The feature: photographs grouped by the month they were taken.
func TestTheTimelineGroupsByCaptureMonth(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()

	july := atLocal(t, 2019, time.July, 4)
	aug := atLocal(t, 2019, time.August, 9)
	sh.photo(t, s, "a.jpg", &july, &sh.folder)
	sh.photo(t, s, "b.jpg", &july, &sh.folder)
	sh.photo(t, s, "c.jpg", &aug, &sh.folder)

	buckets, err := s.PhotoTimeline(ctx, sh.library)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := find(buckets, 2019, 7); !ok || b.Count != 2 {
		t.Errorf("July 2019 = %+v (found %v), want a count of 2", b, ok)
	}
	if b, ok := find(buckets, 2019, 8); !ok || b.Count != 1 {
		t.Errorf("August 2019 = %+v (found %v), want a count of 1", b, ok)
	}
}

// Newest first, which is the order somebody scrolling a timeline expects.
func TestTheTimelineIsNewestFirst(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)

	old := atLocal(t, 2015, time.March, 2)
	recent := atLocal(t, 2024, time.November, 20)
	sh.photo(t, s, "old.jpg", &old, &sh.folder)
	sh.photo(t, s, "new.jpg", &recent, &sh.folder)

	buckets, _ := s.PhotoTimeline(context.Background(), sh.library)
	if len(buckets) < 2 {
		t.Fatalf("got %d buckets", len(buckets))
	}
	if buckets[0].Year != 2024 {
		t.Errorf("first bucket is %d, want the newest (2024)", buckets[0].Year)
	}
}

/*
 * Undated photographs are a bucket, and they sort last.
 *
 * Measured before this was designed: 207 of 3,676 photographs on the reporting
 * library carry no capture time. Dropping them would lose 5% of a library
 * silently, which is the failure this project keeps rediscovering — the request
 * succeeds and only the picture is short.
 */
func TestUndatedPhotosAreABucketAtTheEnd(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)

	dated := atLocal(t, 2020, time.June, 1)
	sh.photo(t, s, "dated.jpg", &dated, &sh.folder)
	sh.photo(t, s, "nodate.jpg", nil, &sh.folder)

	buckets, _ := s.PhotoTimeline(context.Background(), sh.library)
	last := buckets[len(buckets)-1]
	if !last.Undated {
		t.Fatalf("last bucket = %+v, want the undated one", last)
	}
	if last.Count != 1 {
		t.Errorf("undated count = %d, want 1", last.Count)
	}
	for _, b := range buckets[:len(buckets)-1] {
		if b.Undated {
			t.Error("an undated bucket appeared before a dated one")
		}
	}
}

/*
 * A sensitive folder is not in the timeline at all.
 *
 * Not merely covered: a cover can only be lifted in the library grid or inside
 * the folder (ADR 0051, amended), so these could never be uncovered here — and
 * a row of covered tiles in the middle of a holiday still discloses *when* the
 * marked photographs were taken, which is most of what marking a folder is
 * trying not to say.
 */
func TestTheTimelineExcludesSensitiveFolders(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()

	when := atLocal(t, 2021, time.May, 5)
	sh.photo(t, s, "ordinary.jpg", &when, &sh.folder)

	private, err := s.EnsureDerivedContainer(ctx, sh.library, "gallery",
		`C:\pics\Private`, "Private", "private", nil)
	if err != nil {
		t.Fatal(err)
	}
	sh.photo(t, s, "private.jpg", &when, &private)
	if err := s.SetSensitive(ctx, private, true); err != nil {
		t.Fatal(err)
	}

	buckets, _ := s.PhotoTimeline(ctx, sh.library)
	b, ok := find(buckets, 2021, 5)
	if !ok {
		t.Fatal("May 2021 is missing entirely")
	}
	if b.Count != 1 {
		t.Errorf("May 2021 counts %d, want 1 — a marked folder is in the timeline", b.Count)
	}
}

// And a missing file is not counted, so the timeline matches what can be opened.
func TestTheTimelineSkipsMissingFiles(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)

	when := atLocal(t, 2018, time.February, 2)
	id := sh.photo(t, s, "gone.jpg", &when, &sh.folder)
	if _, err := s.db.Exec(`UPDATE media_item SET missing = 1 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}

	buckets, _ := s.PhotoTimeline(context.Background(), sh.library)
	if _, ok := find(buckets, 2018, 2); ok {
		t.Error("a missing photo was counted in the timeline")
	}
}

/*
 * Opening a bucket returns that month, and agrees with the count above it.
 *
 * The two are computed by different queries, which is exactly how they come to
 * disagree — reported, in this project, as "the month says 40 and shows 43".
 */
func TestOpeningABucketAgreesWithItsCount(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()

	when := atLocal(t, 2022, time.September, 9)
	other := atLocal(t, 2022, time.October, 1)
	sh.photo(t, s, "s1.jpg", &when, &sh.folder)
	sh.photo(t, s, "s2.jpg", &when, &sh.folder)
	sh.photo(t, s, "o1.jpg", &other, &sh.folder)

	private, _ := s.EnsureDerivedContainer(ctx, sh.library, "gallery",
		`C:\pics\P2`, "P2", "p2", nil)
	sh.photo(t, s, "p.jpg", &when, &private)
	if err := s.SetSensitive(ctx, private, true); err != nil {
		t.Fatal(err)
	}

	b, ok := find(mustTimeline(t, s, sh.library), 2022, 9)
	if !ok {
		t.Fatal("September 2022 missing")
	}

	items, total, err := s.ListItems(ctx, ItemFilter{
		LibraryID: sh.library, Kind: "photo", Sort: "taken",
		TakenMonth: "2022-09", ExcludeSensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != b.Count || len(items) != b.Count {
		t.Errorf("bucket says %d, listing returned %d (total %d)", b.Count, len(items), total)
	}
}

func mustTimeline(t *testing.T, s *Store, lib int64) []TimelineBucket {
	t.Helper()
	b, err := s.PhotoTimeline(context.Background(), lib)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
