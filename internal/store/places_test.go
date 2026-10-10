package store

import (
	"context"
	"testing"
)

// Not real places: obviously fake ids and names, so nothing here reads as
// anybody's whereabouts.
var (
	placeA = &PhotoPlace{ID: 9000001, Name: "Alphaville", Region: "Somewhere", CountryCode: "ZZ", Country: "Testland"}
	placeB = &PhotoPlace{ID: 9000002, Name: "Betaburg", Region: "Elsewhere", CountryCode: "ZZ", Country: "Testland"}
	here   = &LatLon{Lat: 10.5, Lon: -20.25}
)

func record(t *testing.T, s *Store, item int64, at *LatLon, p *PhotoPlace) {
	t.Helper()
	if err := s.RecordPhotoLocation(context.Background(), item, at, p); err != nil {
		t.Fatal(err)
	}
}

func TestPlacesCountWhatTheirGridHolds(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	a1 := fx.photo(t, s, "a1.jpg", fx.folder)
	a2 := fx.photo(t, s, "a2.jpg", fx.folder)
	b1 := fx.photo(t, s, "b1.jpg", fx.folder)
	sea := fx.photo(t, s, "sea.jpg", fx.folder)
	none := fx.photo(t, s, "none.jpg", fx.folder)
	_ = fx.photo(t, s, "unread.jpg", fx.folder)

	record(t, s, a1, here, placeA)
	record(t, s, a2, here, placeA)
	record(t, s, b1, here, placeB)
	record(t, s, sea, here, nil)
	record(t, s, none, nil, nil)

	sum, err := s.PhotoPlaces(ctx, fx.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Places) != 2 || sum.Places[0].Name != "Alphaville" || sum.Places[0].Count != 2 ||
		sum.Places[1].Name != "Betaburg" || sum.Places[1].Count != 1 {
		t.Fatalf("places = %+v, want Alphaville 2 then Betaburg 1, most photographed first", sum.Places)
	}
	if sum.Elsewhere != 1 || sum.Unlocated != 1 || sum.Unread != 1 {
		t.Errorf("elsewhere %d unlocated %d unread %d, want 1 each", sum.Elsewhere, sum.Unlocated, sum.Unread)
	}

	// Every bucket opens onto exactly its count, asked of the other query
	// rather than hardcoded, so the two cannot drift and keep agreeing.
	for _, p := range sum.Places {
		items, total, err := s.PlacePhotos(ctx, fx.library, p.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if total != p.Count || len(items) != p.Count {
			t.Errorf("%s: count %d, grid total %d holding %d", p.Name, p.Count, total, len(items))
		}
	}
	items, total, err := s.PlacePhotos(ctx, fx.library, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != sea {
		t.Errorf("elsewhere opened onto %d (%d), want only the photo with no town near it", total, len(items))
	}
}

/*
 * A marked folder's locations are deleted when it is marked, and the pass
 * never reads one. Both, because the view's exclusion alone would leave the
 * coordinates in the database and in every backup taken afterwards.
 */
func TestMarkingAFolderForgetsWhereItsPhotosWereTaken(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	open := fx.photo(t, s, "open.jpg", fx.folder)
	hidden := fx.photo(t, s, "hidden.jpg", fx.private)
	record(t, s, open, here, placeA)
	record(t, s, hidden, here, placeA)

	if err := s.SetSensitive(ctx, fx.private, true); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM photo_location WHERE item_id = ?`, hidden).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("a marked photo's location survived the mark")
	}
	sum, err := s.PhotoPlaces(ctx, fx.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Places) != 1 || sum.Places[0].Count != 1 || sum.Unread != 0 {
		t.Errorf("after marking: %+v, want Alphaville holding only the open photo and nothing unread", sum)
	}
	pending, err := s.PendingLocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range pending {
		if it.ID == hidden {
			t.Error("the location pass was handed a photo in a marked folder")
		}
	}
}

// The row is the stamp: a photo read and found to say nothing is not read
// again, and one never read is.
func TestTheQueueIsPhotosNotYetRead(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	read := fx.photo(t, s, "read.jpg", fx.folder)
	unread := fx.photo(t, s, "unread.jpg", fx.folder)
	record(t, s, read, nil, nil)

	pending, err := s.PendingLocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != unread {
		t.Errorf("pending = %v, want only the unread photo", ids(pending))
	}
	if n, err := s.PendingLocationCount(ctx); err != nil || n != 1 {
		t.Errorf("count = %d, %v; want 1", n, err)
	}
}

// Turning the setting off deletes, rather than hides, every location and
// place: off means what ADR 0028 meant.
func TestForgettingDeletesEverything(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	record(t, s, fx.photo(t, s, "a.jpg", fx.folder), here, placeA)
	record(t, s, fx.photo(t, s, "b.jpg", fx.folder), nil, nil)

	n, err := s.ForgetPhotoLocations(ctx)
	if err != nil || n != 2 {
		t.Fatalf("forgot %d, %v; want 2", n, err)
	}
	var rows, places int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM photo_location`).Scan(&rows)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM photo_place`).Scan(&places)
	if rows != 0 || places != 0 {
		t.Errorf("left %d locations and %d places behind", rows, places)
	}
}

// Where a photo was taken is a fact about its bytes. An edited photo may have
// lost or gained its location, so a rescan that finds it changed sends it
// back to be read.
func TestAChangedPhotoIsReadAgain(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := ScanFile{LibraryID: lib.ID, Path: lib.Path + "/p.jpg", Kind: "photo",
		Title: "p", SortTitle: "p", Container: "jpg", SizeBytes: 1, MTime: 1}
	id, err := s.UpsertItem(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	record(t, s, id, here, placeA)

	f.MTime, f.SizeBytes = 2, 2
	if _, err := s.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingLocations(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Errorf("pending = %v, want the edited photo back in the queue", ids(pending))
	}
}

// A newer export can rename a town; the place row follows the latest read
// rather than keeping the first name it was given.
func TestAPlaceTakesItsLatestName(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	ctx := context.Background()

	record(t, s, fx.photo(t, s, "a.jpg", fx.folder), here, placeA)
	renamed := *placeA
	renamed.Name = "Alphaville Heights"
	record(t, s, fx.photo(t, s, "b.jpg", fx.folder), here, &renamed)

	sum, err := s.PhotoPlaces(ctx, fx.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Places) != 1 || sum.Places[0].Name != "Alphaville Heights" || sum.Places[0].Count != 2 {
		t.Errorf("places = %+v, want one place under its new name holding both", sum.Places)
	}
}

func TestRevision67AddsTheTablesAndQueuesNothing(t *testing.T) {
	s := openTestStore(t)
	fx := makeFaceLibrary(t, s)
	fx.photo(t, s, "a.jpg", fx.folder)

	if _, err := s.db.Exec(`DROP TABLE photo_location; DROP TABLE photo_place;
		UPDATE meta SET value = '66' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM photo_location`).Scan(&n); err != nil {
		t.Fatalf("photo_location after migrating: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM photo_place`).Scan(&n); err != nil {
		t.Fatalf("photo_place after migrating: %v", err)
	}
	if n != 0 {
		t.Errorf("the migration wrote %d places; it must read nothing", n)
	}
}
