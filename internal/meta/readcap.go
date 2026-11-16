package meta

import (
	"fmt"
	"io"
)

// MaxResponseBytes bounds one provider response (R08-8). TMDB's largest answer,
// a film with credits, keywords and release dates appended, is well under a
// megabyte; MusicBrainz caps at 4 MiB and TheAudioDB at 1 MiB for the same
// reason.
const MaxResponseBytes = 8 << 20

// ReadCapped reads all of r, or fails once it passes limit bytes. A captive
// portal or a misbehaving proxy answering 200 with an endless body otherwise
// grows memory until the client's timeout. It fails rather than truncating,
// so the answer is "too large", not a confusing JSON error.
func ReadCapped(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response larger than %d bytes", limit)
	}
	return body, nil
}
