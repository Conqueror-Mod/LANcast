package subtitle

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// hashChunk is the number of bytes read from each end of the file.
const hashChunk = 64 * 1024

// MovieHash computes the OpenSubtitles hash: the file size plus the 64-bit
// words of the first and last 64KB, summed with wraparound.
//
// This is the strongest matching signal available. A subtitle uploaded against
// a hash was timed against these exact bytes, so it syncs — no inference from
// release names, resolutions or runtimes required. It reads 128KB regardless of
// file size, so it costs the same on a 40GB remux as on a 700MB rip.
func MovieHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("moviehash: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("moviehash: %w", err)
	}
	size := info.Size()
	if size < hashChunk*2 {
		// The algorithm is only defined for files with two full chunks, and
		// anything this small is not a film.
		return "", fmt.Errorf("moviehash: file is too small (%d bytes)", size)
	}

	hash := uint64(size)

	head := make([]byte, hashChunk)
	if _, err := io.ReadFull(f, head); err != nil {
		return "", fmt.Errorf("moviehash: read head: %w", err)
	}
	hash += sumWords(head)

	if _, err := f.Seek(size-hashChunk, io.SeekStart); err != nil {
		return "", fmt.Errorf("moviehash: seek tail: %w", err)
	}
	tail := make([]byte, hashChunk)
	if _, err := io.ReadFull(f, tail); err != nil {
		return "", fmt.Errorf("moviehash: read tail: %w", err)
	}
	hash += sumWords(tail)

	return fmt.Sprintf("%016x", hash), nil
}

// sumWords adds the little-endian 64-bit words of b, wrapping on overflow.
func sumWords(b []byte) uint64 {
	var sum uint64
	for i := 0; i+8 <= len(b); i += 8 {
		sum += binary.LittleEndian.Uint64(b[i : i+8])
	}
	return sum
}
