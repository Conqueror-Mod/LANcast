package transcode

import (
	"context"
	"testing"
)

/*
 * Skipping around an episode does not stop the server playing anything.
 *
 * The reported fault, stated as the thing that was actually observed rather
 * than as the mechanism behind it: play something, skip a few times, and the
 * server answers that it may be converting everything it can and then plays
 * nothing at all.
 *
 * Three seeks over seven seconds started five sessions against a ceiling of
 * three. Each seek asks for a playlist and — because the client falls back to
 * the progressive stream about six hundred milliseconds later — abandons that
 * HLS session where it stands, at a position nothing will ever ask about again.
 *
 * This is the guard for the whole fix and it is deliberately about the ceiling
 * rather than about sessions: what a person meets is a refusal, so a refusal is
 * what is asserted.
 */
func TestSkippingAroundDoesNotFillTheCeiling(t *testing.T) {
	bin := fakeFFmpeg(t, `
d=$(echo "$@" | tr ' ' '\n' | grep index.m3u8 | xargs dirname)
printf '#EXTM3U\n#EXT-X-PLAYLIST-TYPE:EVENT\n' > "$d/index.m3u8"
sleep 30`)
	m := newManager(t, bin)

	const viewer = "u_3f9"
	const episode = int64(37106)

	// Well past the ceiling of three, because the point is that the number of
	// seeks stops mattering — not that a fourth happens to fit.
	for _, at := range []float64{115, 51, 63, 200, 640, 900, 1200} {
		_, err := m.EnsureHLS(context.Background(), episode, viewer,
			Options{Input: "x.mkv", Decision: remux(), StartAt: at})
		if err != nil {
			t.Fatalf("seeking to %.0fs was refused: %v — this is the reported "+
				"fault, where the server stops playing anything at all", at, err)
		}
	}

	// And it left one session behind, not seven: the positions the viewer moved
	// away from are stopped, rather than waited out by the reaper.
	if n := len(m.Sessions()); n != 1 {
		t.Errorf("%d sessions after seven seeks, want 1", n)
	}
	m.StopAll()
}

/*
 * The same seeking by two people at once is still two streams.
 *
 * Stated beside the test above because the cheap way to pass that one is to
 * collapse by item, and the failure that produces is worse than the fault being
 * fixed — one person pressing play would stop another person's episode.
 */
func TestTwoViewersSkippingKeepTheirOwnStreams(t *testing.T) {
	bin := fakeFFmpeg(t, `
d=$(echo "$@" | tr ' ' '\n' | grep index.m3u8 | xargs dirname)
printf '#EXTM3U\n#EXT-X-PLAYLIST-TYPE:EVENT\n' > "$d/index.m3u8"
sleep 30`)
	m := newManager(t, bin)

	const episode = int64(37106)
	for _, v := range []string{"u_3f9", "u_a21"} {
		for _, at := range []float64{115, 400} {
			if _, err := m.EnsureHLS(context.Background(), episode, v,
				Options{Input: "x.mkv", Decision: remux(), StartAt: at}); err != nil {
				t.Fatalf("%s seeking to %.0fs was refused: %v", v, at, err)
			}
		}
	}

	/*
	 * One each, and each viewer's own. A count alone passed while the second
	 * viewer's seek to 400 reused the first viewer's 400 session and left its
	 * own 115 behind: still two, owned by the wrong people (review finding
	 * R09-2).
	 */
	owners := map[string]float64{}
	for _, s := range m.Sessions() {
		owners[s.Owner] = s.StartAt
	}
	if len(m.Sessions()) != 2 || owners["u_3f9"] != 400 || owners["u_a21"] != 400 {
		t.Errorf("sessions by owner = %v from %d sessions, want each viewer holding their own at 400",
			owners, len(m.Sessions()))
	}
	m.StopAll()
}
