// Package mpv drives an embedded libmpv for the desktop client (ADR 0067).
//
// It is split the way probe is: what the player is told and how its events are
// read are pure and tested here with no DLL present; loading the library and
// calling into it is a thin Windows-only layer.
package mpv

// Option is one mpv option, set before initialisation.
type Option struct {
	Name, Value string
	/*
	 * IfPresent marks an option that may not exist in the library at all.
	 *
	 * LANcast's own build (ADR 0069) leaves out mpv's scripting, and the
	 * options implemented *by* scripts go with it: there is no `ytdl` when
	 * there is no Lua to run ytdl_hook, and no `osc` without the on-screen
	 * controller script. Refusing to start on those would be refusing the
	 * build for not having the thing the option exists to switch off.
	 *
	 * Every other option stays mandatory. An unknown name is a typo, and a
	 * typo in this list is a privacy setting that silently did nothing.
	 */
	IfPresent bool
}

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
		{Name: "config", Value: "no"},
		{Name: "load-scripts", Value: "no"},
		{Name: "ytdl", Value: "no", IfPresent: true},
		{Name: "input-default-bindings", Value: "no"},
		{Name: "input-vo-keyboard", Value: "no"},
		{Name: "input-cursor", Value: "no"},
		{Name: "osc", Value: "no", IfPresent: true},
		{Name: "osd-level", Value: "0"},
		{Name: "keep-open", Value: "yes"},
		{Name: "idle", Value: "yes"},
		{Name: "force-window", Value: "no"},
		// Named, not chosen at random by mpv's probing order: d3d11 is what
		// the Phase 0 spike proved composites under the overlay.
		{Name: "gpu-api", Value: "d3d11"},
		{Name: "hwdec", Value: "auto-safe"},
		// Subtitles are the page's to draw (web/src/playback/nativeTracks.ts):
		// one renderer for both backends, so a track chosen in the player is
		// the only one on screen rather than mpv adding the file's default.
		{Name: "sid", Value: "no"},
		{Name: "wid", Value: uitoa(wid)},
	}
	if logFile != "" {
		opts = append(opts, Option{Name: "log-file", Value: logFile})
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
