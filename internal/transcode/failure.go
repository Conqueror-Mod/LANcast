package transcode

import (
	"regexp"
	"strings"
)

/*
 * Turning ffmpeg's complaint into something a viewer can act on.
 *
 * A channel list from an IPTV provider carries dead entries — a real one had
 * 1,862 channels, and a source that 404s is ordinary rather than exceptional.
 * When that happens the server knows exactly what went wrong, and the viewer
 * used to see a player that simply failed: the response had already committed
 * to 200, ffmpeg died producing nothing, and the browser reported
 * `DEMUXER_ERROR_COULD_NOT_OPEN`. A dead channel read as a broken application.
 *
 * The messages below are deliberately short and free of jargon, and one rule
 * governs all of them: **ffmpeg's raw stderr never reaches a client.** It
 * contains the upstream URL, and those are routinely credentialed — publishing
 * one hands out the subscription. Only a classification derived from it goes
 * out, never the text itself.
 */

// reHTTPStatus catches ffmpeg's phrasing for an HTTP error from the source.
var reHTTPStatus = regexp.MustCompile(`Server returned (\d{3})`)

/*
 * FailureReason explains, in one sentence a viewer can read, why a stream
 * produced nothing.
 *
 * The fallback is deliberately vague rather than inventive: a wrong specific
 * cause is worse than an honest "could not be opened", because it sends someone
 * to fix the wrong thing. Anything not recognised here is still in the server
 * log in full, which is where a diagnosis belongs.
 */
func FailureReason(stderr string) string {
	switch {
	case stderr == "":
		return "this channel produced no video"

	case reHTTPStatus.MatchString(stderr):
		code := reHTTPStatus.FindStringSubmatch(stderr)[1]
		switch code {
		case "401", "403":
			return "the channel's source refused this server (HTTP " + code +
				") — the subscription or credentials may have expired"
		case "404":
			return "the channel's source is gone (HTTP 404) — the provider's list " +
				"may be out of date"
		default:
			return "the channel's source returned HTTP " + code
		}

	case containsAny(stderr, "Connection refused"):
		return "the channel's source refused the connection"

	case containsAny(stderr, "Connection timed out", "timed out", "Operation timed out"):
		return "the channel's source did not respond in time"

	case containsAny(stderr, "No route to host", "Network is unreachable"):
		return "the channel's source could not be reached from this server"

	case containsAny(stderr, "Invalid data found", "could not find codec parameters"):
		return "the channel's source is not a stream this server can read"

	default:
		return "the channel's source could not be opened"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
