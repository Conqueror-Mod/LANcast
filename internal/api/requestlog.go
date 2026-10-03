package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"lancast/internal/auth"
	"lancast/internal/probe"
	"lancast/internal/store"
)

/*
 * What the server refused, and what it decided (docs/logging-plan.md, Phase 1).
 *
 * Both used to be silent. A person turned away by the origin check, or by a
 * session the browser had dropped, left no trace; and a film that played
 * directly, which is most of a library, left none either, so "what did the
 * server decide for this, and why?" could only be answered for conversions.
 *
 * Both go through s.quiet, keyed so that one cause from one client is one
 * line per window with a count, never a line per request. A tab with a dead
 * cookie asking twenty routes a minute is the case to design for.
 */

// logRefusal records a refused request. The key is the reason and the client,
// deliberately not the route: one cause from one place is one story, and the
// route on the line is the first one it hit.
func (s *Server) logRefusal(r *http.Request, level slog.Level, reason string, args ...any) {
	client := auth.ClientKey(r)
	args = append([]any{"reason", reason, "route", r.Method + " " + r.URL.Path, "client", client}, args...)
	s.quiet.Log(s.log, level, "refused|"+reason+"|"+client, "request refused", args...)
}

/*
 * logUnauthenticated says which of three different things a 401 was.
 *
 * They read the same to the client ("sign in to continue"), on purpose, and
 * they are not the same to whoever reads the log:
 *   - a Bearer key the server does not recognise is a script with a revoked
 *     or mistyped key;
 *   - a session cookie the server does not recognise is somebody who *was*
 *     signed in, which is the one worth a Warn (2 October: a cookie the
 *     browser had kept past its server session, or the other way about);
 *   - no credential at all is an ordinary visitor before signing in, and is
 *     Info.
 * Nothing about the credential itself is logged.
 */
func (s *Server) logUnauthenticated(r *http.Request) {
	switch {
	case r.Header.Get("Authorization") != "":
		s.logRefusal(r, slog.LevelWarn, "API key not recognised")
	case hasSessionCookie(r):
		s.logRefusal(r, slog.LevelWarn, "session expired or unknown")
	default:
		s.logRefusal(r, slog.LevelInfo, "not signed in")
	}
}

func hasSessionCookie(r *http.Request) bool {
	c, err := r.Cookie(auth.CookieName)
	return err == nil && c.Value != ""
}

/*
 * logDecision records what the server decided for a playback, and why.
 *
 * /api/items/{id}/playback is asked once as a play starts (and again on a
 * track or quality change, which is a new decision), never per segment, so
 * this is the one place a play passes through exactly once. Titles go in
 * beside IDs (decided 2026-10-02). The same decision for the same item and
 * account inside the window is a rebuild, not news, and is Debug.
 */
func (s *Server) logDecision(r *http.Request, it *store.Item, profile string, audio int, d probe.Decision) {
	account := ""
	if sess, ok := sessionFromContext(r); ok {
		account = sess.Name
	}
	args := []any{
		"item", it.ID, "title", displayTitle(it), "account", account,
		"method", d.Method, "profile", profile,
	}
	if d.Reason != "" {
		args = append(args, "reason", d.Reason)
	}
	if audio >= 0 {
		args = append(args, "audio", audio)
	}
	if can := r.URL.Query().Get("can"); can != "" {
		args = append(args, "claims", can)
	}
	key := fmt.Sprintf("decision|%d|%s|%s|%s", it.ID, account, d.Method, d.Reason)
	s.quiet.Log(s.log, slog.LevelInfo, key, "playback decided", args...)
}

// displayTitle is how a person would name the item: an episode carries its
// show, because "Pilot" alone names forty things.
func displayTitle(it *store.Item) string {
	if it.Kind == "episode" && it.Series != nil && *it.Series != "" {
		return *it.Series + ": " + it.Title
	}
	return it.Title
}
