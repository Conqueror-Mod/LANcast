package config

/*
 * The ceiling an administrator sets for the whole server.
 *
 * Distinct from the quality a client picks for itself, which lives in that
 * client's own storage and is a fact about the screen — how much bandwidth
 * there is between here and it. This is a fact about the *server*: what it is
 * willing to spend a core on, and what it is willing to push down a domestic
 * uplink.
 *
 * The two meet in clientProfile, where ceilings only ever narrow and the lower
 * of the two wins. So a client asking for Original on a server capped at 1080p
 * gets 1080p, and a client asking for 480p on the same server gets 480p — the
 * server's ceiling is a limit, not a target, and it can never raise what a
 * client asked for.
 *
 * The rungs live here rather than in the client for the reason the
 * certification countries do: the server owns what it will allow, and a client
 * carrying its own copy would eventually offer a rung the server does not
 * honour. The client's *own* ladder in playback/prefs.ts is a different control
 * with a different owner and is deliberately left alone.
 */

// QualityRung is one ceiling an administrator can choose.
type QualityRung struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Height is the tallest picture allowed, 0 for no limit.
	Height int `json:"height"`
	// Bitrate is bits per second, matching probe.Profile. Kilobits would be
	// friendlier to type and is the unit nothing else here uses; one unit for a
	// quantity is worth more than a shorter number.
	Bitrate int64 `json:"bitrate"`
}

/*
 * QualityRungs is the ladder, unlimited first.
 *
 * Unlimited is the default and stays the default, because this is a *LAN*
 * server: the ordinary case is a gigabit link to the next room, where any rung
 * below Original is a re-encode that makes the picture worse and the machine
 * hotter to solve a problem nobody has. The rungs exist for the case that does
 * — a server reachable from outside the house.
 *
 * Each pairs a resolution with a bitrate rather than offering them separately,
 * the same reasoning the client's ladder gives: two independent controls make
 * it easy to ask for 1080p at 1 Mbps, which is a worse picture than 480p at
 * 1 Mbps and looks like the server being broken.
 */
var QualityRungs = []QualityRung{
	{ID: "", Label: "No limit", Height: 0, Bitrate: 0},
	{ID: "1080p20", Label: "1080p · 20 Mbps", Height: 1080, Bitrate: 20_000_000},
	{ID: "1080p10", Label: "1080p · 10 Mbps", Height: 1080, Bitrate: 10_000_000},
	{ID: "720p4", Label: "720p · 4 Mbps", Height: 720, Bitrate: 4_000_000},
	{ID: "720p2", Label: "720p · 2 Mbps", Height: 720, Bitrate: 2_000_000},
	{ID: "480p1", Label: "480p · 1.5 Mbps", Height: 480, Bitrate: 1_500_000},
}

// QualityByID resolves a stored ceiling, falling back to no limit.
//
// An unknown id reads as no limit rather than as an error, and that direction
// matters: a hand-edited config file with a typo in it must not stop a server
// serving. The API refuses an unknown id on the way in, which is where a
// mistake can still be pointed at.
func QualityByID(id string) QualityRung {
	for _, r := range QualityRungs {
		if r.ID == id {
			return r
		}
	}
	return QualityRungs[0]
}

// KnownQuality reports whether an id is a rung this server offers.
func KnownQuality(id string) bool {
	for _, r := range QualityRungs {
		if r.ID == id {
			return true
		}
	}
	return false
}
