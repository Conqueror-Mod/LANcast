package api

import (
	"net/http"
	"os"
	"path/filepath"

	"lancast/internal/lyrics"
)

/*
 * The words to a track, from the disk they are already on.
 *
 * No provider and no network. An `.lrc` beside the file, or the tag embedded in
 * it — which is the same position everything else here takes, and the reason a
 * music player can show lyrics without telling anybody's server what is
 * playing.
 *
 * Sidecar first, exactly as subtitles resolve, and for the same two reasons:
 * the sidecar is the one a person can fix, and it is the one that carries
 * timestamps. Unsynced lyrics are a text file; synced lyrics follow the song.
 *
 * This is a handler that turns a database row into filesystem access, so it
 * re-verifies containment within the library root before opening anything. The
 * database is trusted; a bad or hand-edited row must not become arbitrary file
 * read access (CLAUDE.md).
 */

// maxLyricBytes caps a sidecar. A song is a few kilobytes of words; anything
// past this is not a lyric file, and the whole thing is read into memory and
// handed to a page as JSON.
const maxLyricBytes = 256 << 10

type lyricsView struct {
	/*
	 * Source says where the words came from: `sidecar`, `embedded`, or `none`.
	 *
	 * Reported because the answer changes what a person should do about it. A
	 * file beside the track can be edited; a tag needs a tag editor; and
	 * "none" is the only one of the three that is a reason to go looking.
	 */
	Source string `json:"source"`
	lyrics.Lyrics
}

func (s *Server) itemLyrics(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}

	it, err := s.st.GetItem(r.Context(), id, s.userID(r))
	if s.notFoundOr(w, err, "get item", "no such item") {
		return
	}
	if it.Path == "" {
		// A container has no file, so it has no words. An empty answer rather
		// than an error: the question is reasonable and the answer is none.
		writeJSON(w, http.StatusOK, lyricsView{Source: "none", Lyrics: lyrics.Lyrics{Lines: []lyrics.Line{}}})
		return
	}

	path, err := s.itemFilePath(r, it)
	if err != nil {
		s.log.Error("lyrics containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	if found, ok := s.sidecarLyrics(path); ok {
		writeJSON(w, http.StatusOK, lyricsView{Source: "sidecar", Lyrics: found})
		return
	}

	/*
	 * The embedded tag, which costs an ffprobe.
	 *
	 * Nothing in the database holds it: probe throws the LYRICS tag away on
	 * purpose, because a multi-kilobyte blob has no business in a struct that
	 * answers "can this client play this file", and storing every tag to find
	 * four of them is not a trade worth making. So it is read on demand, once,
	 * when somebody has opened the panel that displays it — a container read
	 * with no stream decoding, which is the cheap half of probing.
	 *
	 * Second rather than first because the sidecar is the one a person can fix.
	 * Parsed with the same reader a file gets, since some tags carry
	 * timestamps.
	 */
	if s.prober != nil && s.prober.Available() {
		if tags, err := s.prober.ReadRawTags(r.Context(), path); err == nil {
			if found, ok := lyrics.FromTags(tags); ok {
				if found.Lines == nil {
					found.Lines = []lyrics.Line{}
				}
				writeJSON(w, http.StatusOK, lyricsView{Source: "embedded", Lyrics: found})
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, lyricsView{Source: "none", Lyrics: lyrics.Lyrics{Lines: []lyrics.Line{}}})
}

/*
 * sidecarLyrics reads the `.lrc` beside a track, if there is one.
 *
 * The sidecar is resolved from the *contained* path rather than from the
 * database row, so it inherits the containment check above rather than needing
 * its own — and it is found by directory listing rather than by guessing a
 * filename, so a name differing only in case is still found.
 */
func (s *Server) sidecarLyrics(trackPath string) (lyrics.Lyrics, bool) {
	sidecar := lyrics.FindSidecar(trackPath)
	if sidecar == "" {
		return lyrics.Lyrics{}, false
	}
	// Belt and braces: the sidecar came from listing the track's own directory,
	// so it cannot be elsewhere, and asserting that costs nothing.
	if filepath.Dir(sidecar) != filepath.Dir(trackPath) {
		return lyrics.Lyrics{}, false
	}

	f, err := os.Open(sidecar)
	if err != nil {
		return lyrics.Lyrics{}, false
	}
	defer f.Close()

	if info, err := f.Stat(); err != nil || info.IsDir() || info.Size() > maxLyricBytes {
		return lyrics.Lyrics{}, false
	}

	found := lyrics.Parse(f)
	if found.Empty() {
		// A file that exists and holds nothing is not an answer. Falling
		// through to the tag is what somebody would want, and "we found lyrics"
		// over a blank panel is worse than "none".
		return lyrics.Lyrics{}, false
	}
	if found.Lines == nil {
		found.Lines = []lyrics.Line{}
	}
	return found, true
}
