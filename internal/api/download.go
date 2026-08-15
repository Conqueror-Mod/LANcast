package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/store"
)

/*
 * Downloading a file, as distinct from streaming one.
 *
 * The bytes already leave the building through /api/stream/{id}. What is
 * missing there is the *intent*: a stream is for a player, and every browser
 * treats it as one — it opens in a tab, seeks, and is gone when the tab is.
 * A download is the same bytes with `Content-Disposition: attachment` and a
 * filename somebody will recognise a week later in a Downloads folder.
 *
 * Deliberately never transcoded. The transcoder exists so a device that cannot
 * play a file can still watch it; a download that quietly handed back a
 * re-encoded copy would be a lie about what you have, and the one operation
 * where the original matters most is the one that takes it off the server.
 *
 * Same containment rule as every other handler that turns a database row into
 * filesystem access, for the same reason (CLAUDE.md): the database is trusted,
 * and this is the boundary where a bad row would become an arbitrary file read.
 */
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusNotFound, "not_found", "item has no file to download")
		return
	}

	path, err := s.itemFilePath(r, it)
	if err != nil {
		s.log.Error("download containment check failed", "item", id, "path", it.Path, "error", err)
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "file is missing from disk")
		return
	}

	name := downloadName(it, filepath.Ext(path))
	// Both forms: the plain one for anything old or strict, the RFC 5987 one
	// carrying the characters the plain one cannot. Browsers prefer filename*
	// where they understand it, which is where an accented title survives.
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		"attachment; filename=%q; filename*=UTF-8''%s",
		asciiFold(name), urlEncodePath(name),
	))
	// Named so a Range request can still resume an interrupted transfer —
	// ServeContent handles that, and a nine-gigabyte file is exactly the case
	// where resuming matters.
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// downloadName builds the filename the browser will save under: what the item
// is called, not what its file happens to be called. A scanned file is often
// `tt0111161.2160p.WEB-DL.x265.mkv`, which says nothing on a desktop three
// weeks later, and the database already knows the answer.
func downloadName(it *store.Item, ext string) string {
	name := it.Title
	if name == "" {
		name = "download"
	}
	// An episode names its series and number, because "Pilot.mkv" collides with
	// every other pilot ever made the moment two shows land in one folder.
	if it.Series != nil && *it.Series != "" && it.Season != nil && it.Episode != nil {
		name = fmt.Sprintf("%s - S%02dE%02d - %s", *it.Series, *it.Season, *it.Episode, name)
	} else if it.Year != nil && *it.Year > 0 {
		name = fmt.Sprintf("%s (%d)", name, *it.Year)
	}
	return sanitizeFilename(name) + strings.ToLower(ext)
}

// sanitizeFilename strips what a filesystem — any of them — will not take. The
// server is not saving this file, but it is proposing a name to a client that
// will, and proposing `AC/DC: Live` means proposing a directory on Linux and a
// refusal on Windows.
func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(strings.Trim(s, "."))
	if s == "" {
		return "download"
	}
	// Long enough for any real title, short enough to survive the 255-byte
	// limit every common filesystem shares once an extension is added.
	if len(s) > 180 {
		s = strings.TrimSpace(s[:180])
	}
	return s
}

// asciiFold is the fallback half of Content-Disposition: the quoted `filename=`
// parameter is bytes, and a raw multibyte title there is decoded by guesswork.
// Anything outside printable ASCII becomes an underscore, and `filename*`
// carries the real name for every client that reads it.
func asciiFold(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// urlEncodePath percent-encodes for the RFC 5987 `filename*` parameter, which
// is stricter than a URL path: a space must be %20 and not '+', and the
// separators url.PathEscape leaves alone are exactly the ones that would end
// the header parameter early.
func urlEncodePath(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
