package coverart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- sidecar selection ----------------------------------------------------

func TestPicksCoverOverOtherNames(t *testing.T) {
	got := PickSidecar([]string{"back.jpg", "cover.jpg", "disc.jpg"})
	if got != "cover.jpg" {
		t.Errorf("PickSidecar = %q, want cover.jpg", got)
	}
}

// Ranking is the whole point of the list. An album directory holding both the
// front and the back of the sleeve must not get the back.
func TestSidecarRankingIsRespected(t *testing.T) {
	tests := []struct {
		names []string
		want  string
	}{
		{[]string{"folder.jpg", "cover.jpg"}, "cover.jpg"},
		{[]string{"front.jpg", "folder.jpg"}, "folder.jpg"},
		{[]string{"albumart.jpg", "front.jpg"}, "front.jpg"},
		{[]string{"albumartsmall.jpg", "albumart.jpg"}, "albumart.jpg"},
		// Extension rank only breaks ties within one name, never across names:
		// a cover.png beats a folder.jpg because the name ranks first.
		{[]string{"folder.jpg", "cover.png"}, "cover.png"},
		{[]string{"cover.png", "cover.jpg"}, "cover.jpg"},
	}
	for _, tc := range tests {
		if got := PickSidecar(tc.names); got != tc.want {
			t.Errorf("PickSidecar(%v) = %q, want %q", tc.names, got, tc.want)
		}
	}
}

// Windows and Linux disagree about whether Cover.jpg and cover.jpg are the same
// file, and a real music library is routinely written by both.
func TestSidecarMatchIsCaseInsensitiveButReturnsTheRealName(t *testing.T) {
	got := PickSidecar([]string{"COVER.JPG"})
	if got != "COVER.JPG" {
		t.Errorf("PickSidecar = %q, want the on-disk spelling COVER.JPG", got)
	}
	if got := PickSidecar([]string{"Folder.Png"}); got != "Folder.Png" {
		t.Errorf("PickSidecar = %q, want Folder.Png", got)
	}
}

func TestNoSidecarWhenNothingQualifies(t *testing.T) {
	got := PickSidecar([]string{"track01.mp3", "notes.txt", "back.jpg", "scan.tiff"})
	if got != "" {
		t.Errorf("PickSidecar = %q, want empty", got)
	}
}

func TestNoSidecarFromAnEmptyDirectory(t *testing.T) {
	if got := PickSidecar(nil); got != "" {
		t.Errorf("PickSidecar(nil) = %q, want empty", got)
	}
}

// A directory listing arrives in whatever order the filesystem gave it. Two
// runs over the same album must not disagree about its cover.
func TestSidecarChoiceIsStableAcrossListingOrder(t *testing.T) {
	a := PickSidecar([]string{"cover.jpg", "folder.jpg", "front.jpg"})
	b := PickSidecar([]string{"front.jpg", "folder.jpg", "cover.jpg"})
	if a != b {
		t.Errorf("listing order changed the choice: %q vs %q", a, b)
	}
}

// WebP would decode with the x/image module this project already depends on,
// and is deliberately excluded: the artwork cache cannot store it, and finding
// a cover the cache then rejects reads as an album that mysteriously has none.
func TestUnsupportedFormatsAreNotChosen(t *testing.T) {
	if got := PickSidecar([]string{"cover.webp", "cover.gif", "cover.bmp"}); got != "" {
		t.Errorf("PickSidecar = %q, want empty — the cache cannot store those", got)
	}
}

// --- directory search order -----------------------------------------------

// A multi-disc rip splits an album across subdirectories, so the album's own
// directory is the parent of a disc folder. Both are searched, nearest first.
func TestSidecarDirsPrefersTheTrackDirectoryOverItsParent(t *testing.T) {
	paths := []string{
		filepath.Join("Music", "Album", "Disc 1", "01.flac"),
		filepath.Join("Music", "Album", "Disc 2", "01.flac"),
	}
	dirs := sidecarDirs(paths)
	if len(dirs) < 3 {
		t.Fatalf("sidecarDirs = %v, want both disc folders and the album folder", dirs)
	}
	if !strings.HasSuffix(dirs[0], "Disc 1") {
		t.Errorf("first dir = %q, want the nearest track directory", dirs[0])
	}
	last := dirs[len(dirs)-1]
	if !strings.HasSuffix(last, "Album") {
		t.Errorf("last dir = %q, want the shared parent searched last", last)
	}
}

func TestSidecarDirsDeduplicates(t *testing.T) {
	paths := []string{
		filepath.Join("Music", "Album", "01.flac"),
		filepath.Join("Music", "Album", "02.flac"),
		filepath.Join("Music", "Album", "03.flac"),
	}
	dirs := sidecarDirs(paths)
	seen := map[string]bool{}
	for _, d := range dirs {
		if seen[d] {
			t.Errorf("sidecarDirs repeated %q: %v", d, dirs)
		}
		seen[d] = true
	}
}

// --- resolution -----------------------------------------------------------

// onePixelPNG is the smallest thing that is genuinely a decodable image, so the
// decodable() guard is exercised against real bytes rather than a stub.
var onePixelPNG = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
	0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
	0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

// A resolver with no ffmpeg still finds sidecar covers — a server without media
// tools installed is not a server without album art.
func TestSidecarIsFoundWithoutFFmpeg(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "01.flac")
	if err := os.WriteFile(track, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	// An extractor pointed at a binary that does not exist is unavailable, which
	// is the no-ffmpeg case without depending on the test machine's PATH.
	r := NewResolver(&Extractor{Path: filepath.Join(dir, "no-such-ffmpeg")})
	img, err := r.ForAlbum(context.Background(), []string{track})
	if err != nil {
		t.Fatalf("ForAlbum: %v", err)
	}
	if img.Source != SourceSidecar {
		t.Errorf("Source = %q, want sidecar", img.Source)
	}
	if len(img.Bytes) != len(onePixelPNG) {
		t.Errorf("got %d bytes, want the cover file's %d", len(img.Bytes), len(onePixelPNG))
	}
}

// An album with nothing to find is the ordinary case, and must be distinguishable
// from a failure — the worker stamps both but counts them differently.
func TestNoArtIsItsOwnError(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "01.flac")
	if err := os.WriteFile(track, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(dir, "no-such-ffmpeg")})
	_, err := r.ForAlbum(context.Background(), []string{track})
	if !errors.Is(err, ErrNoArt) {
		t.Errorf("err = %v, want ErrNoArt", err)
	}
}

func TestAnAlbumWithNoTracksFindsNothing(t *testing.T) {
	r := NewResolver(NewExtractor())
	if _, err := r.ForAlbum(context.Background(), nil); !errors.Is(err, ErrNoArt) {
		t.Errorf("err = %v, want ErrNoArt", err)
	}
}

// Bytes that are not an image must never reach the content-addressed cache: it
// would store them once, keyed by hash, and then fail to render them forever.
func TestUndecodableSidecarIsRejected(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "01.flac")
	if err := os.WriteFile(track, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Named like a cover, and is actually a text file — which happens when a
	// download fails and leaves an error page behind.
	if err := os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte("<html>404</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(dir, "no-such-ffmpeg")})
	if _, err := r.ForAlbum(context.Background(), []string{track}); !errors.Is(err, ErrNoArt) {
		t.Errorf("err = %v, want ErrNoArt — an undecodable file is not a cover", err)
	}
}

func TestDecodableAcceptsPNGAndRejectsJunk(t *testing.T) {
	if !decodable(onePixelPNG) {
		t.Error("a real PNG was rejected")
	}
	if decodable([]byte("not an image at all")) {
		t.Error("junk was accepted as an image")
	}
	if decodable(nil) {
		t.Error("empty bytes were accepted as an image")
	}
}

// The disc folder's own cover wins over one sitting in the album folder above
// it, which is what "nearest first" has to mean in practice.
func TestNearestSidecarWins(t *testing.T) {
	album := t.TempDir()
	disc := filepath.Join(album, "Disc 1")
	if err := os.MkdirAll(disc, 0o755); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(disc, "01.flac")
	if err := os.WriteFile(track, []byte("not really audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disc, "cover.png"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(album, "cover.png"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(album, "no-such-ffmpeg")})
	img, err := r.ForAlbum(context.Background(), []string{track})
	if err != nil {
		t.Fatalf("ForAlbum: %v", err)
	}
	if img.From != filepath.Join(disc, "cover.png") {
		t.Errorf("From = %q, want the disc folder's cover", img.From)
	}
}

// --- shared directories ---------------------------------------------------
//
// Found by running this against a real 398-album library, not by reading the
// code. A library organised into letter buckets drops loose singles straight
// into a folder like "C's"; each groups into its own album by tag, all of them
// name that folder as their directory, and one Folder.jpg there was handed to
// five unrelated records. A grid of different albums wearing the same cover
// reads as a broken scanner — worse than no cover at all.

func TestBucketFolderImageIsNotAdoptedAsAnAlbumCover(t *testing.T) {
	bucket := t.TempDir()
	ours := filepath.Join(bucket, "Artist - Our Song.mp3")
	for _, name := range []string{
		"Artist - Our Song.mp3",
		"Someone Else - Their Song.mp3",
		"A Third Band - Another.mp3",
	} {
		if err := os.WriteFile(filepath.Join(bucket, name), []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bucket, "Folder.jpg"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(bucket, "no-such-ffmpeg")})
	_, err := r.ForAlbum(context.Background(), []string{ours})
	if !errors.Is(err, ErrNoArt) {
		t.Errorf("err = %v, want ErrNoArt — that image belongs to the folder, not this album", err)
	}
}

// The counterpart: a directory holding only this album's tracks is the album's
// own, and its cover is genuinely its cover.
func TestAlbumOwnDirectoryCoverIsStillUsed(t *testing.T) {
	dir := t.TempDir()
	var tracks []string
	for _, name := range []string{"01.mp3", "02.mp3", "03.mp3"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, p)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder.jpg"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(dir, "no-such-ffmpeg")})
	img, err := r.ForAlbum(context.Background(), tracks)
	if err != nil {
		t.Fatalf("ForAlbum: %v — an album's own folder image must still be found", err)
	}
	if img.Source != SourceSidecar {
		t.Errorf("Source = %q, want sidecar", img.Source)
	}
}

// A multi-disc album must not be caught by the shared-directory rule: its
// parent holds the disc folders and no audio of its own, so the cover beside
// them is still this album's.
func TestMultiDiscParentCoverSurvivesTheSharedDirectoryRule(t *testing.T) {
	album := t.TempDir()
	var tracks []string
	for _, disc := range []string{"Disc 1", "Disc 2"} {
		d := filepath.Join(album, disc)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "01.mp3")
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, p)
	}
	if err := os.WriteFile(filepath.Join(album, "cover.jpg"), onePixelPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(&Extractor{Path: filepath.Join(album, "no-such-ffmpeg")})
	img, err := r.ForAlbum(context.Background(), tracks)
	if err != nil {
		t.Fatalf("ForAlbum: %v — a multi-disc album's cover sits one level up", err)
	}
	if img.From != filepath.Join(album, "cover.jpg") {
		t.Errorf("From = %q, want the album folder's cover", img.From)
	}
}

func TestSharedWithOtherAlbums(t *testing.T) {
	dir := filepath.Join("Music", "Bucket")
	ours := []string{filepath.Join(dir, "a.mp3"), filepath.Join(dir, "b.mp3")}

	if sharedWithOtherAlbums([]string{"a.mp3", "b.mp3", "cover.jpg"}, dir, ours) {
		t.Error("a directory holding exactly our tracks was called shared")
	}
	if !sharedWithOtherAlbums([]string{"a.mp3", "b.mp3", "stranger.mp3"}, dir, ours) {
		t.Error("a directory holding a track that is not ours was not called shared")
	}
	// Non-audio files never make a directory shared — artwork, logs and
	// playlists sit beside albums all the time.
	if sharedWithOtherAlbums([]string{"a.mp3", "b.mp3", "notes.txt", "cover.jpg", "playlist.m3u"}, dir, ours) {
		t.Error("non-audio files were counted as another album's tracks")
	}
}
