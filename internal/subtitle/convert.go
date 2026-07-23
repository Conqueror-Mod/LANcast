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

		// A bare number directly before a timing line is SRT's cue counter.
		// WebVTT allows cue identifiers, so keeping it is harmless — but
		// dropping it keeps the output tidy and matches what players expect.
		if !sawTiming && isCueNumber(line) && len(pending) == 0 {
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
