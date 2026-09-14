package lyrics

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

/*
 * Finding the file beside the track.
 *
 * Narrower than subtitle discovery on purpose, and the difference is worth
 * stating because the two look like the same problem. A subtitle may legitimately
 * be named for its language alone — "Subs/English.srt" — which is why that
 * package has to reason about whether a video is the only one in its folder
 * before it dares use such a file.
 *
 * Lyrics have no language convention and no Subs/ folder, and a music directory
 * is an album: twelve tracks sharing one folder, every one of them a different
 * song. A file that does not name its track cannot be matched to one, and
 * guessing would hand every track on the record the same words. So the stem
 * must match, and that is the whole rule.
 */

// DirReader lists a directory. Injected for the same reason the subtitle
// package injects one: a scan walking an album should read the folder once
// rather than once per track.
type DirReader func(string) ([]fs.DirEntry, error)

// FindSidecar returns the path of the `.lrc` beside a track, or empty.
func FindSidecar(trackPath string) string {
	return FindSidecarWith(trackPath, os.ReadDir)
}

/*
 * FindSidecarWith is FindSidecar against a caller-supplied directory reader.
 *
 * Case-insensitive, because the comparison is between a name on disk and a name
 * on disk: a file saved as `Song.LRC` beside `Song.flac` is the sidecar for
 * that track on every filesystem this runs on, and being strict about it would
 * mean lyrics that exist and are never shown.
 */
func FindSidecarWith(trackPath string, read DirReader) string {
	dir := filepath.Dir(trackPath)
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(trackPath),
		filepath.Ext(trackPath)))
	if stem == "" {
		return ""
	}

	entries, err := read(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".lrc") {
			continue
		}
		if strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))) == stem {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

/*
 * FromTag reads lyrics out of a value already carried in the track's tags.
 *
 * Pure, and takes the string rather than the file, because reading tags means
 * running ffprobe and this package has no business doing that — the same split
 * probe keeps between a decision and the process that informs it.
 *
 * Embedded lyrics are the fallback rather than the first answer. They are
 * usually unsynced, they are frequently wrong in ways nobody can correct
 * without a tag editor, and the sidecar is the one a person can fix. When a
 * tag does carry timestamps this parses them exactly as a file would, which
 * some do.
 */
func FromTag(value string) Lyrics {
	return Parse(strings.NewReader(value))
}

/*
 * TagNames are the tag keys that carry lyrics, in the order worth trying.
 *
 * `LYRICS` is what ffprobe reports for the common cases — Vorbis comments in
 * FLAC and OGG, and ID3's USLT frame, which ffmpeg surfaces under this name
 * rather than its own. The unsynchronised and synchronised spellings appear on
 * files written by older taggers.
 */
var TagNames = []string{"LYRICS", "lyrics", "UNSYNCEDLYRICS", "USLT", "SYNCEDLYRICS"}

// FromTags picks the first tag that carries anything, case-insensitively.
func FromTags(tags map[string]string) (Lyrics, bool) {
	lower := make(map[string]string, len(tags))
	for k, v := range tags {
		lower[strings.ToLower(k)] = v
	}
	for _, name := range TagNames {
		if v, ok := lower[strings.ToLower(name)]; ok && strings.TrimSpace(v) != "" {
			parsed := FromTag(v)
			if !parsed.Empty() {
				return parsed, true
			}
		}
	}
	return Lyrics{}, false
}
