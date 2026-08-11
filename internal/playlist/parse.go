// Package playlist reads .m3u playlists.
//
// Parsing is pure and takes a reader, for the reason probe.ParseJSON is pure:
// the interesting part is the dialect zoo — extended and plain, absolute and
// relative, Windows separators in a file read on Linux, a BOM, bytes that are
// not UTF-8 — and every one of those is a fixture that needs no disk, no
// database and no scanner to test. See ADR 0030.
package playlist

import (
	"bufio"
	"errors"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrHLS is returned for a playlist that is one of ours.
//
// .m3u8 means both "UTF-8 M3U" and "the thing HLS uses", and LANcast serves
// HLS (ADR 0013). Without this, a scanner pointed anywhere near the transcode
// cache would import a playlist the server had just written, full of segment
// files, as if it were somebody's mixtape.
var ErrHLS = errors.New("playlist: this is an HLS playlist, not a media playlist")

// Entry is one line of a playlist: the path as written, plus whatever the
// #EXTINF line claimed about it.
type Entry struct {
	// Path exactly as it appeared in the file. Not cleaned, not resolved —
	// Resolve does that, because it needs to know where the file was.
	Path string

	// Title from #EXTINF, empty for a plain M3U. Advisory only: the tags in the
	// file the path points at are authoritative for music (ADR 0024), and a
	// playlist written by another application is a worse source than the file
	// itself. Kept because it is the only name a missing entry will ever have.
	Title string

	// Seconds from #EXTINF. Zero when absent, -1 when the file said -1, which
	// is the convention for a stream of unknown length.
	Seconds int
}

// Parse reads a playlist. It never fails on a malformed line — a playlist is a
// text file written by anything, and one bad #EXTINF is not a reason to refuse
// the other two hundred entries. It fails only when the input is not a media
// playlist at all.
func Parse(r io.Reader) ([]Entry, error) {
	sc := bufio.NewScanner(r)
	// A playlist line is a path, and a path can be long; the default 64KB token
	// limit is generous but a deep tree plus a long filename can approach it.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []Entry
	var pending Entry
	first := true

	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff") // BOM, common from Windows editors
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			upper := strings.ToUpper(line)
			// HLS's own tags. Checked before anything else is believed about
			// the file: an HLS playlist is full of lines that look like
			// relative paths, and importing one produces a playlist of
			// fragments.
			if strings.HasPrefix(upper, "#EXT-X-") {
				return nil, ErrHLS
			}
			if strings.HasPrefix(upper, "#EXTINF:") {
				pending = parseExtinf(line)
			}
			// Every other # line is a comment, including #EXTM3U itself.
			continue
		}

		pending.Path = line
		out = append(out, pending)
		pending = Entry{}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseExtinf reads `#EXTINF:<seconds>,<title>`.
//
// Tolerant on purpose. Real files carry `#EXTINF:123` with no comma, extra
// attributes before the comma, and a title containing further commas — which is
// why the split is on the first comma only.
func parseExtinf(line string) Entry {
	rest := line[len("#EXTINF:"):]
	var e Entry
	head, title, found := strings.Cut(rest, ",")
	if found {
		e.Title = strings.TrimSpace(title)
	}
	// Attributes may follow the duration (`123 tvg-id="x"`), so take the
	// leading numeric run rather than the whole field.
	head = strings.TrimSpace(head)
	if i := strings.IndexAny(head, " \t"); i >= 0 {
		head = head[:i]
	}
	if n, err := strconv.Atoi(head); err == nil {
		e.Seconds = n
	}
	return e
}

// Resolve turns an entry's path into an absolute filesystem path, given the
// directory the playlist itself was read from.
//
// Separators are normalised both ways. A playlist written on Windows and read
// on Linux is the ordinary case for this project — the server runs on both, and
// the same library may be mounted by each — and `a\b\c.mp3` is one path
// component to Go on Linux unless something says otherwise.
//
// Returns ok=false for anything that cannot become a local file: a URL, or an
// empty path. A remote stream in a playlist is a real thing and not one LANcast
// can play from its library, so it is dropped here rather than becoming a row
// that fails at playback.
func Resolve(base, p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	// Scheme test before separator rewriting, or `http://x/y` acquires
	// backslashes on Windows and stops looking like a URL.
	if i := strings.Index(p, "://"); i > 0 && !strings.ContainsAny(p[:i], `\/.`) {
		return "", false
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if path.IsAbs(p) || filepath.IsAbs(p) || hasDriveLetter(p) {
		return filepath.Clean(filepath.FromSlash(p)), true
	}
	return filepath.Clean(filepath.Join(base, filepath.FromSlash(p))), true
}

// hasDriveLetter spots `C:/x` on a non-Windows build, where filepath.IsAbs says
// no. A playlist from a Windows machine read by a Linux server is exactly the
// case this exists for; the path will not resolve there either, but it must be
// reported as an absolute path that is missing rather than quietly joined onto
// the playlist's own directory to make something that never existed.
func hasDriveLetter(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}
