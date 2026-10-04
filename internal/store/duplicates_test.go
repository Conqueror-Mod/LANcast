package store

import (
	"context"
	"testing"
)

/*
 * Exact duplicate photos (ADR 0075): the same bytes, grouped, each copy with
 * its album; a changed file loses its digest; a marked photo takes no part.
 */

func hashPhoto(t *testing.T, s *Store, id int64, sha string) {
	t.Helper()
	if err := s.SetPhotoMeta(context.Background(), id, 100, 100, 0, sha); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicatesGroupTheSameBytesWithTheirAlbums(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	other, err := s.EnsureDerivedContainer(ctx, sh.library, "gallery", `C:\pics\Nature`, "Nature", "nature", nil)
	if err != nil {
		t.Fatal(err)
	}
	// One photo filed in two albums, and a loose copy at the root — the
	// shapes the real library had.
	a := sh.photo(t, s, `Holiday\20210227.jpg`, nil, &sh.folder)
	b := sh.photo(t, s, `Nature\20210227.jpg`, nil, &other)
	c := sh.photo(t, s, `20210227.jpg`, nil, nil)
	lone := sh.photo(t, s, `Holiday\only.jpg`, nil, &sh.folder)
	for _, id := range []int64{a, b, c} {
		hashPhoto(t, s, id, "9f2c")
	}
	hashPhoto(t, s, lone, "1111")

	groups, err := s.PhotoDuplicates(ctx, sh.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1 — a photo with no twin is not a duplicate", len(groups))
	}
	g := groups[0]
	if g.SHA256 != "9f2c" || len(g.Copies) != 3 {
		t.Fatalf("group = %+v, want three copies of 9f2c", g)
	}
	albums := map[int64]string{}
	for _, cp := range g.Copies {
		if cp.Album == nil {
			albums[cp.Item.ID] = "(root)"
		} else {
			albums[cp.Item.ID] = *cp.Album
		}
	}
	if albums[a] != "Holiday" || albums[b] != "Nature" || albums[c] != "(root)" {
		t.Errorf("albums = %v, want Holiday, Nature and the root", albums)
	}
}

// The largest group first: three copies of one photo is a bigger tidy-up than
// two of another.
func TestDuplicatesPutTheLargestGroupFirst(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	for i, sha := range []string{"pair", "pair", "trio", "trio", "trio"} {
		hashPhoto(t, s, sh.photo(t, s, sha+string(rune('a'+i))+".jpg", nil, &sh.folder), sha)
	}
	groups, err := s.PhotoDuplicates(ctx, sh.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].SHA256 != "trio" {
		t.Errorf("order = %v, want the three-copy group first", shaOrder(groups))
	}
}

func shaOrder(gs []DuplicateGroup) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.SHA256
	}
	return out
}

/*
 * A marked photo is not listed, even beside an unmarked twin: showing the twin
 * shows the picture the mark exists to obscure. With the marked copy gone, the
 * twin has no partner and is not a duplicate either.
 */
func TestDuplicatesLeaveOutMarkedPhotos(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	open := sh.photo(t, s, `Holiday\a.jpg`, nil, &sh.folder)
	marked := sh.photo(t, s, `b.jpg`, nil, nil)
	hashPhoto(t, s, open, "same")
	hashPhoto(t, s, marked, "same")
	if err := s.SetSensitive(ctx, marked, true); err != nil {
		t.Fatal(err)
	}
	groups, err := s.PhotoDuplicates(ctx, sh.library)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Errorf("got %+v, want nothing — the only twin is marked", groups)
	}
}

func TestDuplicatesLeaveOutMissingPhotos(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	a := sh.photo(t, s, `a.jpg`, nil, nil)
	b := sh.photo(t, s, `b.jpg`, nil, nil)
	hashPhoto(t, s, a, "same")
	hashPhoto(t, s, b, "same")
	if _, err := s.db.Exec(`UPDATE media_item SET missing = 1 WHERE id = ?`, b); err != nil {
		t.Fatal(err)
	}
	if groups, _ := s.PhotoDuplicates(ctx, sh.library); len(groups) != 0 {
		t.Errorf("got %+v, want nothing — a missing file is not a copy of anything", groups)
	}
}

/*
 * An edited photo is read again: its digest goes and its thumbnail is queued,
 * or it would go on being listed as a duplicate of the original it no longer
 * matches.
 */
func TestAChangedPhotoLosesItsDigestAndIsReadAgain(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Pictures", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := ScanFile{LibraryID: lib.ID, Path: lib.Path + "/a.jpg", Kind: "photo",
		Title: "a", SortTitle: "a", Container: "jpg", SizeBytes: 1000, MTime: 1}
	id, err := s.UpsertItem(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	hashPhoto(t, s, id, "before")
	if err := s.MarkArtworkChecked(ctx, id); err != nil {
		t.Fatal(err)
	}

	f.SizeBytes, f.MTime = 2000, 2 // edited
	if _, err := s.UpsertItem(ctx, f); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM photo_hash WHERE item_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("an edited photo kept the digest of the file it replaced")
	}
	pending, err := s.PendingPhotos(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != id {
		t.Errorf("pending = %v, want the edited photo queued to be read again", pending)
	}
}

// A film is not a photo: an upsert of anything else touches neither.
func TestAnUpsertOfAFilmLeavesPhotoStateAlone(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.UpsertItem(ctx, file(lib.ID, lib.Path+"/f.mkv", "F"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkArtworkChecked(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertItem(ctx, file(lib.ID, lib.Path+"/f.mkv", "F")); err != nil {
		t.Fatal(err)
	}
	var checked *int64
	if err := s.db.QueryRow(`SELECT cover_checked_at FROM media_item WHERE id = ?`, id).Scan(&checked); err != nil {
		t.Fatal(err)
	}
	if checked == nil {
		t.Error("a film's artwork stamp was cleared by an upsert meant for photos")
	}
}

// Revision 58 sends every photo back through the worker so existing ones get
// a digest.
func TestRevision58QueuesEveryPhotoForADigest(t *testing.T) {
	s := openTestStore(t)
	sh := makeShots(t, s)
	ctx := context.Background()
	id := sh.photo(t, s, `a.jpg`, nil, nil)
	if err := s.MarkArtworkChecked(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE meta SET value = '57' WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(s.db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pending, err := s.PendingPhotos(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("pending = %d photos, want the one already thumbnailed queued for its digest", len(pending))
	}
}
