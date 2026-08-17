package transcode

import (
	"strings"
	"testing"
)

// The real stderr that started this, from a provider list entry whose source had
// gone. Kept verbatim because the URL in it is the thing that must not escape.
const deadChannelStderr = `[in#0 @ 0000021060c78900] Error opening input: Server returned 404 Not Found
Error opening input file https://content.uplynk.com/channel/3324f2467c414329b3b0cc5cd987b6be.m3u8.
Error opening input files: Server returned 404 Not Found`

func TestFailureReasonClassifies(t *testing.T) {
	for _, tt := range []struct {
		name   string
		stderr string
		want   string
	}{
		{"gone", deadChannelStderr, "404"},
		{"unauthorised", "Error opening input: Server returned 401 Unauthorized", "401"},
		{"forbidden", "Server returned 403 Forbidden", "403"},
		{"other status", "Server returned 500 Internal Server Error", "500"},
		{"refused", "tcp://x: Connection refused", "refused the connection"},
		{"timeout", "Connection timed out", "did not respond in time"},
		{"unreachable", "No route to host", "could not be reached"},
		{"garbage", "Invalid data found when processing input", "not a stream"},
		{"silence", "", "produced no video"},
		{"unknown", "something nobody has seen before", "could not be opened"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := FailureReason(tt.stderr)
			if !strings.Contains(got, tt.want) {
				t.Errorf("FailureReason() = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

/*
 * The reason never carries the source URL.
 *
 * ffmpeg writes the full URL into its stderr, and a provider's channel URLs are
 * routinely credentialed — publishing one hands out the subscription. This is
 * the guard that lets the handler pass raw stderr in without auditing every
 * branch above for what it echoes.
 */
func TestFailureReasonNeverEchoesTheInput(t *testing.T) {
	got := FailureReason(deadChannelStderr)

	for _, secret := range []string{"http", "uplynk", ".m3u8", "3324f2467c414329b3b0cc5cd987b6be"} {
		if strings.Contains(got, secret) {
			t.Errorf("FailureReason leaked %q: %s", secret, got)
		}
	}

	// A blanket check for the general case: no branch may return its argument.
	if strings.Contains(got, "Error opening input") {
		t.Errorf("FailureReason echoed ffmpeg's text verbatim: %s", got)
	}
}
