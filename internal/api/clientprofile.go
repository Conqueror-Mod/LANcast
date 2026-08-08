package api

import (
	"net/http"

	"lancast/internal/probe"
)

// clientProfile resolves what the caller can play: a named profile, widened by
// whatever extra codecs it claims.
//
// **One function, because two would drift.** The playback endpoint decides how
// a file will be delivered and the stream endpoint decides again when it is
// asked for; if those two read the request differently, a client is told
// "direct play" and then served a transcode, or asks for a transcode of a file
// the server now thinks needs none and gets a 409. Resolving it once here is
// what keeps the answer to "how will this play" the same in both places
// (docs/client-capabilities-plan.md).
//
// `?can=` is additive over the named profile and never subtracts, so a request
// that omits it behaves exactly as it did before this existed.
func clientProfile(r *http.Request) probe.Profile {
	q := r.URL.Query()
	base := probe.ProfileByName(q.Get("profile"))
	return probe.WithCapabilities(base, probe.ParseCapabilities(q.Get("can")))
}
