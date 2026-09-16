package artwork

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

/*
 * Keeping the artwork cache from growing for ever.
 *
 * It is content-addressed and nothing has ever removed anything from it, so it
 * holds every poster of every film ever deleted and every poster a corrected
 * match replaced. On a library of twenty thousand items that is real disk spent
 * on images nothing will ever ask for again.
 *
 * What makes this safe is the *order* things are removed in, which is chosen by
 * what can be recovered and how expensively:
 *
 *  1. Orphans — a hash no item references any more. Pure waste, recoverable
 *     only by asking the provider again, and nothing will ever ask.
 *  2. Derived sizes of live hashes — a thumbnail, a poster, a 2x. Recoverable
 *     *locally* by decoding the original again, no network, no provider quota.
 *  3. Nothing else.
 *
 * A live **original** is never deleted, at any size limit. It is the only copy
 * that cannot be rebuilt without going back to a provider — which needs a key,
 * a network, and the provider still having the image. A cap that could remove
 * one would turn a disk-space setting into "some of your posters are gone now",
 * which is not what anybody means by a cache limit.
 *
 * So a limit is a target rather than a guarantee, and it says so: a library
 * whose live originals alone exceed the cap keeps them and reports the
 * shortfall. The alternative is silently deleting the thing the setting was
 * never about.
 */

// Report is what one pass did, for the log and for a test to assert on.
type Report struct {
	// OrphanFiles and OrphanBytes are artwork no item references.
	OrphanFiles int
	OrphanBytes int64
	// DerivedFiles and DerivedBytes are re-derivable sizes dropped to get under
	// the limit.
	DerivedFiles int
	DerivedBytes int64
	// AfterBytes is what the cache holds once the pass is done.
	AfterBytes int64
	/*
	 * OverBy is how far above the limit the cache still is, which can only
	 * happen when live originals alone exceed it. Reported rather than acted
	 * on: the alternative is deleting an original, and the whole point of the
	 * order above is that originals are not what a size limit is about.
	 */
	OverBy int64
}

// entry is one file in the cache.
type entry struct {
	path  string
	hash  string
	size  Size
	bytes int64
	// modified orders the derived sweep, oldest first. Access time would be
	// better and is not dependable: Windows disables last-access updates by
	// default, so atime is often the creation time wearing a disguise.
	modified int64
}

/*
 * Prune removes what it can and reports what it did.
 *
 * `live` is every hash the library still references; anything else is an
 * orphan. Passing an empty set would therefore delete the entire cache, so a
 * caller that failed to read the library must not call this — the store's query
 * returning an error has to abort the pass rather than proceed with nothing.
 *
 * maxBytes of 0 means no limit, and in that state orphans are *still* removed:
 * they are waste under any policy, and the recoverable-order rule means doing
 * so can never cost a visible poster.
 */
func Prune(root string, live map[string]bool, maxBytes int64) (Report, error) {
	var rep Report
	if root == "" {
		return rep, nil
	}
	if live == nil {
		/*
		 * Refused rather than treated as "nothing is live". The difference
		 * between "this library references no artwork" and "I could not find
		 * out" is the whole cache, and a nil map is what a caller that dropped
		 * an error would hand over.
		 */
		return rep, fmt.Errorf("artwork: prune needs the set of referenced hashes")
	}

	entries, err := scanCache(root)
	if err != nil {
		return rep, err
	}

	var kept []entry
	for _, e := range entries {
		if live[e.hash] {
			kept = append(kept, e)
			continue
		}
		if err := os.Remove(e.path); err != nil {
			// A file that vanished under us, or one held open. Neither is worth
			// abandoning the pass for; the next one will find it.
			continue
		}
		rep.OrphanFiles++
		rep.OrphanBytes += e.bytes
	}

	var total int64
	for _, e := range kept {
		total += e.bytes
	}

	if maxBytes > 0 && total > maxBytes {
		/*
		 * Derived sizes only, oldest first. Each one is a decode away from
		 * coming back, so the cost of being wrong here is CPU rather than a
		 * missing picture.
		 */
		derived := make([]entry, 0, len(kept))
		for _, e := range kept {
			if e.size != SizeOriginal {
				derived = append(derived, e)
			}
		}
		sort.Slice(derived, func(i, j int) bool { return derived[i].modified < derived[j].modified })

		for _, e := range derived {
			if total <= maxBytes {
				break
			}
			if err := os.Remove(e.path); err != nil {
				continue
			}
			rep.DerivedFiles++
			rep.DerivedBytes += e.bytes
			total -= e.bytes
		}
	}

	rep.AfterBytes = total
	if maxBytes > 0 && total > maxBytes {
		rep.OverBy = total - maxBytes
	}
	removeEmptyDirs(root)
	return rep, nil
}

// scanCache lists every artwork file under root.
//
// Files that are not named like artwork are ignored rather than removed. The
// cache directory is LANcast's, but deleting something it does not recognise is
// how a tool earns a reputation.
func scanCache(root string) ([]entry, error) {
	var out []entry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A directory that disappeared mid-walk is not a reason to abandon
			// the pass.
			return nil //nolint:nilerr // see above
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jpg") {
			return nil
		}
		hash := filepath.Base(filepath.Dir(path))
		if !validHash(hash) {
			return nil
		}
		size := Size(strings.TrimSuffix(d.Name(), ".jpg"))
		if !ValidSize(size) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, entry{
			path: path, hash: hash, size: size,
			bytes: info.Size(), modified: info.ModTime().Unix(),
		})
		return nil
	})
	return out, err
}

// removeEmptyDirs tidies the two-level fan-out left behind by a removal.
//
// Best effort: an empty directory costs an inode and nothing else, and failing
// to remove one is not worth reporting.
func removeEmptyDirs(root string) {
	shards, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, shard := range shards {
		if !shard.IsDir() {
			continue
		}
		shardPath := filepath.Join(root, shard.Name())
		hashes, err := os.ReadDir(shardPath)
		if err != nil {
			continue
		}
		for _, h := range hashes {
			if h.IsDir() {
				_ = os.Remove(filepath.Join(shardPath, h.Name()))
			}
		}
		_ = os.Remove(shardPath)
	}
}

// TotalBytes reports what the cache currently holds, for a settings page that
// wants to show the number before anybody changes the limit.
//
// Not called Size, which is already the name of the variant type here — two
// meanings for one word in one package is how a call site ends up reading
// plausibly and meaning something else.
func TotalBytes(root string) (int64, error) {
	entries, err := scanCache(root)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		total += e.bytes
	}
	return total, nil
}
