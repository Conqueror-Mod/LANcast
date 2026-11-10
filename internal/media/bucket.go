package media

import (
	"regexp"
	"strings"
)

/*
 * reLetterBucket matches the index folders people sort a music collection
 * into: "G's", "#", "0-9", "A-Z", "A–C".
 *
 * Only shapes that cannot be a name. A bare letter is left out on purpose: X
 * is a band, and so is "Prof", so a folder called one of them is an artist
 * until it is proven otherwise. "G's" is never an artist, and that is the
 * layout a real library used (2026-11-10).
 */
var reLetterBucket = regexp.MustCompile(`^(?:[\p{L}\d]['’]s|#|[\p{L}\d]\s*[-–]\s*[\p{L}\d])$`)

// IsLetterBucket reports whether a folder name is an alphabetical index
// ("G's", "#", "A-Z") rather than an artist or an album.
func IsLetterBucket(name string) bool {
	return reLetterBucket.MatchString(strings.TrimSpace(name))
}
