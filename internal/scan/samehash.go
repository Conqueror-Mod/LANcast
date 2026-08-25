package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

/*
 * "Are these two files the same bytes?" — answered by sampling, not by reading
 * them (ADR 0042).
 *
 * The collision report offers size and path for free. The question a person
 * actually asks next is whether the two files are the same, and only reading
 * them can say. Reading them *fully* cannot be the answer: the pairs this was
 * built for include a 14.6 GB mkv, and hashing every collision in a library
 * would be hours of disk for a report somebody opened once.
 *
 * So: size, then three 1 MB windows — head, middle, tail. That is exactly the
 * comparison the ADR 0042 investigation ran by hand, and it is what found that
 * the file calling itself an alternate cut was a byte-for-byte copy:
 *
 *	size    2832374353   2832374353
 *	head 1MB   F4839821…     F4839821…
 *	mid 4MB    4CB76636…     4CB76636…
 *	tail 1MB   8FCBB2BE…     8FCBB2BE…
 *
 * **This is a fingerprint, not a proof**, and the API says so in the field
 * name. Two files can agree on size and all three windows and still differ
 * somewhere in between — contrived for a media file, but not impossible, and
 * the honest word for the result is "identical so far as sampled".
 *
 * That is a defensible trade *because nothing acts on it*. LANcast never
 * merges, ranks or deletes on this answer; it shows it to a person who is
 * deciding. A sampled hash that informs a human is a different risk from a
 * sampled hash that authorises a delete, and the second one is not on offer.
 */

// sampleWindow is how much is read at each of the three offsets.
const sampleWindow = 1 << 20

// Fingerprint is a file's size and sampled content hash.
type Fingerprint struct {
	Size int64
	// Hash is empty when the file could not be read. An unreadable file is not
	// a mismatch — it is an absence of evidence, and the report says so rather
	// than implying the files differ.
	Hash string
}

/*
 * FingerprintFile samples a file at three offsets and hashes what it read.
 *
 * The size goes into the hash as well as being returned, so two files of
 * different lengths cannot collide on the digest even if every sampled window
 * happens to match — which is the realistic near-miss here, since a re-encode
 * often shares neither.
 *
 * A file shorter than the window is hashed whole, which is both correct and
 * cheaper than the sampling it replaces.
 */
func FingerprintFile(path string) (Fingerprint, error) {
	f, err := os.Open(path)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("fingerprint %s: %w", path, err)
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return Fingerprint{}, fmt.Errorf("fingerprint %s: %w", path, err)
	}
	size := st.Size()

	h := sha256.New()
	fmt.Fprintf(h, "%d:", size)

	if size <= 3*sampleWindow {
		if _, err := io.Copy(h, f); err != nil {
			return Fingerprint{}, fmt.Errorf("fingerprint %s: %w", path, err)
		}
		return Fingerprint{Size: size, Hash: hex.EncodeToString(h.Sum(nil))}, nil
	}

	buf := make([]byte, sampleWindow)
	for _, off := range []int64{0, size/2 - sampleWindow/2, size - sampleWindow} {
		if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			return Fingerprint{}, fmt.Errorf("fingerprint %s at %d: %w", path, off, err)
		}
		h.Write(buf)
	}
	return Fingerprint{Size: size, Hash: hex.EncodeToString(h.Sum(nil))}, nil
}
