package subtitle

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Traits are the release characteristics that predict whether a subtitle will
// sync with a file.
//
// Extracted rather than stripped: internal/media discards this information to
// find a title, and here it is the whole point. Different masters have
// different frame counts and different edits, so two files of the same film can
// need completely different timings.
type Traits struct {
	Source     string // bluray | web | hdtv | dvd | remux
	Edition    string // theatrical | extended | directors | remastered | unrated | imax
	Resolution string // 2160p | 1080p | 720p | 480p
	Group      string // release group, lowercased
}

var (
	reRemux   = regexp.MustCompile(`(?i)\bremux\b`)
	reSource  = regexp.MustCompile(`(?i)\b(remux|blu-?ray|bdrip|brrip|web-?dl|webrip|web|hdtv|dvdrip|dvd|hdrip)\b`)
	reEdition = regexp.MustCompile(`(?i)\b(theatrical|extended|director'?s?\.?cut|directors|unrated|remastered|imax|ultimate|special\.?edition)\b`)
	reRes     = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p|4k|uhd)\b`)
	// Release groups sit at the end after a hyphen: "...x264-SPARKS.mkv".
	reGroup = regexp.MustCompile(`-([A-Za-z0-9_]{2,20})$`)
)

// ParseRelease extracts traits from a filename or release string.
func ParseRelease(name string) Traits {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	var t Traits

	// REMUX is checked before the general source pattern. Regexp matching is
	// leftmost, and "BluRay.REMUX" would otherwise resolve to bluray — but a
	// remux is an untouched stream copy, a materially different master from a
	// re-encoded BDRip.
	if reRemux.MatchString(base) {
		t.Source = "remux"
	} else if m := reSource.FindStringSubmatch(base); m != nil {
		t.Source = normalizeSource(m[1])
	}
	if m := reEdition.FindStringSubmatch(base); m != nil {
		t.Edition = normalizeEdition(m[1])
	}
	if m := reRes.FindStringSubmatch(base); m != nil {
		t.Resolution = normalizeResolution(m[1])
	}
	if m := reGroup.FindStringSubmatch(base); m != nil {
		t.Group = strings.ToLower(m[1])
	}
	return t
}

func normalizeSource(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", ""))
	switch s {
	case "bluray", "bdrip", "brrip":
		return "bluray"
	case "remux":
		return "remux"
	case "webdl", "webrip", "web":
		return "web"
	case "hdtv":
		return "hdtv"
	case "dvdrip", "dvd", "hdrip":
		return "dvd"
	}
	return s
}

func normalizeEdition(s string) string {
	s = strings.ToLower(strings.NewReplacer(".", "", "'", "", " ", "").Replace(s))
	switch {
	case strings.HasPrefix(s, "director"):
		return "directors"
	case strings.Contains(s, "specialedition"):
		return "special"
	}
	return s
}

func normalizeResolution(s string) string {
	s = strings.ToLower(s)
	if s == "4k" || s == "uhd" {
		return "2160p"
	}
	return s
}

// HeightToResolution maps a probed pixel height to the label release names use.
//
// Bands, not equality: a real 225-film library showed heights of 1040, 1036,
// 1012, 802 and 800. Scope transfers crop the frame, so 4K scope is 3840x1600
// and 1080p scope is 1920x800 — which is why the 2160p band starts well below
// 2160.
//
// The imprecision is deliberate and bounded: height alone cannot separate
// 1080p scope (800 tall) from true 720p, and resolution carries the smallest
// weight in matching precisely because of that.
func HeightToResolution(height int) string {
	switch {
	case height <= 0:
		return ""
	case height >= 1400:
		return "2160p"
	case height >= 900:
		return "1080p"
	case height >= 620:
		return "720p"
	case height >= 500:
		return "576p"
	default:
		return "480p"
	}
}
