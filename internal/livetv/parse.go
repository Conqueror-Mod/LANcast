// Package livetv reads channel lists.
//
// A channel list is an M3U, which this project already parses — but for a
// different dialect and a different purpose, which is why this is a second
// parser rather than a flag on the first.
//
// `internal/playlist` reads a *media playlist*: paths to files on disk, whose
// own tags are authoritative (ADR 0024), where the `#EXTINF` title is advisory
// and the attributes are noise to be skipped. An IPTV channel list is the
// opposite in every one of those: the entries are URLs that nothing local can
// describe, the `#EXTINF` title is the *only* name a channel will ever have,
// and the attributes are the payload — `tvg-logo` is the channel's picture and
// `group-title` is how a list of six hundred becomes navigable.
//
// Making one parser serve both would mean a mode flag deciding whether the
// attributes matter and whether a missing local file is an error, which is two
// parsers wearing one name.
package livetv

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// ErrNotAPlaylist is returned when the input is not an M3U at all — an HTML
// error page from a provider whose subscription lapsed is the common case, and
// importing it as one channel called "<!DOCTYPE html>" is worse than refusing.
var ErrNotAPlaylist = errors.New("livetv: not an M3U channel list")

// Channel is one entry in a channel list.
type Channel struct {
	Name string
	URL  string
	// LogoURL is `tvg-logo`. Kept as a URL rather than fetched here: the
	// artwork cache is content-addressed and fetching belongs to a worker, not
	// to a parser that may be reading six hundred of these.
	LogoURL string
	// Group is `group-title` — "Sports", "News", "UK". The single most useful
	// attribute in the file, because it is the difference between a wall of
	// channels and a list somebody can find anything in.
	Group string
}

// maxChannels bounds a list. A provider playlist of ten thousand channels is
// real, and importing it whole makes every page that lists channels unusable
// while filling the database with entries nobody will scroll to.
const maxChannels = 5000

/*
 * Parse reads a channel list.
 *
 * Tolerant of everything except not being a playlist, for the same reason the
 * media playlist parser is: these files are written by dozens of tools, and one
 * malformed `#EXTINF` is not a reason to refuse the other five hundred entries.
 * A line that cannot be understood is skipped rather than guessed at.
 */
func Parse(r io.Reader) ([]Channel, error) {
	sc := bufio.NewScanner(r)
	// Channel URLs with query strings and tokens are long, and the default
	// 64KB line limit is not obviously enough for a provider that signs every
	// URL. A truncated URL would be stored and fail at play time.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out []Channel
	var pending Channel
	var sawHeader, sawAnyDirective bool

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			upper := strings.ToUpper(line)
			switch {
			case strings.HasPrefix(upper, "#EXTM3U"):
				sawHeader = true
				sawAnyDirective = true
			case strings.HasPrefix(upper, "#EXTINF:"):
				pending = parseExtinf(line)
				sawAnyDirective = true
			default:
				// #EXTGRP, #EXTVLCOPT and the rest: not understood, not fatal.
				sawAnyDirective = true
			}
			continue
		}

		// A bare line is the URL the preceding #EXTINF described.
		if !looksLikeURL(line) {
			// A channel list whose entries are file paths is a media playlist
			// somebody has handed to the wrong importer. Skipping is right:
			// the alternative is a channel that cannot ever play.
			pending = Channel{}
			continue
		}
		pending.URL = line
		if pending.Name == "" {
			// A URL with no #EXTINF has no name but is still a channel. Naming
			// it after its host beats an empty row in a list.
			pending.Name = hostOf(line)
		}
		out = append(out, pending)
		pending = Channel{}
		if len(out) >= maxChannels {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// The header is the only real signal that this is an M3U, but plenty of
	// real files omit it — so absence alone is not a refusal. What *is* a
	// refusal is a file with no directives and no URLs at all, which is what an
	// HTML error page looks like from here.
	if len(out) == 0 && !sawHeader && !sawAnyDirective {
		return nil, ErrNotAPlaylist
	}
	return out, nil
}

/*
 * parseExtinf reads `#EXTINF:-1 tvg-logo="…" group-title="…",Channel Name`.
 *
 * The attributes sit between the duration and the comma, and the title is
 * everything after the *first* comma — channel names contain commas ("BBC One
 * HD, London") and splitting on the last one truncates them.
 */
func parseExtinf(line string) Channel {
	rest := line[len("#EXTINF:"):]
	head, title := cutOutsideQuotes(rest)

	c := Channel{Name: strings.TrimSpace(title)}
	c.LogoURL = attr(head, "tvg-logo")
	c.Group = attr(head, "group-title")
	if c.Name == "" {
		// Some lists carry the name only as tvg-name.
		c.Name = attr(head, "tvg-name")
	}
	return c
}

/*
 * cutOutsideQuotes splits the attribute run from the title at the first comma
 * that is not inside a quoted value.
 *
 * A plain split on the first comma is wrong, and wrong in a way that looks
 * right on every tidy file: `tvg-logo="https://cdn.example/img?id=1,2"` has a
 * comma inside it, so the split lands mid-URL and the "title" becomes the tail
 * of a logo address. Splitting on the *last* comma is wrong the other way,
 * because channel names contain commas too ("BBC One HD, London").
 *
 * Neither shortcut survives a real provider list, so the quote state is tracked.
 */
func cutOutsideQuotes(s string) (head, title string) {
	inQuotes := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				return s[:i], s[i+1:]
			}
		}
	}
	return s, ""
}

/*
 * attr pulls `key="value"` out of the attribute run.
 *
 * Written by hand rather than with a regular expression because the values
 * contain characters that make an expression fragile — URLs with commas and
 * equals signs are ordinary in `tvg-logo` — and because the failure mode of a
 * greedy match here is a logo URL that swallows the group name.
 */
func attr(s, key string) string {
	i := strings.Index(s, key+"=")
	if i < 0 {
		return ""
	}
	rest := s[i+len(key)+1:]
	if len(rest) == 0 {
		return ""
	}
	if rest[0] == '"' {
		if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
			return rest[1 : 1+end]
		}
		return ""
	}
	// Unquoted values end at the next space.
	if end := strings.IndexAny(rest, " \t"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func looksLikeURL(s string) bool {
	lower := strings.ToLower(s)
	for _, scheme := range []string{"http://", "https://", "rtsp://", "rtmp://", "udp://", "rtp://"} {
		if strings.HasPrefix(lower, scheme) {
			return true
		}
	}
	return false
}

func hostOf(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "Channel"
	}
	return s
}
