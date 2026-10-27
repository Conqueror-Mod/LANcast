package media

import (
	"path/filepath"
	"strings"
)

/*
 * What an extra is, and what to call it (ADR 0098).
 *
 * Here rather than beside the code that lists a film's extras, for the rule
 * this package exists for: all filename guessing lives in internal/media.
 * IsExtra decides *whether* a file is an extra; ClassifyExtra says *which
 * kind*, and what a person should read on its tile.
 */

// Extra kinds, in the order a detail page lists them.
const (
	ExtraTrailer        = "trailer"
	ExtraFeaturette     = "featurette"
	ExtraBehindTheScene = "behind_the_scenes"
	ExtraDeletedScene   = "deleted_scene"
	ExtraInterview      = "interview"
	ExtraScene          = "scene"
	ExtraShort          = "short"
	ExtraOther          = "other"
)

// ExtraKindOrder ranks kinds for display: trailers first, as every player does.
var ExtraKindOrder = map[string]int{
	ExtraTrailer: 0, ExtraFeaturette: 1, ExtraBehindTheScene: 2, ExtraDeletedScene: 3,
	ExtraInterview: 4, ExtraScene: 5, ExtraShort: 6, ExtraOther: 7,
}

var dirKinds = map[string]string{
	"trailers":        ExtraTrailer,
	"featurettes":     ExtraFeaturette,
	"behindthescenes": ExtraBehindTheScene,
	"deletedscenes":   ExtraDeletedScene,
	"interviews":      ExtraInterview,
	"scenes":          ExtraScene,
	"shorts":          ExtraShort,
	"extras":          ExtraOther,
	"other":           ExtraOther,
}

var suffixKinds = []struct{ suffix, kind string }{
	{"-trailer", ExtraTrailer},
	{"-featurette", ExtraFeaturette},
	{"-behindthescenes", ExtraBehindTheScene},
	{"-deleted", ExtraDeletedScene},
	{"-interview", ExtraInterview},
	{"-scene", ExtraScene},
	{"-short", ExtraShort},
	{"-other", ExtraOther},
}

// IsSample reports a sample clip: junk a person never wants listed, so it is
// excluded from extras as it is from the library.
func IsSample(path string) bool {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	return base == "sample" || strings.HasSuffix(base, "-sample") ||
		strings.HasSuffix(base, ".sample") || strings.HasSuffix(base, " sample")
}

/*
 * ClassifyExtra names an extra's kind and title from its path.
 *
 * dir is the extras folder it sits in ("Trailers"), or "" when it sits beside
 * the film. The folder wins over a suffix, because the folder is what a person
 * organised; a suffix is what a tool wrote. A bare "trailer.mkv" is a trailer.
 *
 * The title is the filename with its marker removed and separators turned to
 * spaces: "The Film-featurette.mkv" reads "The Film", "Making_of.mp4" reads
 * "Making of".
 */
func ClassifyExtra(dir, path string) (kind, title string) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	lower := strings.ToLower(stem)

	kind = ""
	if dir != "" {
		kind = dirKinds[normalizeDirName(dir)]
	}
	cut := stem
	for _, s := range suffixKinds {
		if strings.HasSuffix(lower, s.suffix) {
			if kind == "" {
				kind = s.kind
			}
			cut = stem[:len(stem)-len(s.suffix)]
			break
		}
	}
	if lower == "trailer" && kind == "" {
		kind = ExtraTrailer
	}
	if kind == "" {
		kind = ExtraOther
	}

	title = strings.TrimSpace(strings.NewReplacer("_", " ", ".", " ").Replace(cut))
	title = strings.Join(strings.Fields(title), " ")
	if title == "" || strings.EqualFold(title, "trailer") {
		title = extraKindLabel(kind)
	}
	return kind, title
}

func extraKindLabel(kind string) string {
	switch kind {
	case ExtraTrailer:
		return "Trailer"
	case ExtraFeaturette:
		return "Featurette"
	case ExtraBehindTheScene:
		return "Behind the scenes"
	case ExtraDeletedScene:
		return "Deleted scene"
	case ExtraInterview:
		return "Interview"
	case ExtraScene:
		return "Scene"
	case ExtraShort:
		return "Short"
	}
	return "Extra"
}

// IsExtraDir reports whether a folder name is one of the extras folders.
func IsExtraDir(name string) bool { return extraDirs[normalizeDirName(name)] }

// ExtraStemMatches reports whether a suffix-marked extra beside a film belongs
// to that film: "Alien-trailer" belongs to "Alien", not to "Aliens". Used where
// several films share a folder and the folder cannot say whose extra it is.
func ExtraStemMatches(filmPath, extraPath string) bool {
	film := strings.ToLower(strings.TrimSuffix(filepath.Base(filmPath), filepath.Ext(filmPath)))
	extra := strings.ToLower(strings.TrimSuffix(filepath.Base(extraPath), filepath.Ext(extraPath)))
	for _, s := range suffixKinds {
		if strings.HasSuffix(extra, s.suffix) {
			return strings.TrimSpace(extra[:len(extra)-len(s.suffix)]) == film
		}
	}
	return false
}
