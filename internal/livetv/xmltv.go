package livetv

/*
 * XMLTV — the schedule half of Live TV.
 *
 * A channel list says what channels exist. It does not say what is on, and no
 * amount of parsing an M3U harder will make it: the schedule is a second file,
 * in a second format, published on its own cadence. XMLTV is that format, and
 * it is the only one worth supporting — every IPTV provider, every tuner
 * backend and every grabber emits it.
 *
 * The two files are joined on `tvg-id`, which is why the M3U parser now keeps
 * that attribute. It is optional and frequently absent, and that is a real
 * limit rather than something to guess around: a channel with no `tvg-id`
 * simply has no listings, and the alternative — matching on display name —
 * would confidently attach "BBC One" listings to "BBC One HD" and be wrong in a
 * way nobody could see from the guide.
 */

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ErrNotXMLTV is returned when the input is not an XMLTV document. Same reason
// as ErrNotAPlaylist: providers answer a lapsed subscription with an HTML error
// page, and importing that as a guide with no programmes in it looks like a
// working import of an empty schedule.
var ErrNotXMLTV = errors.New("livetv: not an XMLTV document")

// Programme is one entry in a schedule.
type Programme struct {
	// ChannelID is the XMLTV `channel` attribute, matched against a channel's
	// `tvg-id`. Never a LANcast channel id — the join happens at import.
	ChannelID string
	Title     string
	Desc      string
	Category  string
	// Start and Stop are absolute instants. XMLTV carries an offset per
	// timestamp, so a guide can legitimately mix them, and storing anything
	// other than an instant would mean re-deciding the offset at read time.
	Start time.Time
	Stop  time.Time
	// Season and Episode are 1-based, zero when the guide did not say. Only
	// `xmltv_ns` numbering is read; see parseEpisodeNum.
	Season  int
	Episode int
	IconURL string
}

/*
 * maxProgrammes bounds a guide.
 *
 * A fortnight of listings for six hundred channels is roughly half a million
 * entries, and a provider that publishes one is not unusual. The bound is
 * generous because a guide truncated mid-day is worse than useless — it reads
 * as "nothing is on this evening" — but it exists because the parse holds the
 * result in memory before it reaches the database.
 */
const maxProgrammes = 300000

/*
 * ParseXMLTV reads a guide.
 *
 * Streamed with a token decoder rather than unmarshalled whole: a real guide is
 * tens of megabytes of XML, and `xml.Unmarshal` of the document would hold the
 * parsed tree *and* every resulting Programme at once. The token loop keeps one
 * element alive at a time.
 *
 * Tolerant in the same shape as the M3U parser: an entry that cannot be
 * understood is skipped, not guessed at, and one malformed programme does not
 * discard the fortnight around it.
 */
func ParseXMLTV(r io.Reader) ([]Programme, error) {
	dec := xml.NewDecoder(r)
	// Guides carry named entities from HTML (&nbsp; and friends) that a strict
	// XML parser rejects. Refusing a whole schedule over one &eacute; in a film
	// synopsis is not a trade worth making.
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var out []Programme
	sawRoot := false

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// A truncated download is common enough to be worth salvaging: what
			// has already been read is a real, if short, schedule.
			if len(out) > 0 {
				return out, nil
			}
			return nil, ErrNotXMLTV
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "tv":
			sawRoot = true
		case "programme":
			p, ok := decodeProgramme(dec, se)
			if !ok {
				continue
			}
			out = append(out, p)
			if len(out) >= maxProgrammes {
				return out, nil
			}
		default:
			// <channel> elements are deliberately not read. They carry display
			// names, and a display name is exactly the join this refuses to
			// make — the id is the contract.
			continue
		}
	}

	if !sawRoot && len(out) == 0 {
		return nil, ErrNotXMLTV
	}
	return out, nil
}

// xmlProgramme mirrors the subset of <programme> that a guide needs. The rest
// of XMLTV — credits, ratings, star-ratings, subtitles, video quality — is
// real and deliberately dropped: none of it appears in this UI, and decoding it
// costs allocation on every one of half a million entries.
type xmlProgramme struct {
	Start   string `xml:"start,attr"`
	Stop    string `xml:"stop,attr"`
	Channel string `xml:"channel,attr"`

	Titles []string `xml:"title"`
	Descs  []string `xml:"desc"`
	Cats   []string `xml:"category"`

	EpisodeNums []struct {
		System string `xml:"system,attr"`
		Value  string `xml:",chardata"`
	} `xml:"episode-num"`

	Icon struct {
		Src string `xml:"src,attr"`
	} `xml:"icon"`
}

func decodeProgramme(dec *xml.Decoder, se xml.StartElement) (Programme, bool) {
	var x xmlProgramme
	if err := dec.DecodeElement(&x, &se); err != nil {
		return Programme{}, false
	}

	start, ok := ParseXMLTVTime(x.Start)
	if !ok {
		// Without a start there is no schedule — the entry cannot be placed.
		return Programme{}, false
	}
	stop, stopOK := ParseXMLTVTime(x.Stop)
	if !stopOK || !stop.After(start) {
		/*
		 * A missing or nonsensical stop gets an hour.
		 *
		 * Dropping the entry instead would leave a hole in the guide, and a hole
		 * reads as "nothing is on" rather than "the guide was sloppy here". An
		 * hour is the modal programme length, and the following entry's start
		 * is what actually ends a row on screen.
		 */
		stop = start.Add(time.Hour)
	}

	ch := strings.TrimSpace(x.Channel)
	if ch == "" {
		// A programme on no channel cannot be shown anywhere.
		return Programme{}, false
	}

	p := Programme{
		ChannelID: ch,
		Title:     firstNonEmpty(x.Titles),
		Desc:      firstNonEmpty(x.Descs),
		Category:  firstNonEmpty(x.Cats),
		Start:     start,
		Stop:      stop,
		IconURL:   strings.TrimSpace(x.Icon.Src),
	}
	if p.Title == "" {
		// Guides do publish untitled filler around midnight. Naming it beats a
		// blank row that looks like a rendering bug.
		p.Title = "Unknown programme"
	}
	for _, en := range x.EpisodeNums {
		if strings.EqualFold(strings.TrimSpace(en.System), "xmltv_ns") {
			p.Season, p.Episode = parseEpisodeNum(en.Value)
			break
		}
	}
	return p, true
}

/*
 * ParseXMLTVTime reads `20260815143000 +0100`.
 *
 * The offset is optional in the format and absent from plenty of real guides.
 * When it is missing the timestamp is read as **this server's local time**,
 * which is the reading that is right for the case that produces it: a tuner
 * backend on this network generating listings for the place it is standing in.
 * Reading it as UTC instead would put a British evening's television at 8pm
 * only by coincidence, and shift it by an hour every summer.
 *
 * Truncated forms are accepted because XMLTV permits them — a date alone is a
 * legal timestamp, and refusing it would discard a whole channel's day.
 */
func ParseXMLTVTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}

	digits, offset := s, ""
	if i := strings.IndexAny(s, " +-"); i > 0 {
		digits, offset = s[:i], strings.TrimSpace(s[i:])
	}
	digits = strings.TrimSpace(digits)

	// 20260815, 2026081514, 202608151430, 20260815143000 — pad the missing
	// tail with zeroes rather than carrying four layouts.
	if len(digits) > 14 {
		digits = digits[:14]
	}
	switch len(digits) {
	case 8, 10, 12, 14:
	default:
		return time.Time{}, false
	}
	digits += strings.Repeat("0", 14-len(digits))

	loc := time.Local
	if offset != "" {
		z, ok := parseOffset(offset)
		if !ok {
			return time.Time{}, false
		}
		loc = z
	}

	t, err := time.ParseInLocation("20060102150405", digits, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// parseOffset reads `+0100`, `-0500`, `+01:00` and `Z` into a fixed zone. Named
// zones ("BST") are not accepted: they are ambiguous across countries, and a
// guide that used one would be silently placed in the wrong hemisphere.
func parseOffset(s string) (*time.Location, bool) {
	if strings.EqualFold(s, "z") || strings.EqualFold(s, "utc") || strings.EqualFold(s, "gmt") {
		return time.UTC, true
	}
	sign := 1
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		sign, s = -1, s[1:]
	default:
		return nil, false
	}
	s = strings.ReplaceAll(s, ":", "")
	if len(s) == 2 {
		s += "00"
	}
	if len(s) != 4 {
		return nil, false
	}
	h, err1 := strconv.Atoi(s[:2])
	m, err2 := strconv.Atoi(s[2:])
	if err1 != nil || err2 != nil || h > 14 || m > 59 {
		return nil, false
	}
	secs := sign * (h*3600 + m*60)
	if secs == 0 {
		return time.UTC, true
	}
	return time.FixedZone(fmt.Sprintf("UTC%+03d%02d", sign*h, m), secs), true
}

/*
 * parseEpisodeNum reads the `xmltv_ns` system: `2 . 5 . 0/1`, zero-based, with
 * any part possibly empty.
 *
 * Only this system is read. `onscreen` is the other common one and it is free
 * text — "S03E04", "Ep 4", "4/12", "Series 3" — so parsing it means a second
 * guessing engine, and CLAUDE.md keeps guessing in `internal/media`. A guide
 * that publishes only `onscreen` numbering yields a programme with a title and
 * no numbers, which is what the UI shows anyway for most channels.
 */
func parseEpisodeNum(v string) (season, episode int) {
	parts := strings.Split(v, ".")
	get := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		f := strings.TrimSpace(parts[i])
		if j := strings.Index(f, "/"); j >= 0 {
			f = strings.TrimSpace(f[:j])
		}
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return 0
		}
		return n + 1 // xmltv_ns counts from zero; everything else here counts from one.
	}
	return get(0), get(1)
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}
