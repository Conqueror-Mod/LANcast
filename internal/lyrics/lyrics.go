/*
Package lyrics reads words that belong to a track, from the disk they are
already on.

No provider and no network. Everything a first version needs is beside the
file: an `.lrc` sidecar, or the tag embedded in the track. That is not a
limitation being apologised for — it is the same position the rest of this
project takes, and the reason a music player can show lyrics without asking
anybody's server who is listening to what.

**Sidecar first**, exactly as subtitles resolve and for the same reason: the
sidecar is the one a person can fix. It is also the only one of the two that
carries timestamps, which is most of the feature — unsynced lyrics are a text
file, and synced lyrics follow the song.

Parsing is pure and takes a reader, which is what lets every case below be a
fixture rather than a track: an offset tag, a repeated chorus, a file somebody
saved as plain text with a `.lrc` extension.
*/
package lyrics

import (
	"bufio"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Line is one line of a song, at the time it is sung.
type Line struct {
	// AtMS is milliseconds from the start of the track. Meaningless when the
	// lyrics are unsynced, where it stays zero for every line rather than
	// being invented from the line number.
	AtMS int64  `json:"at_ms"`
	Text string `json:"text"`
}

// Lyrics is what was found for one track.
type Lyrics struct {
	/*
	 * Synced is the difference between a feature and a text file.
	 *
	 * Reported rather than inferred from the lines, because "every line is at
	 * zero" is also what a synced file with one line at the start looks like,
	 * and a player deciding which to draw must not have to guess.
	 */
	Synced bool   `json:"synced"`
	Lines  []Line `json:"lines"`

	// Metadata the file carried about itself. Displayed if a client wants it;
	// never written back over the track's own metadata, which came from a
	// provider or an NFO and is not this file's to correct.
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
}

/*
 * timeTag matches one `[mm:ss.xx]` stamp.
 *
 * Fractions are optional and may be two or three digits: two is hundredths,
 * which is what the format specifies and what most files carry, and three is
 * milliseconds, which several editors write anyway. Reading two as
 * milliseconds would put every line a second early.
 */
var timeTag = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)

// metaTag matches `[ar:Artist]` and friends — a tag whose first character is
// not a digit, which is what separates it from a timestamp.
var metaTag = regexp.MustCompile(`^\[([a-zA-Z#]+):(.*)\]$`)

/*
 * wordTag matches the word-level stamps of "enhanced" LRC, `<00:12.34>`.
 *
 * Stripped rather than honoured. Karaoke-style word highlighting is a real
 * feature and not this one; leaving the stamps in the text would print angle
 * brackets across the middle of every line, which is the worst of both.
 */
var wordTag = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`)

/*
Parse reads LRC, falling back to plain text.

A file that carries no timestamps at all is not an error and not empty: plenty
of `.lrc` files in the wild are somebody's copy-and-paste, and the honest thing
is to show the words and say they are not synced rather than to show nothing.

The parse is deliberately forgiving in one direction only. Anything that looks
like a timestamp is used; anything else on a line is kept as text. A strict
reader would reject files people actually have, and the cost of being wrong
here is a line drawn at the wrong second, not a corrupted library.
*/
func Parse(r io.Reader) Lyrics {
	var out Lyrics
	var offsetMS int64
	plain := []string{}

	sc := bufio.NewScanner(r)
	// Songs are short, but a file with no newlines at all should not take the
	// default 64KB limit down with it.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for sc.Scan() {
		raw := strings.TrimRight(sc.Text(), "\r")
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if m := metaTag.FindStringSubmatch(line); m != nil {
			key := strings.ToLower(m[1])
			value := strings.TrimSpace(m[2])
			switch key {
			case "ti":
				out.Title = value
			case "ar":
				out.Artist = value
			case "al":
				out.Album = value
			case "offset":
				if n, err := strconv.ParseInt(strings.TrimPrefix(value, "+"), 10, 64); err == nil {
					offsetMS = n
				}
			}
			// Every other bracketed word — `[by:]`, `[length:]`, `[re:]` — is
			// about the file rather than the song, and is dropped rather than
			// printed as a lyric.
			continue
		}

		stamps := timeTag.FindAllStringSubmatchIndex(line, -1)
		if len(stamps) == 0 {
			plain = append(plain, strings.TrimSpace(wordTag.ReplaceAllString(line, "")))
			continue
		}

		/*
		 * The text is what follows the last stamp on the line.
		 *
		 * A repeated chorus is written as several stamps sharing one line —
		 * `[01:02.00][02:34.00]Here we go` — and each of them is a separate
		 * occurrence of the same words, which is exactly what a player needs to
		 * highlight it twice.
		 */
		text := strings.TrimSpace(wordTag.ReplaceAllString(line[stamps[len(stamps)-1][1]:], ""))
		for _, s := range stamps {
			at := stampMS(line, s)
			out.Lines = append(out.Lines, Line{AtMS: at, Text: text})
		}
		out.Synced = true
	}

	if !out.Synced {
		for _, t := range plain {
			out.Lines = append(out.Lines, Line{Text: t})
		}
		return out
	}

	/*
	 * The offset, applied once here rather than by every reader.
	 *
	 * `[offset:+500]` means the words should appear *earlier* by that much —
	 * it exists so somebody can correct a file against their own copy of a
	 * track without retyping ninety timestamps. Subtracting is the reading
	 * every player in common use takes.
	 *
	 * Clamped at zero: a large positive offset on an early line would
	 * otherwise produce a negative time, which is not a place in a song.
	 */
	if offsetMS != 0 {
		for i := range out.Lines {
			out.Lines[i].AtMS -= offsetMS
			if out.Lines[i].AtMS < 0 {
				out.Lines[i].AtMS = 0
			}
		}
	}

	/*
	 * Sorted, because a file is not obliged to be.
	 *
	 * Chorus stamps are routinely written out of order, and a player that
	 * scans forward for the current line would stop at the first one it passed
	 * and never advance again. Stable, so two lines sharing a timestamp keep
	 * the order the file wrote them in — which is how a two-line couplet stays
	 * a couplet.
	 */
	sort.SliceStable(out.Lines, func(i, j int) bool {
		return out.Lines[i].AtMS < out.Lines[j].AtMS
	})
	return out
}

// stampMS turns one matched `[mm:ss.xx]` into milliseconds.
func stampMS(line string, idx []int) int64 {
	group := func(n int) string {
		lo, hi := idx[2*n], idx[2*n+1]
		if lo < 0 {
			return ""
		}
		return line[lo:hi]
	}
	minutes, _ := strconv.ParseInt(group(1), 10, 64)
	seconds, _ := strconv.ParseInt(group(2), 10, 64)
	ms := int64(0)
	if frac := group(3); frac != "" {
		n, _ := strconv.ParseInt(frac, 10, 64)
		switch len(frac) {
		case 1:
			ms = n * 100
		case 2:
			// Hundredths, which is what the format specifies.
			ms = n * 10
		default:
			ms = n
		}
	}
	return minutes*60_000 + seconds*1000 + ms
}

/*
Empty reports whether anything worth showing was found.

A file of nothing but metadata tags parses successfully and has no words in it,
and "we found lyrics" for a blank panel is worse than "none found".
*/
func (l Lyrics) Empty() bool {
	for _, line := range l.Lines {
		if strings.TrimSpace(line.Text) != "" {
			return false
		}
	}
	return true
}
