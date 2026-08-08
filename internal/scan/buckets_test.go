package scan

import "testing"

func albumOf(t *testing.T, groups []trackGroup, id int64) string {
	t.Helper()
	for _, g := range groups {
		if g.itemID == id {
			return g.album
		}
	}
	t.Fatalf("no group for item %d", id)
	return ""
}

// The case from the real library: a folder of alphabetical buckets, with loose
// singles sitting directly in them. Every bucket became an album named after a
// letter, holding one track by an artist who never made a record called "B's".
func TestALetterBucketIsNotAnAlbum(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Billy the Kit", album: "B's", dir: `/music/B's`, albumFromFolder: true},
		{itemID: 2, artist: "BlueShift", album: "B's", dir: `/music/B's`, albumFromFolder: true},
		{itemID: 3, artist: "Benny Benassi", album: "B's", dir: `/music/B's`, albumFromFolder: true},
	}
	dropBucketAlbums(groups)

	for _, id := range []int64{1, 2, 3} {
		if got := albumOf(t, groups, id); got != "" {
			t.Errorf("item %d kept album %q — a folder holding three artists is not a record", id, got)
		}
	}
}

// The fallback still works where it was right: a folder whose tracks agree on
// the artist looks like a record, so it stays one.
func TestACoherentFolderIsStillAnAlbum(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Aesop Rock", album: "The Impossible Kid", dir: "/music/Aesop Rock/The Impossible Kid", albumFromFolder: true},
		{itemID: 2, artist: "Aesop Rock", album: "The Impossible Kid", dir: "/music/Aesop Rock/The Impossible Kid", albumFromFolder: true},
	}
	dropBucketAlbums(groups)

	for _, id := range []int64{1, 2} {
		if got := albumOf(t, groups, id); got != "The Impossible Kid" {
			t.Errorf("item %d album = %q, want the folder's album kept", id, got)
		}
	}
}

// A tagged album is a statement about the record, not a guess, so a folder full
// of different artists must not strip it. That is the compilation case — one
// directory, one album, many performers — and stripping it would shatter
// exactly the records ADR 0024 groups by album artist to keep whole.
func TestATaggedAlbumSurvivesAMixedFolder(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Various", album: "Now That's What I Call Music", dir: "/music/comp", albumFromFolder: false},
		{itemID: 2, artist: "Someone Else", album: "Now That's What I Call Music", dir: "/music/comp", albumFromFolder: false},
	}
	dropBucketAlbums(groups)

	for _, id := range []int64{1, 2} {
		if got := albumOf(t, groups, id); got == "" {
			t.Errorf("item %d lost its tagged album", id)
		}
	}
}

// Cohesion alone cannot see a bucket holding one loose track — a single track
// never disagrees with itself, which is how an album called "C's" with one song
// in it survived the first version of this rule on the real library. Depth
// catches it: a record does not sit loose at the top of a library.
func TestALoneTrackInARootFolderIsNotAnAlbum(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Cosmic Gate", album: "C's", dir: `/music/C's`,
			albumFromFolder: true, albumAtRoot: true},
	}
	dropBucketAlbums(groups)

	if got := albumOf(t, groups, 1); got != "" {
		t.Errorf("album = %q, want none — a record does not live at the library root", got)
	}
}

// Depth only condemns the root. A single-track folder nested under an artist is
// an album with one track on it, which is a real thing (an EP, a single with a
// folder of its own), so it is left alone.
func TestALoneTrackUnderAnArtistFolderIsStillAnAlbum(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Bloodywood", album: "Rakshak", dir: "/music/Bloodywood/Rakshak",
			albumFromFolder: true, albumAtRoot: false},
	}
	dropBucketAlbums(groups)

	if got := albumOf(t, groups, 1); got != "Rakshak" {
		t.Errorf("album = %q, want it kept", got)
	}
}

// Folders are judged independently: a bucket next door must not strip a real
// record's folder-derived album.
func TestFoldersAreJudgedSeparately(t *testing.T) {
	groups := []trackGroup{
		{itemID: 1, artist: "Billy the Kit", album: "B's", dir: `/music/B's`, albumFromFolder: true},
		{itemID: 2, artist: "BlueShift", album: "B's", dir: `/music/B's`, albumFromFolder: true},
		{itemID: 3, artist: "Beck", album: "Guero", dir: "/music/Beck/Guero", albumFromFolder: true},
		{itemID: 4, artist: "Beck", album: "Guero", dir: "/music/Beck/Guero", albumFromFolder: true},
	}
	dropBucketAlbums(groups)

	if got := albumOf(t, groups, 1); got != "" {
		t.Errorf("bucket track kept album %q", got)
	}
	if got := albumOf(t, groups, 3); got != "Guero" {
		t.Errorf("real album became %q", got)
	}
}
