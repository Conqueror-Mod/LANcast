package api

import "net/http"

/*
 * Live TV is off unless the `live_tv` setting turns it on (docs/v1-plan.md §8,
 * decision 6 as changed 2026-11-09).
 *
 * It is parked work, and 1.0 does not offer it, so an installed server keeps
 * every line of it and answers none of it: the channel and guide routes, the
 * channel sources, and the guide's own fetch. Nothing is deleted while it is
 * off, so turning it on brings back what was there.
 *
 * A server built without settings (the unit tests' bare servers) counts as on,
 * as it does for music matching: those tests are about the routes, not about
 * the switch, which has tests of its own.
 */
func (s *Server) liveTVOn() bool {
	return s.settings == nil || s.settings.Get().LiveTV
}

// liveTV wraps a live TV route: `409 live_tv_off` while the switch is off.
func (s *Server) liveTV(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.liveTVOn() {
			writeError(w, http.StatusConflict, "live_tv_off",
				"Live TV is turned off on this server.")
			return
		}
		h(w, r)
	}
}
