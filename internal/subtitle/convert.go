package subtitle

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// srtTiming matches an SRT cue timing line. SRT uses a comma before
// milliseconds; WebVTT requires a period, and that single character is the
// difference between a track that renders and one the browser silently drops.
var srtTiming = regexp.MustCompile(
	`^(\d{1,2}:\d{2}:\d{2})[,.](\d{1,3})\s*-->\s*(\d{1,2}:\d{2}:\d{2})[,.](\d{1,3})(.*)$`)

// SRTToVTT converts SubRip to WebVTT.
//
// Done in Go rather than by spawning ffmpeg: this is the most common subtitle
// path by a wide margin, and it is a text transformation, not a media one.
// Avoiding a process per track keeps a subtitle picker responsive.
func SRTToVTT(r io.Reader) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("WEBVTT\n\n")

	scanner := bufio.NewScanner(r)
	// Subtitle lines are short, but a malformed file can present one enormous
	// "line"; a larger buffer avoids failing on those.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		pending   []string
		sawTiming bool
	)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		for _, l := range pending {
			out.WriteString(l)
			out.WriteByte('\n')
		}
		out.WriteByte('\n')
		pending = pending[:0]
	}

	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = stripBOM(line)
			first = false
		}
		line = strings.TrimRight(line, "\r")

		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		if m := srtTiming.FindStringSubmatch(line); m != nil {
			sawTiming = true
			pending = append(pending, fmt.Sprintf("%s.%s --> %s.%s%s",
				m[1], padMillis(m[2]), m[3], padMillis(m[4]), m[5]))
			continue
		}

		// A bare number at the start of a cue block is SRT's cue counter. An
		// empty pending buffer means we are at that boundary (just after a blank
		// line), so a lone number here is the counter, not subtitle text — a
		// numeric line of actual text arrives after the timing line, when
		// pending is non-empty. WebVTT allows cue identifiers so keeping it would
		// render, but dropping every counter (not just the first) keeps the
		// output tidy and matches what players expect.
		if isCueNumber(line) && len(pending) == 0 {
			continue
		}

		pending = append(pending, line)
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("subtitle: read: %w", err)
	}
	if !sawTiming {
		return nil, fmt.Errorf("subtitle: no cues found; the file may not be SubRip")
	}
	return out.Bytes(), nil
}

// vttTiming matches a WebVTT cue timing line, capturing the two timestamps and
// whatever cue settings trail them.
var vttTiming = regexp.MustCompile(
	`^(\d{1,2}:\d{2}:\d{2}\.\d{3})\s*-->\s*(\d{1,2}:\d{2}:\d{2}\.\d{3})(.*)$`)

// ShiftVTT moves every cue earlier by offset seconds, so a subtitle lines up
// with a transcode that began partway into the film. A transcode restarts the
// media timeline at zero for whatever offset it was asked for, while cue times
// are absolute; without this shift, resuming a film mid-way leaves the cues
// sitting in a future the player never reaches — the subtitles simply never
// appear. Cues that end before the offset are dropped; a cue straddling it is
// clamped to start at zero.
func ShiftVTT(vtt []byte, offset float64) []byte {
	if offset <= 0 {
		return vtt
	}

	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(vtt))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// drop suppresses the text lines of a cue whose timing fell entirely before
	// the offset, until the blank line that ends the cue.
	drop := false
	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "" {
			drop = false
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}

		if m := vttTiming.FindStringSubmatch(line); m != nil {
			start := parseVTTStamp(m[1]) - offset
			end := parseVTTStamp(m[2]) - offset
			if end <= 0 {
				drop = true // this cue is in the past; skip it and its text
				continue
			}
			if start < 0 {
				start = 0
			}
			out.WriteString(fmt.Sprintf("%s --> %s%s\n", formatVTTStamp(start), formatVTTStamp(end), m[3]))
			continue
		}

		if drop {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func parseVTTStamp(s string) float64 {
	var h, m int
	var sec float64
	if _, err := fmt.Sscanf(s, "%d:%d:%f", &h, &m, &sec); err != nil {
		return 0
	}
	return float64(h*3600+m*60) + sec
}

func formatVTTStamp(t float64) string {
	if t < 0 {
		t = 0
	}
	h := int(t) / 3600
	m := (int(t) % 3600) / 60
	s := t - float64(h*3600+m*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", h, m, s)
}

// EnsureVTTHeader adds the WebVTT signature if it is missing.
//
// Files named .vtt in the wild are frequently SRT with the extension changed.
// A browser rejects the whole track without the header, showing no subtitles
// and no explanation.
func EnsureVTTHeader(body []byte) []byte {
	trimmed := bytes.TrimLeft(body, bom+" \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("WEBVTT")) {
		return body
	}
	if bytes.Contains(trimmed, []byte("-->")) && bytes.Contains(trimmed, []byte(",")) {
		// Looks like SRT wearing a .vtt extension.
		if converted, err := SRTToVTT(bytes.NewReader(body)); err == nil {
			return converted
		}
	}
	return append([]byte("WEBVTT\n\n"), body...)
}

// padMillis normalizes SRT's millisecond field to three digits. "5" means 500
// milliseconds, not 5, and getting it wrong shifts every cue.
func padMillis(ms string) string {
	switch len(ms) {
	case 3:
		return ms
	case 2:
		return ms + "0"
	case 1:
		return ms + "00"
	default:
		return ms[:3]
	}
}

func isCueNumber(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" || len(s) > 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// bom is the UTF-8 byte order mark, written as an escape rather than a literal
// so the source file itself stays BOM-free.
const bom = "\ufeff"

func stripBOM(s string) string {
	return strings.TrimPrefix(s, bom)
}

// LooksLikeText reports whether a file's opening bytes are plausible UTF-8
// text, so a mislabelled binary is refused before it reaches a player.
func LooksLikeText(head []byte) bool {
	if len(head) == 0 {
		return false
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	return utf8.Valid(head) || bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF})
}
