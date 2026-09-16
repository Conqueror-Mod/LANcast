package api

import (
	"net/http/httptest"
	"testing"

	"lancast/internal/config"
	"lancast/internal/probe"
)

/*
 * The ceiling an administrator sets for the whole server.
 *
 * What matters is the *direction*: a ceiling narrows and never widens. A
 * version that raised what a client asked for would be a server overriding a
 * laptop on hotel wifi to push it 20 Mbps, which is the opposite of the feature
 * and would look like the quality selector being ignored.
 *
 * Tested against withServerCeiling directly rather than through a handler,
 * because the rule is arithmetic and the handler adds nothing to it — the same
 * reasoning probe's decision table follows.
 */

func req(t *testing.T, query string) *probe.Profile {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/x?"+query, nil)
	p := clientProfile(r)
	return &p
}

func TestNoCeilingChangesNothing(t *testing.T) {
	// The default, and the state every existing server is in. It must behave
	// exactly as it did before this setting existed.
	asked := req(t, "max_height=720&max_bitrate=2000000")
	got := withServerCeiling(*asked, config.QualityByID(""))
	if got.MaxHeight != 720 || got.MaxVideoBitRate != 2_000_000 {
		t.Errorf("got %dp/%d, want the client's own ceiling untouched",
			got.MaxHeight, got.MaxVideoBitRate)
	}
}

func TestTheServerCeilingNarrowsAClientAskingForMore(t *testing.T) {
	// A client asking for Original — no ceiling at all — on a capped server.
	asked := req(t, "")
	got := withServerCeiling(*asked, config.QualityByID("720p4"))
	if got.MaxHeight != 720 {
		t.Errorf("height = %d, want 720", got.MaxHeight)
	}
	if got.MaxVideoBitRate != 4_000_000 {
		t.Errorf("bitrate = %d, want 4 Mbps", got.MaxVideoBitRate)
	}
}

func TestTheServerCeilingNeverRaisesWhatAClientAskedFor(t *testing.T) {
	/*
	 * The direction that matters. A laptop on hotel wifi asking for 480p must
	 * still get 480p on a server capped at 1080p — the ceiling is a limit, not
	 * a target, and raising it would read as the quality selector being
	 * ignored.
	 */
	asked := req(t, "max_height=480&max_bitrate=1500000")
	got := withServerCeiling(*asked, config.QualityByID("1080p20"))
	if got.MaxHeight != 480 {
		t.Errorf("height = %d; the server raised what the client asked for", got.MaxHeight)
	}
	if got.MaxVideoBitRate != 1_500_000 {
		t.Errorf("bitrate = %d; the server raised what the client asked for", got.MaxVideoBitRate)
	}
}

func TestTheLowerOfEachHalfWins(t *testing.T) {
	/*
	 * The two halves are compared independently, which is the only answer that
	 * is a ceiling in both dimensions. A client wanting 1080p at 2 Mbps on a
	 * server capped at 720p at 4 Mbps gets 720p at 2 Mbps — each limit honoured
	 * rather than one rung beating the other wholesale.
	 */
	asked := req(t, "max_height=1080&max_bitrate=2000000")
	got := withServerCeiling(*asked, config.QualityByID("720p4"))
	if got.MaxHeight != 720 {
		t.Errorf("height = %d, want the server's 720", got.MaxHeight)
	}
	if got.MaxVideoBitRate != 2_000_000 {
		t.Errorf("bitrate = %d, want the client's 2 Mbps", got.MaxVideoBitRate)
	}
}

func TestAnUnknownRungIsNoLimitRatherThanAnError(t *testing.T) {
	/*
	 * A hand-edited config file with a typo must not stop the server serving.
	 * The API refuses an unknown id on the way in, which is where a mistake can
	 * still be pointed at; by the time it is being applied to a stream there is
	 * nobody to tell.
	 */
	asked := req(t, "")
	got := withServerCeiling(*asked, config.QualityByID("1080p999"))
	if got.MaxHeight != 0 || got.MaxVideoBitRate != 0 {
		t.Errorf("an unknown rung imposed %dp/%d", got.MaxHeight, got.MaxVideoBitRate)
	}
}

/*
 * The setting at the boundary.
 */

func TestTheRungsAreServed(t *testing.T) {
	// Served rather than known by the client, for the reason the certification
	// countries are: the server owns what it will allow.
	h := newHarness(t)
	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)

	rungs, ok := body["quality_rungs"].([]any)
	if !ok || len(rungs) == 0 {
		t.Fatalf("quality_rungs = %#v, want a non-empty ladder", body["quality_rungs"])
	}
	if got := body["max_quality"]; got != "" {
		t.Errorf("max_quality = %#v, want no limit by default", got)
	}
}

func TestAnUnknownRungIsRefused(t *testing.T) {
	// A ceiling that silently does nothing is one somebody sets before going
	// away for a week believing their uplink is protected.
	h := newHarness(t)
	for _, id := range []string{"1080p", "720", "potato", "ORIGINAL"} {
		wantError(t, h.do(t, "PUT", "/api/settings",
			map[string]any{"max_quality": id}), 400, "bad_request")
	}
}

func TestSettingAndClearingTheCeiling(t *testing.T) {
	h := newHarness(t)
	h.do(t, "PUT", "/api/settings", map[string]any{"max_quality": "720p4"})

	var body map[string]any
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if body["max_quality"] != "720p4" {
		t.Fatalf("max_quality = %#v", body["max_quality"])
	}

	// Empty is the way back to no limit, and is a valid value rather than a
	// missing one.
	h.do(t, "PUT", "/api/settings", map[string]any{"max_quality": ""})
	decode(t, h.do(t, "GET", "/api/settings", nil), &body)
	if body["max_quality"] != "" {
		t.Errorf("max_quality = %#v, want it cleared", body["max_quality"])
	}
}
