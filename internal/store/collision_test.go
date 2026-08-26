package store

import (
	"context"
	"testing"
)

/*
 * The collision report (ADR 0042).
 *
 * These are built from the five situations found in the real library that
 * motivated the decision, because the point of the report is that a shared
 * provider id means five different things and the server must not guess which.
 */

func seedCollisionItem(t *testing.T, s *Store, libID int64, path, title, provider, extID string,
	size int64, kind string, edition string) int64 {
	t.Helper()
	var ed any
	if edition != "" {
		ed = edition
	}
	var sz any
	if size > 0 {
		sz = size
	}
	res, err := s.db.ExecContext(context.Background(), `
		INSERT INTO media_item (library_id, kind, path, title, sort_title,
		                        provider, external_id, size_bytes, edition,
		                        added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)`,
		libID, kind, path, title, title, provider, extID, sz, ed)
	if err != nil {
		t.Fatalf("seed %s: %v", path, err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedCollisionLibrary(t *testing.T, s *Store) int64 {
	t.Helper()
	res, err := s.db.ExecContext(context.Background(),
		`INSERT INTO library (name, kind, created_at) VALUES ('Films','movie',0)`)
	if err != nil {
		t.Fatalf("seed library: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestCollisionsReportsTwoFilesClaimingOneWork(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)

	seedCollisionItem(t, s, lib, "/m/Spider-Verse (2018).mkv", "Spider-Verse", "tmdb", "324857", 2832374353, "movie", "")
	seedCollisionItem(t, s, lib, "/m/Spider-Verse (Alternate Cut) (2018).mkv", "Spider-Verse", "tmdb", "324857", 2832374353, "movie", "Alternate Cut")
	// A film nothing collides with must not appear.
	seedCollisionItem(t, s, lib, "/m/Fight Club (1999).mkv", "Fight Club", "tmdb", "550", 900, "movie", "")

	got, err := s.Collisions(context.Background(), 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(got), got)
	}
	if len(got[0].Members) != 2 {
		t.Fatalf("got %d members, want 2", len(got[0].Members))
	}
	if !got[0].SameSize {
		t.Error("identical byte counts did not report as the same size")
	}
	// The marker is carried so a person can see what the file claimed -- and it
	// is a label: this pair was byte-for-byte identical.
	var sawEdition bool
	for _, m := range got[0].Members {
		if m.Edition != nil && *m.Edition == "Alternate Cut" {
			sawEdition = true
		}
		if m.Path == "" {
			t.Error("a member came back with no path, which is the whole report")
		}
	}
	if !sawEdition {
		t.Error("the edition marker was not carried into the report")
	}
}

/*
 * Different sizes are the more useful answer, and the one that needs no I/O:
 * equal sizes make a copy likely, unequal sizes rule one out outright. The
 * real library had a 787 MB mp4 beside a 14.6 GB mkv of the same cut.
 */
func TestCollisionsDistinguishesADifferentEncode(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	seedCollisionItem(t, s, lib, "/m/a.mp4", "A Film", "tmdb", "99", 787_000_000, "movie", "")
	seedCollisionItem(t, s, lib, "/m/a.mkv", "A Film", "tmdb", "99", 14_600_000_000, "movie", "")

	got, err := s.Collisions(context.Background(), 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1", len(got))
	}
	if got[0].SameSize {
		t.Error("different byte counts reported as the same size")
	}
	// Largest first, so the biggest copy is the one a reader sees first.
	if *got[0].Members[0].SizeBytes < *got[0].Members[1].SizeBytes {
		t.Error("members are not ordered by size")
	}
}

/*
 * A show and its seasons legitimately share a provider id. Reporting that
 * would bury the real collisions under every show in the library.
 */
func TestCollisionsIgnoresTheHierarchy(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	seedCollisionItem(t, s, lib, "/tv/Show", "A Show", "tmdb", "1234", 0, "show", "")
	seedCollisionItem(t, s, lib, "/tv/Show/S1", "Season 1", "tmdb", "1234", 0, "season", "")
	seedCollisionItem(t, s, lib, "/tv/Show/S2", "Season 2", "tmdb", "1234", 0, "season", "")

	got, err := s.Collisions(context.Background(), 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the hierarchy reported as %d collisions: %+v", len(got), got)
	}
}

// An unmatched file claims no work, so it cannot collide with one.
func TestCollisionsIgnoresUnmatchedFiles(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	ctx := context.Background()
	for _, p := range []string{"/m/x.mkv", "/m/y.mkv"} {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO media_item (library_id, kind, path, title, sort_title,
			                        added_at, updated_at)
			VALUES (?, 'movie', ?, 'X', 'x', 0, 0)`, lib, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	got, err := s.Collisions(ctx, 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unmatched files reported as %d collisions", len(got))
	}
}

/*
 * An unknown size is not a match. A row never probed, or whose file has gone,
 * cannot support the claim "these are the same size" -- and a report that
 * invents evidence is worse than one that omits it.
 */
func TestSameSizeRefusesAnUnknownSize(t *testing.T) {
	n := int64(10)
	if sameSize([]CollisionMember{{SizeBytes: &n}, {SizeBytes: nil}}) {
		t.Error("an unknown size counted as equal")
	}
	if sameSize([]CollisionMember{{SizeBytes: nil}, {SizeBytes: nil}}) {
		t.Error("two unknown sizes counted as equal")
	}
	if !sameSize([]CollisionMember{{SizeBytes: &n}, {SizeBytes: &n}}) {
		t.Error("two equal sizes did not count as equal")
	}
}

/*
 * The key that nearly shipped wrong.
 *
 * **Every episode of a show carries the show's external_id** — that is how an
 * episode's provider identity works, the show id plus a position. Keyed on
 * (provider, external_id) alone, every multi-episode show in the library reads
 * as one enormous collision: on the real library that was 999 episode rows
 * against 86 genuine film ones, a report 92% noise, burying the thing it exists
 * to surface.
 *
 * Caught by running the query against the live database before shipping it,
 * not by any test — which is why these two exist now.
 */
func TestEpisodesOfOneShowAreNotACollision(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	ctx := context.Background()

	// Six episodes of one show, all carrying the show's id, as the enrichment
	// pass really stores them.
	for i := 1; i <= 6; i++ {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO media_item (library_id, kind, path, title, sort_title,
			                        provider, external_id, season, episode,
			                        size_bytes, added_at, updated_at)
			VALUES (?, 'episode', ?, ?, 'black books', 'tmdb', '903', 1, ?, ?, 0, 0)`,
			lib, "/tv/s01e0"+string(rune('0'+i))+".mkv", "Episode", i, 100+i); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	got, err := s.Collisions(ctx, 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a show's episodes reported as %d collisions: %+v", len(got), got)
	}
}

// And the condition that *is* real, and was undetectable under the old key:
// two files of the same episode.
func TestTwoFilesOfOneEpisodeAreACollision(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	ctx := context.Background()

	for _, p := range []string{"/tv/s01e01.mkv", "/tv/s01e01 (720p).mkv"} {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO media_item (library_id, kind, path, title, sort_title,
			                        provider, external_id, season, episode,
			                        size_bytes, added_at, updated_at)
			VALUES (?, 'episode', ?, 'Cooking the Books', 'black books',
			        'tmdb', '903', 1, 1, 500, 0, 0)`, lib, p); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// A different episode of the same show must not join them.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO media_item (library_id, kind, path, title, sort_title,
		                        provider, external_id, season, episode,
		                        size_bytes, added_at, updated_at)
		VALUES (?, 'episode', '/tv/s01e02.mkv', 'Manny', 'black books',
		        'tmdb', '903', 1, 2, 500, 0, 0)`, lib); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := s.Collisions(ctx, 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d collisions, want 1: %+v", len(got), got)
	}
	if len(got[0].Members) != 2 {
		t.Errorf("got %d members, want the two files of S01E01", len(got[0].Members))
	}
	if got[0].Season == nil || *got[0].Season != 1 ||
		got[0].Episode == nil || *got[0].Episode != 1 {
		t.Errorf("collision did not carry its position: %+v", got[0])
	}
}

/*
 * A film has no season or episode, and NULL never equals NULL — so without
 * COALESCE every film fails its own IN test and the report comes back empty.
 * The one that would have looked like "no duplicates" rather than an error.
 */
func TestFilmsStillCollideWithNoPosition(t *testing.T) {
	s := openTestStore(t)
	lib := seedCollisionLibrary(t, s)
	seedCollisionItem(t, s, lib, "/m/a.mkv", "A Film", "tmdb", "9480", 800, "movie", "")
	seedCollisionItem(t, s, lib, "/m/b.mkv", "A Film", "tmdb", "9480", 800, "movie", "DC")

	got, err := s.Collisions(context.Background(), 0)
	if err != nil {
		t.Fatalf("collisions: %v", err)
	}
	if len(got) != 1 || len(got[0].Members) != 2 {
		t.Fatalf("films with no position did not collide: %+v", got)
	}
	if got[0].Season != nil || got[0].Episode != nil {
		t.Errorf("a film reported a position: %+v", got[0])
	}
}

/*
 * Collection organisation rules.
 *
 * Two rules, both read-time, both derived from what a real library actually
 * held: 277 collections of which 102 held exactly one film, and 699
 * memberships not one of which carried a non-zero `ord`.
 */

func seedCollection(t *testing.T, s *Store, lib int64, title string) int64 {
	t.Helper()
	id, err := s.UpsertItem(context.Background(), ScanFile{
		LibraryID: lib, Path: "/c/" + title, Kind: "collection",
		Title: title, SortTitle: title, MTime: 1,
	})
	if err != nil {
		t.Fatalf("seed collection: %v", err)
	}
	return id
}

func addMember(t *testing.T, s *Store, item, collection int64) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO item_collection (item_id, collection_id, ord) VALUES (?, ?, 0)`,
		item, collection); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func seedFilm(t *testing.T, s *Store, lib int64, path, title string, year int) int64 {
	t.Helper()
	f := ScanFile{
		LibraryID: lib, Path: path, Kind: "movie",
		Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
	}
	if year > 0 {
		f.Year = &year
	}
	id, err := s.UpsertItem(context.Background(), f)
	if err != nil {
		t.Fatalf("seed film: %v", err)
	}
	return id
}

/*
 * Release order, not alphabetical.
 *
 * The case from the real library: "The Final Destination" (2009) is the fourth
 * film and sorts under T, so alphabetically it landed after "Final Destination
 * 5" (2011) — a franchise reading as a list rather than a sequence.
 */
func TestCollectionMembersAreInReleaseOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "Final Destination Collection")

	for _, f := range []struct {
		title string
		year  int
	}{
		{"Final Destination 5", 2011},
		{"The Final Destination", 2009},
		{"Final Destination", 2000},
		{"Final Destination 2", 2003},
	} {
		addMember(t, s, seedFilm(t, s, lib, "/m/"+f.title+".mkv", f.title, f.year), col)
	}

	got, err := s.CollectionMembers(ctx, col)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	want := []string{
		"Final Destination", "Final Destination 2",
		"The Final Destination", "Final Destination 5",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d members, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Title != w {
			t.Errorf("position %d = %q, want %q", i, got[i].Title, w)
		}
	}
}

// A film with no year sorts last. NULL leads in SQLite, and an unmatched row
// heading a franchise is worse than it trailing one.
func TestAnUndatedFilmTrailsItsCollection(t *testing.T) {
	s := openTestStore(t)
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")
	addMember(t, s, seedFilm(t, s, lib, "/m/unknown.mkv", "Unknown", 0), col)
	addMember(t, s, seedFilm(t, s, lib, "/m/first.mkv", "First", 1990), col)

	got, err := s.CollectionMembers(context.Background(), col)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(got) != 2 || got[0].Title != "First" {
		t.Errorf("order = %v, want the dated film first", titles(got))
	}
}

/*
 * A film whose file is gone is not in the collection.
 *
 * Reported from a real library as duplication in the Halloween Collection --
 * seventeen tiles for nine films, several titles appearing twice and one three
 * times. Not duplicates: every file had been renamed from
 * "Halloween.1978.1080p.Bluray.AC3.x264.mkv" into
 * "Halloween (1978)/Halloween (1978).mkv", so the scanner marked the old rows
 * missing -- as it must, since an unmounted drive is not permission to destroy
 * a library -- and they kept their memberships.
 *
 * Every neighbouring query already filtered: the child count counts present
 * films, collectionIsReal decides a collection exists on present films. This
 * listing was the one that did not, so the page disagreed with its own header.
 */
func TestCollectionOmitsFilmsWhoseFilesAreGone(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "Halloween Collection")

	present := seedFilm(t, s, lib, "/m/Halloween (1978)/Halloween (1978).mkv", "Halloween", 1978)
	addMember(t, s, present, col)

	// The same film under its old name, renamed away twice.
	for _, old := range []string{
		"/m/Halloween.1978.1080p.Bluray.AC3.x264.mkv",
		"/m/Halloween 1978/Halloween.mkv",
	} {
		gone := seedFilm(t, s, lib, old, "Halloween", 1978)
		addMember(t, s, gone, col)
		if _, err := s.db.ExecContext(ctx,
			`UPDATE media_item SET missing = 1 WHERE id = ?`, gone); err != nil {
			t.Fatalf("mark missing: %v", err)
		}
	}

	got, err := s.CollectionMembers(ctx, col)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d members, want 1: %v", len(got), titles(got))
	}
	if got[0].ID != present {
		t.Errorf("listed id %d, want the present file %d", got[0].ID, present)
	}

	/*
	 * And the rows are still there. Nothing was deleted -- a drive coming back
	 * has to put the films back, which is the whole reason scanning marks
	 * missing rather than removing.
	 */
	var rows int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_collection WHERE collection_id = ?`, col).Scan(&rows); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if rows != 3 {
		t.Errorf("%d membership rows survive, want 3 — the listing must hide, not delete", rows)
	}
}

// The listing and the header have to agree, which is what the report was
// actually about: "9 films" over a grid of 17.
func TestCollectionMemberCountAgreesWithTheListing(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")

	for i, p := range []string{"/m/one.mkv", "/m/two.mkv"} {
		addMember(t, s, seedFilm(t, s, lib, p, "Film", 2000+i), col)
	}
	gone := seedFilm(t, s, lib, "/m/old.mkv", "Film", 2000)
	addMember(t, s, gone, col)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET missing = 1 WHERE id = ?`, gone); err != nil {
		t.Fatalf("mark missing: %v", err)
	}

	members, err := s.CollectionMembers(ctx, col)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	items := []Item{{ID: col, Kind: "collection"}}
	if err := s.AttachChildCounts(ctx, items); err != nil {
		t.Fatalf("counts: %v", err)
	}
	if items[0].ChildCount != len(members) {
		t.Errorf("header says %d, listing shows %d", items[0].ChildCount, len(members))
	}
}

/*
 * A collection with no image of its own wears its first film's poster.
 *
 * Smart collections have nothing to fetch: they are defined by a keyword, and
 * keyword 180547 has no image behind it. The Marvel Cinematic Universe tile
 * rendered as its own title on an empty rectangle, in a grid of 176 collections
 * that all have art -- which reads as broken however honest the reason.
 */
func seedPoster(t *testing.T, s *Store, itemID int64, hash string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO artwork (hash, kind, bytes, width, height, created_at)
		 VALUES (?, 'poster', 1, 1, 1, 0)`, hash); err != nil {
		t.Fatalf("seed artwork: %v", err)
	}
	var artID int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT id FROM artwork WHERE hash = ?`, hash).Scan(&artID); err != nil {
		t.Fatalf("read artwork: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO item_artwork (item_id, artwork_id, kind, selected)
		 VALUES (?, ?, 'poster', 1)`, itemID, artID); err != nil {
		t.Fatalf("link artwork: %v", err)
	}
}

func TestACollectionBorrowsItsFirstFilmsPoster(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "Marvel Cinematic Universe")

	// Deliberately seeded out of order, so passing cannot be an accident of
	// insertion: the rule is the earliest release, not the first row.
	endgame := seedFilm(t, s, lib, "/m/endgame.mkv", "Avengers: Endgame", 2019)
	ironman := seedFilm(t, s, lib, "/m/ironman.mkv", "Iron Man", 2008)
	seedPoster(t, s, endgame, "hash-endgame")
	seedPoster(t, s, ironman, "hash-ironman")
	addMember(t, s, endgame, col)
	addMember(t, s, ironman, col)

	items := []Item{{ID: col, Kind: "collection"}}
	if err := s.inheritCollectionPosters(ctx, items); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if items[0].Artwork == nil || items[0].Artwork.Poster != "hash-ironman" {
		t.Fatalf("poster = %+v, want the earliest film's", items[0].Artwork)
	}
	// Flagged, so a client can tell a borrowed image from an owned one and a
	// real poster arriving later supersedes it with nothing to clean up.
	if !items[0].Artwork.Inherited {
		t.Error("a borrowed poster was not flagged inherited")
	}
}

// A collection that has its own poster keeps it. Every TMDB franchise has one,
// so this is the overwhelmingly common case and must not be overwritten.
func TestACollectionWithItsOwnPosterKeepsIt(t *testing.T) {
	s := openTestStore(t)
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "Halloween Collection")
	film := seedFilm(t, s, lib, "/m/h.mkv", "Halloween", 1978)
	seedPoster(t, s, film, "hash-film")
	addMember(t, s, film, col)

	items := []Item{{ID: col, Kind: "collection",
		Artwork: &Artwork{Poster: "hash-own"}}}
	if err := s.inheritCollectionPosters(context.Background(), items); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if items[0].Artwork.Poster != "hash-own" {
		t.Errorf("poster = %q, want the collection's own", items[0].Artwork.Poster)
	}
	if items[0].Artwork.Inherited {
		t.Error("an owned poster was flagged inherited")
	}
}

// A franchise must not wear the poster of a file that is gone -- the same row
// the collection listing has just stopped showing.
func TestACollectionDoesNotBorrowFromAMissingFilm(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")

	gone := seedFilm(t, s, lib, "/m/old.mkv", "First", 1990)
	seedPoster(t, s, gone, "hash-gone")
	addMember(t, s, gone, col)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE media_item SET missing = 1 WHERE id = ?`, gone); err != nil {
		t.Fatalf("mark missing: %v", err)
	}
	present := seedFilm(t, s, lib, "/m/new.mkv", "Second", 1995)
	seedPoster(t, s, present, "hash-present")
	addMember(t, s, present, col)

	items := []Item{{ID: col, Kind: "collection"}}
	if err := s.inheritCollectionPosters(ctx, items); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if items[0].Artwork == nil || items[0].Artwork.Poster != "hash-present" {
		t.Errorf("poster = %+v, want the present film's", items[0].Artwork)
	}
}

/*
 * Choosing which of its films a collection wears.
 *
 * The inherited default -- earliest release -- is right for almost every
 * franchise and wrong for some: the MCU wearing Iron Man (2008) is defensible
 * and is not what somebody who has looked at it wants.
 */
func TestChoosingACollectionPosterOverridesTheDefault(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "Marvel Cinematic Universe")

	first := seedFilm(t, s, lib, "/m/ironman.mkv", "Iron Man", 2008)
	chosen := seedFilm(t, s, lib, "/m/endgame.mkv", "Avengers: Endgame", 2019)
	seedPoster(t, s, first, "hash-ironman")
	seedPoster(t, s, chosen, "hash-endgame")
	addMember(t, s, first, col)
	addMember(t, s, chosen, col)

	// Before: the default.
	items := []Item{{ID: col, Kind: "collection"}}
	if err := s.inheritCollectionPosters(ctx, items); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if items[0].Artwork.Poster != "hash-ironman" {
		t.Fatalf("default poster = %q, want the earliest film's", items[0].Artwork.Poster)
	}

	if err := s.SetCollectionPoster(ctx, col, chosen); err != nil {
		t.Fatalf("SetCollectionPoster: %v", err)
	}

	art, err := s.ItemArtwork(ctx, col)
	if err != nil {
		t.Fatalf("ItemArtwork: %v", err)
	}
	if art == nil || art.Poster != "hash-endgame" {
		t.Fatalf("poster = %+v, want the chosen film's", art)
	}
	// Owned now, not borrowed: the inherit pass must leave it alone.
	after := []Item{{ID: col, Kind: "collection", Artwork: art}}
	if err := s.inheritCollectionPosters(ctx, after); err != nil {
		t.Fatalf("inherit after choosing: %v", err)
	}
	if after[0].Artwork.Poster != "hash-endgame" {
		t.Errorf("the default overwrote a choice: %+v", after[0].Artwork)
	}

	/*
	 * And it locks. Without one the next artwork write replaces the choice --
	 * PutArtwork deselects every poster row before selecting its own -- and a
	 * choice a refresh can undo is not a choice (ADR 0008).
	 */
	locked, err := s.LockedFields(ctx, col)
	if err != nil {
		t.Fatalf("LockedFields: %v", err)
	}
	var hasArtwork bool
	for _, f := range locked {
		if f == "artwork" {
			hasArtwork = true
		}
	}
	if !hasArtwork {
		t.Errorf("locked fields = %v, want artwork locked", locked)
	}
}

// The undo half. An override somebody cannot take back is a trap, and the
// default is a rule that improves.
func TestClearingACollectionPosterReturnsToTheDefault(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")

	first := seedFilm(t, s, lib, "/m/one.mkv", "First", 1990)
	other := seedFilm(t, s, lib, "/m/two.mkv", "Second", 1995)
	seedPoster(t, s, first, "hash-first")
	seedPoster(t, s, other, "hash-second")
	addMember(t, s, first, col)
	addMember(t, s, other, col)

	if err := s.SetCollectionPoster(ctx, col, other); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.ClearCollectionPoster(ctx, col); err != nil {
		t.Fatalf("clear: %v", err)
	}

	art, _ := s.ItemArtwork(ctx, col)
	if art != nil && art.Poster != "" {
		t.Errorf("a cleared collection still owns %q", art.Poster)
	}
	items := []Item{{ID: col, Kind: "collection"}}
	if err := s.inheritCollectionPosters(ctx, items); err != nil {
		t.Fatalf("inherit: %v", err)
	}
	if items[0].Artwork == nil || items[0].Artwork.Poster != "hash-first" {
		t.Errorf("poster = %+v, want the default back", items[0].Artwork)
	}
	locked, _ := s.LockedFields(ctx, col)
	for _, f := range locked {
		if f == "artwork" {
			t.Error("clearing left the artwork lock behind")
		}
	}
}

/*
 * A film that is not in the collection is refused. The id arrives from a
 * client, and this is the boundary where a bad one would become "any item's
 * poster on any collection".
 */
func TestACollectionRefusesAPosterFromANonMember(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")
	member := seedFilm(t, s, lib, "/m/in.mkv", "In", 1990)
	seedPoster(t, s, member, "hash-in")
	addMember(t, s, member, col)

	stranger := seedFilm(t, s, lib, "/m/out.mkv", "Out", 1991)
	seedPoster(t, s, stranger, "hash-out")

	if err := s.SetCollectionPoster(ctx, col, stranger); err == nil {
		t.Fatal("a non-member's poster was accepted")
	}
	art, _ := s.ItemArtwork(ctx, col)
	if art != nil && art.Poster != "" {
		t.Errorf("a refused request still wrote %q", art.Poster)
	}
}

// A member with no poster of its own has nothing to lend.
func TestACollectionRefusesAMemberWithNoPoster(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lib := mustLibrary(t, s).ID
	col := seedCollection(t, s, lib, "A Franchise")
	bare := seedFilm(t, s, lib, "/m/bare.mkv", "Bare", 1990)
	addMember(t, s, bare, col)

	if err := s.SetCollectionPoster(ctx, col, bare); err == nil {
		t.Error("a member with no poster was accepted")
	}
}
