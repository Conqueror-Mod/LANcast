package store

import (
	"context"
	"testing"
	"time"
)

/*
 * On this day.
 *
 * The interesting cases are all about what does *not* appear, which is why
 * every test here builds a photograph that ought to be excluded and asserts it
 * is. A memories shelf that shows too much is not a slightly worse shelf — it
 * is a covered folder on the home page.
 *
 * Dates are built from the server's local clock rather than from fixed
 * timestamps, because that is what the query uses: a fixture pinned to a UTC
 * instant would drift in and out of the right day depending on where the test
 * ran, which is the same timezone fault the feature exists to keep out of the
 * client.
 */

// takenAt gives the unix time for local noon, yearsAgo years back from today.
// Noon so that a machine several hours either side of UTC still lands on the
// same calendar day.
func takenAt(yearsAgo int) int64 {
	n := time.Now()
	return time.Date(n.Year()-yearsAgo, n.Month(), n.Day(), 12, 0, 0, 0, time.Local).Unix()
}

func (fx faceFixture) datedPhoto(t *testing.T, s *Store, name string, parent int64, taken int64) int64 {
	t.Helper()
	id := fx.photo(t, s, name, parent)
	if _, err := s.db.Exec(`UPDATE media_item SET taken_at = ? WHERE id = ?`, taken, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMemoriesAreTodayInAnEarlierYear(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	lastYear := fx.datedPhoto(t, s, "lastyear.jpg", fx.folder, takenAt(1))
	longAgo := fx.datedPhoto(t, s, "longago.jpg", fx.folder, takenAt(5))
	// A different day, and a photograph with no capture time at all.
	otherDay := fx.datedPhoto(t, s, "other.jpg", fx.folder, takenAt(1)+5*24*60*60)
	undated := fx.photo(t, s, "undated.jpg", fx.folder)

	items, on, err := s.PhotoMemories(ctx, 40)
	if err != nil {
		t.Fatal(err)
	}
	if on != time.Now().Format("01-02") {
		t.Errorf("resolved day = %q, want today in the server's own clock", on)
	}

	got := map[int64]bool{}
	for _, it := range items {
		got[it.ID] = true
	}
	if !got[lastYear] || !got[longAgo] {
		t.Errorf("a photograph from this day in an earlier year is missing: %v", got)
	}
	if got[otherDay] {
		t.Error("a photograph from a different day appeared")
	}
	if got[undated] {
		t.Error("a photograph with no capture time appeared; there is no day it is the anniversary of")
	}
}

/*
 * This morning's photographs are not a memory.
 *
 * And the practical half matters more than the semantic one: somebody who has
 * just imported a card would otherwise have that card fill the shelf and push
 * the actual memories off the end, on the one day they were there to be seen.
 */
func TestMemoriesExcludeTheCurrentYear(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	today := fx.datedPhoto(t, s, "today.jpg", fx.folder, takenAt(0))
	lastYear := fx.datedPhoto(t, s, "lastyear.jpg", fx.folder, takenAt(1))

	items, _, err := s.PhotoMemories(ctx, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == today {
			t.Error("a photograph taken today appeared as a memory")
		}
	}
	if len(items) != 1 || items[0].ID != lastYear {
		t.Errorf("want only last year's photograph, got %d items", len(items))
	}
}

/*
 * A marked folder never reaches the shelf.
 *
 * Stricter than the timeline's version of this rule and deliberately so. The
 * timeline is somewhere you navigated to; a memory is unsolicited and lands on
 * the home page, in front of whoever is in the room. A covered photograph
 * surfacing there is the worst case ADR 0051 has.
 */
func TestMemoriesNeverShowAMarkedFolder(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	open := fx.datedPhoto(t, s, "open.jpg", fx.folder, takenAt(2))
	hidden := fx.datedPhoto(t, s, "hidden.jpg", fx.private, takenAt(2))
	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}

	items, _, err := s.PhotoMemories(ctx, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == hidden {
			t.Error("a photograph in a marked folder appeared on the memories shelf")
		}
	}
	if len(items) != 1 || items[0].ID != open {
		t.Errorf("the unmarked photograph should still be there; got %d items", len(items))
	}
}

// A missing file is not worth a tile: the shelf is for looking at.
func TestMemoriesExcludeMissingFiles(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	gone := fx.datedPhoto(t, s, "gone.jpg", fx.folder, takenAt(3))
	if _, err := s.db.Exec(`UPDATE media_item SET missing = 1 WHERE id = ?`, gone); err != nil {
		t.Fatal(err)
	}

	items, _, err := s.PhotoMemories(ctx, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("a missing photograph appeared on the shelf: %d items", len(items))
	}
}

// Most days there is nothing, and that is an answer rather than a failure.
func TestMemoriesOnAnEmptyDay(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	fx.datedPhoto(t, s, "other.jpg", fx.folder, takenAt(1)+10*24*60*60)

	items, on, err := s.PhotoMemories(ctx, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("want no memories, got %d", len(items))
	}
	if on == "" {
		t.Error("the resolved day is still reported on a day with nothing in it")
	}
}
