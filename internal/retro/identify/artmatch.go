package identify

import (
	"sort"
	"strings"

	"lancast/internal/media"
)

/*
 * bestThumbnail picks, from a thumbnail set's listing, the image for a game
 * whose exact name is not in it — or "" when nothing is the same game.
 *
 * The same game means the same title, through the project's own normaliser
 * (ROMTitle, then SortTitle), and nothing looser: a wrong box is worse than
 * the placeholder, because it looks like an answer. Among those:
 *
 *   - Releases that are not the game somebody owns are passed over unless the
 *     name asks for one: a "Mini" console's re-release, a Virtual Console
 *     build, an alpha, beta, prototype, demo or sample, a kiosk or pirate
 *     cart, and anything bracketed — a hack or a translation.
 *   - Then the most tags in common, which is the region first of all: a
 *     "(USA, Europe) (Rev 1)" dump gets the "(USA, Europe)" box before the
 *     Japanese one.
 *   - Then the fewest tags the name does not have, then the name itself, so
 *     the choice is the same every time.
 *
 * Found on a real library: Road Rash II is "(USA, Europe) (Rev 1)" in the
 * DAT and has boxes only as "(USA, Europe) (RR205)" and "(RR206)", and a fan
 * translation of The Binding Blade, matching no DAT, has a box under its
 * English name.
 */
func bestThumbnail(name string, listing []string) string {
	want := titleKey(name)
	if want == "" {
		return ""
	}
	tags, bracketed := media.ROMTags(name)
	have := map[string]bool{}
	for _, t := range tags {
		have[t] = true
	}

	type cand struct {
		name          string
		common, extra int
	}
	var cands []cand
	for _, c := range listing {
		if titleKey(c) != want {
			continue
		}
		ctags, cbr := media.ROMTags(c)
		if cbr && !bracketed {
			continue
		}
		skip := false
		common := 0
		for _, t := range ctags {
			if have[t] {
				common++
				continue
			}
			if isOtherRelease(t) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		cands = append(cands, cand{c, common, len(ctags) - common})
	}
	if len(cands) == 0 {
		return ""
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.common != b.common {
			return a.common > b.common
		}
		if a.extra != b.extra {
			return a.extra < b.extra
		}
		return a.name < b.name
	})
	return cands[0].name
}

func titleKey(name string) string {
	return media.SortTitle(media.ROMTitle(name))
}

// isOtherRelease reports a tag naming a release that is not the cartridge
// somebody dumped: a re-release, an unfinished build, or an unofficial cart.
func isOtherRelease(tag string) bool {
	if strings.Contains(tag, "virtual console") {
		return true
	}
	for _, w := range strings.Fields(tag) {
		switch w {
		case "mini", "alpha", "beta", "proto", "prototype", "demo", "sample", "kiosk", "pirate":
			return true
		}
	}
	return false
}
