package lyrics

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"
)

/*
 * Finding the file beside the track.
 *
 * The case that decides this design is an album: twelve tracks in one folder,
 * every one a different song. A rule loose enough to match a file that does not
 * name its track hands the whole record the same words.
 */

type fakeEntry struct {
	name string
	dir  bool
}

func (f fakeEntry) Name() string               { return f.name }
func (f fakeEntry) IsDir() bool                { return f.dir }
func (f fakeEntry) Type() fs.FileMode          { return 0 }
func (f fakeEntry) Info() (fs.FileInfo, error) { return fakeInfo{f}, nil }

type fakeInfo struct{ e fakeEntry }

func (i fakeInfo) Name() string       { return i.e.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return 0 }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return i.e.dir }
func (i fakeInfo) Sys() any           { return nil }

func reader(names ...string) DirReader {
	return func(string) ([]fs.DirEntry, error) {
		out := make([]fs.DirEntry, 0, len(names))
		for _, n := range names {
			out = append(out, fakeEntry{name: n})
		}
		return out, nil
	}
}

func TestTheSidecarBesideTheTrack(t *testing.T) {
	got := FindSidecarWith(filepath.Join("W:", "Music", "Song.flac"),
		reader("Song.flac", "Song.lrc", "cover.jpg"))
	if got != filepath.Join("W:", "Music", "Song.lrc") {
		t.Errorf("found %q", got)
	}
}

func TestAnAlbumDoesNotShareOneLyricFile(t *testing.T) {
	/*
	 * The case this rule exists for. A subtitle may be named for its language
	 * alone and still belong to the only film in its folder; a lyric file that
	 * does not name its track belongs to nothing that can be worked out, and
	 * guessing gives every song on the record the same words.
	 */
	dir := reader(
		"01 First Song.flac", "02 Second Song.flac", "03 Third Song.flac",
		"02 Second Song.lrc", "lyrics.lrc", "English.lrc",
	)
	if got := FindSidecarWith(filepath.Join("W:", "Album", "01 First Song.flac"), dir); got != "" {
		t.Errorf("a track with no lyric file of its own was given %q", got)
	}
	want := filepath.Join("W:", "Album", "02 Second Song.lrc")
	if got := FindSidecarWith(filepath.Join("W:", "Album", "02 Second Song.flac"), dir); got != want {
		t.Errorf("found %q, want %q", got, want)
	}
}

func TestTheExtensionIsMatchedWhateverItsCase(t *testing.T) {
	// A file saved as Song.LRC beside Song.flac is that track's sidecar on
	// every filesystem this runs on, and being strict means lyrics that exist
	// and are never shown.
	for _, name := range []string{"Song.LRC", "Song.Lrc", "SONG.lrc"} {
		if got := FindSidecarWith(filepath.Join("W:", "M", "Song.flac"), reader("Song.flac", name)); got == "" {
			t.Errorf("%q was not matched", name)
		}
	}
}

func TestADirectoryNamedLikeASidecarIsNotOne(t *testing.T) {
	read := func(string) ([]fs.DirEntry, error) {
		return []fs.DirEntry{fakeEntry{name: "Song.lrc", dir: true}}, nil
	}
	if got := FindSidecarWith(filepath.Join("W:", "M", "Song.flac"), read); got != "" {
		t.Errorf("a directory was returned as a lyric file: %q", got)
	}
}

func TestAnUnreadableDirectoryIsNoLyricsRatherThanAFailure(t *testing.T) {
	// A permission error on a folder is not a reason for a track to stop
	// playing, and this answers a question nobody has to ask.
	read := func(string) ([]fs.DirEntry, error) { return nil, errors.New("nope") }
	if got := FindSidecarWith(filepath.Join("W:", "M", "Song.flac"), read); got != "" {
		t.Errorf("found %q in a directory that could not be read", got)
	}
}

func TestLyricsInATagAreReadTheSameWayAFileIs(t *testing.T) {
	// Some tags carry timestamps, and a reader that assumed otherwise would
	// throw away the only synced copy somebody has.
	got, ok := FromTags(map[string]string{"LYRICS": "[00:12.00]a line\n"})
	if !ok {
		t.Fatal("a tag with words in it reported nothing")
	}
	if !got.Synced || got.Lines[0].AtMS != 12_000 {
		t.Errorf("tag lyrics = %+v, want synced at 12000", got.Lines)
	}
}

func TestTheTagNameIsMatchedWhateverItsCase(t *testing.T) {
	// ffprobe's spelling depends on the container, and a case-sensitive lookup
	// would find lyrics in FLAC and miss them in MP4.
	for _, key := range []string{"LYRICS", "lyrics", "Lyrics", "UNSYNCEDLYRICS"} {
		if _, ok := FromTags(map[string]string{key: "a line"}); !ok {
			t.Errorf("%q was not read", key)
		}
	}
}

func TestATagHoldingNothingIsNotLyrics(t *testing.T) {
	// An empty tag is extremely common and must not light up a panel.
	for _, v := range []string{"", "   ", "\n\n"} {
		if _, ok := FromTags(map[string]string{"LYRICS": v}); ok {
			t.Errorf("a tag containing %q was reported as lyrics", v)
		}
	}
	if _, ok := FromTags(map[string]string{"TITLE": "Song"}); ok {
		t.Error("a track with no lyrics tag reported lyrics")
	}
}

func TestTheTagNameRealFilesActuallyUse(t *testing.T) {
	/*
	 * Found by playing a track rather than by reading a specification.
	 *
	 * An ID3 USLT frame carries a language and a description, and ffmpeg
	 * surfaces it under a key built from them — `lyrics-XXX` on a real MP3 in
	 * the library this was tested against, where XXX is the undefined-language
	 * code, and `lyrics-eng` elsewhere. Matching whole key names found none of
	 * them: the panel said "no lyrics for this track" over a file carrying
	 * 1,453 characters of them.
	 *
	 * The fixture that passed was written from the same wrong assumption as the
	 * code. A fixture cannot catch an error it shares.
	 */
	for _, key := range []string{"lyrics-XXX", "lyrics-eng", "LYRICS-XXX", "lyrics-x-none"} {
		got, ok := FromTags(map[string]string{key: "[Chorus]\nOh, my Lord\n"})
		if !ok {
			t.Errorf("%q was not read as lyrics", key)
			continue
		}
		if len(got.Lines) == 0 {
			t.Errorf("%q gave no lines", key)
		}
	}
}

func TestALyricistIsNotLyrics(t *testing.T) {
	/*
	 * The guard on the rule above. `lyricist` is a different ID3 frame naming a
	 * person, and a prefix match that swallowed it would put somebody's name on
	 * screen as the words to the song.
	 */
	for _, key := range []string{"lyricist", "LYRICIST", "TEXT"} {
		if _, ok := FromTags(map[string]string{key: "Somebody Else"}); ok {
			t.Errorf("%q was read as lyrics", key)
		}
	}
}
