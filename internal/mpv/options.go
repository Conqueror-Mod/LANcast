// Package mpv drives an embedded libmpv for the desktop client (ADR 0067).
//
// It is split the way probe is: what the player is told and how its events are
// read are pure and tested here with no DLL present; loading the library and
// calling into it is a thin Windows-only layer.
package mpv

// Option is one mpv option, set before initialisation.
type Option struct{ Name, Value string }

// Options is what every embedded instance starts with, in order.
//
// The first block is the no-phone-home rule from ADR 0067, and it is not a
// preference. mpv left to its defaults reads config and scripts from the
// user's profile and will run yt-dlp on a URL it recognises: each of those is
// code or a network call LANcast did not choose, running inside LANcast's
// window. Removing any of these lines is a principle change, which is why a
// test names every one.
//
// The second block makes it a component rather than a player: no on-screen
// controller (the React chrome is the controller), no keyboard or mouse
// handling of its own (the page owns input), and keep-open so reaching the end
// raises an event the provider decides about, rather than mpv tearing the
// video down first.
func Options(wid uint64, logFile string) []Option {
	opts := []Option{
		{"config", "no"},
		{"load-scripts", "no"},
		{"ytdl", "no"},
		{"input-default-bindings", "no"},
		{"input-vo-keyboard", "no"},
		{"input-cursor", "no"},
		{"osc", "no"},
		{"osd-level", "0"},
		{"keep-open", "yes"},
		{"idle", "yes"},
		{"force-window", "no"},
		// Named, not chosen at random by mpv's probing order: d3d11 is what
		// the Phase 0 spike proved composites under the overlay.
		{"gpu-api", "d3d11"},
		{"hwdec", "auto-safe"},
		// Subtitles are the page's to draw (web/src/playback/nativeTracks.ts):
		// one renderer for both backends, so a track chosen in the player is
		// the only one on screen rather than mpv adding the file's default.
		{"sid", "no"},
		{"wid", uitoa(wid)},
	}
	if logFile != "" {
		opts = append(opts, Option{"log-file", logFile})
	}
	return opts
}

func uitoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
