/*
 * Package contain answers one question: is this path inside that folder, on
 * the filesystem rather than on paper?
 *
 * The lexical answer — filepath.Abs, then filepath.Rel — is about the *string*.
 * It is the right first test and the wrong last one: `Films/film.mkv` is a
 * string inside `Films` whatever the file is, and if the file is a symbolic
 * link to `C:\Windows\System32\config\SAM` then opening it opens that. The
 * server runs as LocalSystem, so "anything the service can read" is
 * everything, and the only thing standing between a library folder somebody
 * can write to and the whole machine is this check.
 *
 * So both sides are resolved — every link followed — before comparing, and
 * the lexical test is kept as well. Both must hold: the resolved test is what
 * makes a link out of the library fail, and the lexical one keeps the path a
 * caller goes on to open looking like the thing that was checked.
 *
 * Two shapes of path are not fully on disk, and both are normal:
 *
 *   - A file that does not exist yet — the optimized copy about to be written,
 *     a file a drive that went to sleep used to hold. Its nearest existing
 *     ancestor is resolved and the rest appended, which is exactly what the
 *     OS will do when the file is created.
 *   - A dangling link. That is refused: where it points is decided by whoever
 *     made it, and writing through one creates a file wherever that is.
 *
 * What is not covered, deliberately: the gap between this check and the open
 * that follows it. A link swapped in between is a race against somebody who
 * can already write to the library folder; closing it needs openat-style
 * walking that Windows does not offer through os, and the check is about a
 * link that is simply *there*, which is the case a scan produces.
 *
 * Windows junctions are followed like links. EvalSymlinks stopped following
 * them in Go 1.23, which is why resolve walks the path itself; see there.
 */
package contain

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrOutside is returned when a path lies outside the folder that must hold it,
// by name or once its links are followed.
var ErrOutside = errors.New("resolved path escapes the library root")

// ErrDanglingLink is returned for a link whose target does not exist. Where it
// would point is not knowable until something is created there.
var ErrDanglingLink = errors.New("path is a link to nothing")

// Resolve returns p as an absolute path with every symbolic link followed. A
// path that does not exist yet resolves its nearest existing ancestor and keeps
// the rest as written.
func Resolve(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return resolve(abs, 0)
}

// maxLinks bounds a chain of links. Linux gives up at 40.
const maxLinks = 40

var errTooManyLinks = errors.New("too many levels of links")

/*
 * resolve walks abs one component at a time, following every link it meets.
 *
 * Not filepath.EvalSymlinks, and the reason was measured rather than read: on
 * Windows since Go 1.23, EvalSymlinks does not follow a junction and fails
 * with "path not found" for every path *beneath* one. A library under a
 * junctioned folder is an ordinary Windows setup. Worse than failing, it is
 * inconsistent — a root reached through a junction stays unresolved while a
 * link inside it whose target names the real folder resolves past it, so a
 * link to the library's own files read as outside it.
 *
 * So a junction is followed here like any other link, on both sides of every
 * comparison. A component that does not exist ends the walk and the rest is
 * appended as written — a file about to be created is created there. A link
 * whose target does not exist is ErrDanglingLink.
 */
func resolve(abs string, depth int) (string, error) {
	if depth > maxLinks {
		return "", errTooManyLinks
	}
	abs = filepath.Clean(abs)
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	parts := strings.Split(strings.Trim(abs[len(vol):], string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		if part == "" {
			continue
		}
		next := filepath.Join(cur, part)
		fi, err := os.Lstat(next)
		if errors.Is(err, fs.ErrNotExist) {
			return filepath.Join(append([]string{next}, parts[i+1:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		dest, isLink := linkTarget(next, fi)
		if !isLink {
			cur = next
			continue
		}
		if _, err := os.Stat(next); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", ErrDanglingLink
			}
			return "", err
		}
		if !filepath.IsAbs(dest) {
			// Relative to the folder the link is in, which is cur, already
			// resolved — so a ".." in it climbs the real tree, as the OS does.
			dest = filepath.Join(cur, dest)
		}
		real, err := resolve(dest, depth+1)
		if err != nil {
			return "", err
		}
		cur = real
	}
	return cur, nil
}

// linkTarget reads where a link points, for a symbolic link anywhere and for a
// Windows junction, which Lstat reports as irregular rather than as a link.
// Other irregular files (deduplicated ones, say) are not links: Readlink fails
// for them and they are taken as they are.
func linkTarget(path string, fi fs.FileInfo) (string, bool) {
	m := fi.Mode()
	if m&fs.ModeSymlink == 0 && !(runtime.GOOS == "windows" && m&fs.ModeIrregular != 0) {
		return "", false
	}
	dest, err := os.Readlink(path)
	if err != nil || dest == "" {
		return "", false
	}
	return dest, true
}

// Inside reports whether p is root or lies beneath it, comparing by path
// component. Both are taken as given: callers that need links followed pass
// resolved paths, which is what Within does.
func Inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		// Different volumes on Windows. Unrelated by construction.
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Within confirms target lies inside root both by name and once every link on
// either side is followed, and returns target as a cleaned absolute path — the
// path to open, which is the path that was checked.
func Within(root, target string) (string, error) {
	absRoot, absTarget, err := abs2(root, target)
	if err != nil {
		return "", err
	}
	if !Inside(absRoot, absTarget) {
		return "", ErrOutside
	}
	realRoot, err := Resolve(absRoot)
	if err != nil {
		return "", err
	}
	realTarget, err := Resolve(absTarget)
	if err != nil {
		return "", err
	}
	if !Inside(realRoot, realTarget) {
		return "", ErrOutside
	}
	return absTarget, nil
}

/*
 * EntryWithin is Within for an operation on the directory entry itself rather
 * than on what it names — removing a file.
 *
 * os.Remove never follows the last component: removing a link removes the
 * link. So the link's own target is not the question, and asking it would
 * refuse to delete a stray link that points outside the library — which is
 * exactly the file somebody would want gone. The *folders* above it are still
 * followed, because a linked folder decides which directory the unlink
 * happens in.
 */
func EntryWithin(root, target string) (string, error) {
	absRoot, absTarget, err := abs2(root, target)
	if err != nil {
		return "", err
	}
	if !Inside(absRoot, absTarget) || absRoot == absTarget {
		return "", ErrOutside
	}
	realRoot, err := Resolve(absRoot)
	if err != nil {
		return "", err
	}
	realDir, err := Resolve(filepath.Dir(absTarget))
	if err != nil {
		return "", err
	}
	if !Inside(realRoot, filepath.Join(realDir, filepath.Base(absTarget))) {
		return "", ErrOutside
	}
	return absTarget, nil
}

func abs2(a, b string) (string, string, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return "", "", err
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return "", "", err
	}
	return absA, absB, nil
}
