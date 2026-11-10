package api

import (
	"net/http"

	"lancast/internal/picture"
	"lancast/internal/retro/saves"
)

/*
 * Every request body has a ceiling, before any handler or gate sees it.
 *
 * Handlers decode JSON straight from r.Body, and most never said how much
 * they would read. Four of them answer before sign-in (sign-in, its second
 * factor, passkey sign-in, and first-run setup), so anyone who could reach
 * the port could make the server hold a body of any size in memory. The
 * sign-in rate limit did not help: it counts attempts after the body is read
 * (docs/hardening-plan.md, item 1).
 *
 * The ceiling sits in front of the router rather than in each handler, so a
 * new handler is capped without having to remember to be. A handler may still
 * set a smaller limit of its own; the smaller one wins.
 */
const defaultBodyLimit = 1 << 20 // 1 MiB: the largest JSON any route takes is a few hundred KiB

/*
 * largeBodies are the routes that take a file, by their mux pattern, each with
 * its own ceiling. Kept equal to (or just above) what the handler itself
 * accepts, so the two cannot disagree about what is too large.
 * TestLargeBodiesAreRoutes keeps this list honest against the router.
 */
var largeBodies = map[string]int64{
	"PUT /api/profile/picture":         picture.MaxBytes + 1,
	"PUT /api/items/{id}/saves/{slot}": saves.MaxBytes + 1,
	"POST /api/report":                 reportBodyLimit,
	"POST /api/plugins":                maxBundleBytes,
}

// limitBodies applies the ceiling for the route a request is going to.
func limitBodies(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			next.ServeHTTP(w, r)
			return
		}
		limit := int64(defaultBodyLimit)
		if _, pattern := mux.Handler(r); pattern != "" {
			if n, ok := largeBodies[pattern]; ok {
				limit = n
			}
		}
		// A body that says up front it is too large is refused without
		// reading a byte of it. One that does not say, or lies, is cut off
		// at the ceiling by MaxBytesReader, and the handler's decode fails.
		if r.ContentLength > limit {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large",
				"The request body is larger than this route accepts.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}
