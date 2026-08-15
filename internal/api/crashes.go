package api

import (
	"net/http"
	"runtime/debug"
)

/*
 * Recovering a panic, and admitting to it.
 *
 * Without this, a panic unwinds through net/http, which recovers it, logs a
 * stack to standard error and closes the connection without a response. The
 * client sees a network error; the operator sees nothing at all unless they
 * happen to be reading the log. The fault that is hardest to report is the one
 * that leaves no trace on the screen where it happened.
 *
 * Two things change. The caller gets a 500 with the same error shape as every
 * other failure — so the client renders a message rather than a dead fetch —
 * and the panic becomes a numbered crash report in Settings, which is what
 * turns "it crashed once last week" into a stack trace with a route on it.
 *
 * The route pattern is recorded, not the URL. `GET /api/items/{id}` is what
 * somebody fixes; `/api/items/4193` invites the belief that item 4193 is
 * special. Go 1.22's r.Pattern gives the first for free.
 */
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// http.ErrAbortHandler is the documented way to drop a connection
			// on purpose. Recording it would fill the crash list with things
			// that are not crashes.
			if v == http.ErrAbortHandler {
				panic(v)
			}

			stack := debug.Stack()
			where := r.Pattern
			if where == "" {
				where = r.Method + " " + r.URL.Path
			}
			s.log.Error("panic recovered", "where", where, "value", v, "stack", string(stack))

			if s.crashes != nil {
				if _, err := s.crashes.Record(where, v, stack); err != nil {
					s.log.Error("write crash report", "error", err)
				}
			}

			// Headers may already be out — a panic halfway through streaming a
			// file cannot be turned into a JSON error, and trying produces a
			// corrupt body plus a "superfluous WriteHeader" line. The report is
			// written either way, which is the part that had to survive.
			defer func() { _ = recover() }()
			writeError(w, http.StatusInternalServerError, "internal",
				"the server hit an unexpected fault; a crash report was saved")
		}()
		next.ServeHTTP(w, r)
	})
}

// listCrashes returns the saved reports, newest first. Admin only: a stack
// trace names source paths, and the route it names may be one a member cannot
// reach.
func (s *Server) listCrashes(w http.ResponseWriter, r *http.Request) {
	if s.crashes == nil {
		writeJSON(w, http.StatusOK, map[string]any{"crashes": []any{}})
		return
	}
	reports, err := s.crashes.List()
	if err != nil {
		s.writeInternal(w, err, "list crash reports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"crashes": reports})
}

func (s *Server) clearCrashes(w http.ResponseWriter, r *http.Request) {
	if s.crashes != nil {
		if err := s.crashes.Clear(); err != nil {
			s.writeInternal(w, err, "clear crash reports")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
