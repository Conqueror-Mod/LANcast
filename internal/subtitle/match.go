package subtitle

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"lancast/internal/media"
)

// Target describes the file a subtitle needs to match.
type Target struct {
	FileName   string  // for release traits
	Title      string  // the movie/series title, for cross-checking the candidate
	Year       int     // 0 when unknown; guards against same-title-different-year
	FPS        float64 // 0 when unknown
	Height     int
	DurationMS int64
}

// Confidence thresholds, mirroring the metadata matcher.
//
// AutoApply is deliberately high and effectively reachable only via a hash
// match or a very strong release agreement. Applying a subtitle that does not
// sync is worse than asking: a wrong subtitle is actively distracting for two
// hours, where a prompt costs one click.
const (
	AutoApply      = 0.90
	PlausibleMatch = 0.55

	// hashBand is the floor for a verified hash match: with no comparable
	// traits a hash match still scores 1.0, but when the release traits are
	// comparable the score spans [hashBand, 1.0] by how well they agree, so
	// several subtitles claiming the same hash for different releases no longer
	// tie at 1.0. The band stays at or above AutoApply so a hash match with a
	// matching frame rate still auto-applies; a frame-rate *conflict* is handled
	// separately and demoted below it.
	hashBand = 0.95
)

// Rank scores candidates against the target and sorts them best first.
func Rank(target Target, cands []Candidate) {
	want := ParseRelease(target.FileName)
	wantRes := HeightToResolution(target.Height)

	for i := range cands {
		cands[i].Score, cands[i].Reason = score(target, want, wantRes, cands[i])
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score != cands[j].Score {
			return cands[i].Score > cands[j].Score
		}
		// Downloads break ties only. It is a popularity signal, and popular
		// is not the same as correct — the most-downloaded entry is often for
		// a different release entirely.
		return cands[i].DownloadCount > cands[j].DownloadCount
	})
}

// BestAutoMatch returns the top candidate if it is safe to apply without
// asking, and reports whether it qualified.
func BestAutoMatch(cands []Candidate) (Candidate, bool) {
	if len(cands) == 0 {
		return Candidate{}, false
	}
	best := cands[0]
	return best, best.Score >= AutoApply
}

func score(target Target, want Traits, wantRes string, c Candidate) (float64, string) {
	name := firstNonEmpty(c.Release, c.FileName)

	// The candidate must be for the same film before anything else is weighed.
	// The provider is asked for this title, but a hash query returns whatever is
	// tagged with that hash, and a query search returns near-title noise — so a
	// subtitle for a different movie routinely appears, and if its frame rate and
	// source happen to agree it scores high enough to auto-apply. This is how a
	// Deadpool 2 subtitle ended up on Avengers. A title disagreement overrides
	// every other signal, including a claimed hash match: a hash that maps to
	// another movie's file is bad provider data, not evidence. The candidate is
	// demoted rather than dropped, so it stays selectable if our own title is
	// wrong, but it can never auto-apply.
	if target.Title != "" && titleMismatch(target.Title, name) {
		return clamp(0.15 + popularity(c.DownloadCount)*0.1),
			"different title (" + releaseTitle(name) + ")"
	}

	// Same title, different year is a different film or a different edition —
	// "Aladdin (1992)" and "Aladdin (2019)" share a title but not a single cue
	// timing. The title gate cannot see this, since both parse to "Aladdin", so
	// the year is checked separately. Like the title gate it overrides a claimed
	// hash match (a hash mapping to another year's release is bad provider data)
	// and demotes rather than drops, so it stays selectable when a candidate
	// simply failed to name its year. Both years must be known to judge.
	if got := media.Parse("", name); target.Year > 0 && got.Year > 0 && got.Year != target.Year {
		return clamp(0.20 + popularity(c.DownloadCount)*0.1),
			fmt.Sprintf("different year (%d)", got.Year)
	}

	got := ParseRelease(name)
	total, weight, reasons := traitAgreement(target, want, wantRes, got, c)

	// A hash match claims this subtitle was timed against these exact bytes.
	// But OpenSubtitles routinely marks several *different-release* subtitles as
	// matching one file's hash — a DVD-rip, a 1080p BluRay, and a 720p WEB of the
	// same film all flagged "exact file match" — when only one can match this
	// file. So the claim is verified against the release traits we know from the
	// probe rather than trusted outright.
	if c.HashMatch {
		// A frame-rate conflict overrides the hash outright: a subtitle timed at
		// a different frame rate drifts progressively out of sync no matter what
		// the hash says. Like the title and year gates, this demotes, not drops.
		if target.FPS > 0 && c.FPS > 0 && math.Abs(target.FPS-c.FPS) >= 0.02 {
			return 0.5, fmt.Sprintf("hash claimed, but a different frame rate (%.3f) would drift", c.FPS)
		}
		// With no comparable traits there is nothing to check the claim against,
		// so trust it. Otherwise score within a high band by trait agreement, so
		// the subtitle whose release actually matches this file ranks above the
		// other claimed hash matches instead of tying with them and being ordered
		// by download count — which is how a DVD-rip ends up on a 1080p file.
		if weight == 0 {
			return 1.0, "matches this exact file"
		}
		s := clamp(hashBand + (1-hashBand)*(total/weight))
		if total/weight >= 0.999 || len(reasons) == 0 {
			return s, "matches this exact file"
		}
		return s, "hash match · " + strings.Join(reasons, ", ")
	}

	if weight == 0 {
		// Nothing comparable. Popularity is all that is left, and it is a weak
		// enough signal that the result must stay below auto-apply.
		return clamp(0.30 + popularity(c.DownloadCount)*0.2), "no matching details; ranked by popularity"
	}

	normalized := total / weight
	// Popularity contributes a small amount and can never carry a candidate
	// over the line on its own.
	final := clamp(normalized*0.9 + popularity(c.DownloadCount)*0.1)

	if len(reasons) == 0 {
		return final, ""
	}
	return final, strings.Join(reasons, ", ")
}

// traitAgreement scores how well a candidate's release traits agree with the
// target file, returning the weighted total, the total weight of the traits
// that were comparable, and human-readable reasons. Shared by the name-based
// path and by hash-match verification so the two cannot disagree about what
// "the same release" means.
func traitAgreement(target Target, want Traits, wantRes string, got Traits, c Candidate) (total, weight float64, reasons []string) {
	// Frame rate is the strongest name-independent signal available: a mismatch
	// drifts progressively worse through the film rather than being a constant
	// offset a viewer could ignore.
	if target.FPS > 0 && c.FPS > 0 {
		weight += 0.35
		if math.Abs(target.FPS-c.FPS) < 0.02 {
			total += 0.35
			reasons = append(reasons, fmt.Sprintf("%.3f fps", c.FPS))
		} else {
			reasons = append(reasons, fmt.Sprintf("different frame rate (%.3f)", c.FPS))
		}
	}

	// A different cut is a different film as far as timings go.
	if want.Edition != "" || got.Edition != "" {
		weight += 0.25
		switch {
		case want.Edition == got.Edition:
			total += 0.25
			if want.Edition != "" {
				reasons = append(reasons, want.Edition+" cut")
			}
		default:
			reasons = append(reasons, "different edition")
		}
	}

	if want.Source != "" && got.Source != "" {
		weight += 0.20
		if want.Source == got.Source {
			total += 0.20
			reasons = append(reasons, got.Source)
		} else {
			reasons = append(reasons, "different source")
		}
	}

	// An exact group match usually means the same encode, so it is worth a lot
	// when present and nothing when absent.
	if want.Group != "" && got.Group != "" {
		weight += 0.15
		if want.Group == got.Group {
			total += 0.15
			reasons = append(reasons, "same release group")
		}
	}

	// Resolution: the candidate's named resolution, or the one its source
	// implies when it names none — a DVD source is standard-definition however
	// the release is titled. This is what separates a DVD-rip subtitle from an
	// HD one on a bare-named file: the file's resolution is known from the probe,
	// and a DVD-sourced sub for a 1080p file is a different, lower master.
	if wantRes != "" {
		candRes := got.Resolution
		if candRes == "" {
			candRes = sourceResolution(got.Source)
		}
		if candRes != "" {
			weight += 0.05
			if candRes == wantRes {
				total += 0.05
			} else {
				reasons = append(reasons, "different resolution ("+candRes+")")
			}
		}
	}

	return total, weight, reasons
}

// sourceResolution returns the resolution a source guarantees when the release
// names none. Only DVD is unambiguous — it is always standard-definition;
// BluRay, WEB, and remux span every resolution and imply nothing.
func sourceResolution(source string) string {
	if source == "dvd" {
		return "480p"
	}
	return ""
}

// popularity compresses download counts into 0..1 with diminishing returns.
func popularity(n int) float64 {
	if n <= 0 {
		return 0
	}
	f := float64(n)
	return f / (f + 500)
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// titleMismatch reports whether a candidate's release name is for a
// recognizably different title than want. It returns false when either title
// yields no usable tokens: a missing title is not evidence of a mismatch, and
// the release-trait scoring still applies.
func titleMismatch(want, candName string) bool {
	a := titleTokens(want)
	b := titleTokens(releaseTitle(candName))
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	// Overlap is measured against the smaller set, so a short subtitle release
	// ("Infinity War") still matches a fuller item title ("Avengers Infinity
	// War"). Real different-movie pairs share essentially no significant tokens.
	small := a
	if len(b) < len(small) {
		small = b
	}
	shared := 0
	for tok := range small {
		if a[tok] && b[tok] {
			shared++
		}
	}
	return float64(shared)/float64(len(small)) < 0.5
}

// releaseTitle pulls the film or series name out of a release name, reusing the
// one filename parser so it agrees with how the library titled the item.
func releaseTitle(name string) string {
	info := media.Parse("", name)
	if info.Series != "" {
		return info.Series
	}
	return info.Title
}

// titleTokens lowercases a title and splits it into the alphanumeric words
// worth comparing, dropping one-character fragments like a trailing sequel "2"
// that carry no title signal.
func titleTokens(title string) map[string]bool {
	out := map[string]bool{}
	fields := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if len([]rune(f)) >= 2 {
			out[f] = true
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
