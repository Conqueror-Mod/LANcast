package api

import (
	"net/http"

	"lancast/internal/config"
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
//
// `?max_height=` and `?max_bitrate=` are the quality ceiling, and they only
// ever *narrow*. That direction is the whole safety property: a ceiling can
// force an encode that would not otherwise have happened, but it can never
// talk the server into direct-playing something this client cannot decode.
// Which is why they are applied after WithCapabilities rather than folded into
// it — capabilities widen, ceilings narrow, and a single knob that did both
// would let a request argue its way past the codec check.
func clientProfile(r *http.Request) probe.Profile {
	q := r.URL.Query()
	base := probe.ProfileByName(q.Get("profile"))
	p := probe.WithCapabilities(base, probe.ParseCapabilities(q.Get("can")))

	// A named profile may carry its own ceiling. The lower of the two wins, so
	// asking for 1080p on a profile capped at 720p does not raise it.
	if h := queryIntDefault(r, "max_height", 0); h > 0 {
		if p.MaxHeight == 0 || h < p.MaxHeight {
			p.MaxHeight = h
		}
	}
	// In bits per second, matching Profile. Kilobits would be friendlier to
	// type and is the unit nothing else here uses; one unit for a quantity is
	// worth more than a shorter query string.
	if b := queryIntDefault(r, "max_bitrate", 0); b > 0 {
		if p.MaxVideoBitRate == 0 || int64(b) < p.MaxVideoBitRate {
			p.MaxVideoBitRate = int64(b)
		}
	}
	return p
}

/*
 * withServerCeiling narrows a profile by the limit an administrator set.
 *
 * Applied *after* the client's own ceiling and by the same rule — the lower
 * wins, and neither can raise the other. So the two compose without either
 * needing to know about the other: a client asking for 480p on a server capped
 * at 1080p still gets 480p, and a client asking for Original gets 1080p.
 *
 * Separate from clientProfile, which is pure and takes only a request, because
 * this needs the settings. Keeping the request-parsing testable without a
 * server is worth one more function.
 */
func withServerCeiling(p probe.Profile, ceiling config.QualityRung) probe.Profile {
	if ceiling.Height > 0 && (p.MaxHeight == 0 || ceiling.Height < p.MaxHeight) {
		p.MaxHeight = ceiling.Height
	}
	if ceiling.Bitrate > 0 && (p.MaxVideoBitRate == 0 || ceiling.Bitrate < p.MaxVideoBitRate) {
		p.MaxVideoBitRate = ceiling.Bitrate
	}
	return p
}

/*
 * profileFor is what every handler should use: the caller's profile, narrowed
 * by the server's own ceiling.
 *
 * A method rather than each handler composing the two itself. There are five
 * call sites, and the failure of the composed version is silent — a handler
 * that called clientProfile directly would serve at full quality with no error
 * anywhere, and the setting would look like it did nothing on that one path.
 * One function means a new call site gets the ceiling by default rather than by
 * remembering.
 */
func (s *Server) profileFor(r *http.Request) probe.Profile {
	return withServerCeiling(clientProfile(r), config.QualityByID(s.settings.Get().MaxQuality))
}
