package host

/*
 * The rewind buffer: the game's state, taken every few frames, newest last.
 *
 * Bounded twice, by count and by bytes, because a state's size is the core's
 * business: an NES state is a few kilobytes and an N64 one is many megabytes.
 * The byte budget is what keeps rewind from becoming the largest thing in the
 * process; past it, the oldest states go first, so rewind always reaches back
 * as far as the budget allows and no further.
 */
type rewindRing struct {
	states   [][]byte
	bytes    int
	maxCount int
	maxBytes int
}

func newRewindRing(maxCount, maxBytes int) *rewindRing {
	return &rewindRing{maxCount: maxCount, maxBytes: maxBytes}
}

// push keeps a copy of state as the newest, dropping the oldest to stay
// within both bounds. A single state larger than the whole budget is not
// kept at all.
func (r *rewindRing) push(state []byte) {
	if len(state) == 0 || len(state) > r.maxBytes {
		return
	}
	c := make([]byte, len(state))
	copy(c, state)
	r.states = append(r.states, c)
	r.bytes += len(c)
	for len(r.states) > r.maxCount || r.bytes > r.maxBytes {
		r.bytes -= len(r.states[0])
		r.states[0] = nil
		r.states = r.states[1:]
	}
}

// pop takes the newest state, or reports there is none left.
func (r *rewindRing) pop() ([]byte, bool) {
	n := len(r.states)
	if n == 0 {
		return nil, false
	}
	s := r.states[n-1]
	r.states[n-1] = nil
	r.states = r.states[:n-1]
	r.bytes -= len(s)
	return s, true
}

func (r *rewindRing) clear() {
	r.states = nil
	r.bytes = 0
}

func (r *rewindRing) len() int { return len(r.states) }
