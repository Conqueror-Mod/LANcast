package host

import (
	"bytes"
	"encoding/binary"
)

/*
 * Achievement progress rides on the end of a save state (ADR 0112).
 *
 * rcheevos counts things across frames (hits, "collect ten coins"), and a
 * state that brings back the game's memory without that count would let an
 * achievement unlock early or never. So the count is kept with the state, as
 * RetroArch keeps it: the core's own bytes, then rcheevos', then their length
 * and a tag. The core never sees the trailer; it is cut off before any state
 * reaches Unserialize.
 *
 * A state with no trailer (one saved before this, or with achievements off)
 * is the core's bytes as they are. The browser player never meets one: its
 * states name a web core, which the desktop refuses, and the other way round.
 */

var progressTag = [4]byte{'L', 'C', 'R', 'A'}

const progressFooter = 8 // uint32 length, then the tag

// withProgress appends progress to a core's state, or returns it as it is.
func withProgress(state, progress []byte) []byte {
	if len(progress) == 0 {
		return state
	}
	out := make([]byte, 0, len(state)+len(progress)+progressFooter)
	out = append(out, state...)
	out = append(out, progress...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(progress)))
	return append(out, progressTag[:]...)
}

// splitProgress separates a stored state into the core's bytes and the
// progress kept with them, which is nil when there is none.
func splitProgress(data []byte) (state, progress []byte) {
	if len(data) < progressFooter || !bytes.Equal(data[len(data)-4:], progressTag[:]) {
		return data, nil
	}
	n := int(binary.LittleEndian.Uint32(data[len(data)-progressFooter:]))
	end := len(data) - progressFooter
	if n <= 0 || n > end {
		return data, nil
	}
	return data[:end-n], data[end-n : end]
}
