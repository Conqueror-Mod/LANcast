package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"lancast/internal/store"
)

// audit records a deliberate act (ADR 0026).
//
// It is called from handlers, after authorisation has succeeded and the
// mutation has returned without error, so the log records what happened rather
// than what was attempted. The store cannot do this itself: its methods take a
// context and typed arguments, never a session, so it cannot see the actor.
//
// A failed write is logged and swallowed. The mutation has already happened and
// already returned; refusing it after the fact would turn a full disk into a
// denial of the user's own deletions. That is a real weakening and it is the
// stated trade in the ADR.
//
// The request context is deliberately not used for the write: a client that
// disconnects the moment its delete returns must not also cancel the record of
// it.
func (s *Server) audit(r *http.Request, action, targetKind, targetID, summary string, detail any) {
	ev := store.AuditEvent{
		ActorID:    s.userID(r),
		ActorName:  s.actorName(r),
		Action:     action,
		TargetKind: targetKind,
		TargetID:   targetID,
		Summary:    summary,
	}
	if detail != nil {
		if b, err := json.Marshal(detail); err == nil {
			ev.Detail = string(b)
		}
	}
	if err := s.st.AppendAudit(context.WithoutCancel(r.Context()), ev); err != nil {
		s.log.Error("audit write failed", "action", action, "summary", summary, "error", err)
	}
}

// actorName is the display name to freeze into the event. It falls back to the
// id rather than to "unknown", so an event is always attributable to something.
func (s *Server) actorName(r *http.Request) string {
	if sess, ok := sessionFromContext(r); ok {
		if u, err := s.st.UserByID(r.Context(), sess.UserID); err == nil && u != nil {
			return u.Name
		}
		return sess.UserID
	}
	// No session means the unconfigured loopback state, where the owner has
	// full access before the first account exists. That is a real actor and it
	// deserves an honest name rather than a blank.
	return "local owner"
}

// auditID renders an int64 target for the log's TEXT target column.
func auditID(id int64) string { return strconv.FormatInt(id, 10) }

// listAudit serves the audit log, newest first. Admin only: it names filesystem
// paths, library roots and account changes, which is operator information for
// the same reason GET /api/logs is.
func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.AuditFilter{
		Action:  q.Get("action"),
		ActorID: q.Get("actor"),
		Limit:   100,
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"limit must be a positive whole number")
			return
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad_request",
				"offset must be zero or a positive whole number")
			return
		}
		f.Offset = n
	}

	events, total, err := s.st.ListAudit(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err, "list audit")
		return
	}
	actions, err := s.st.AuditActions(r.Context())
	if err != nil {
		s.writeInternal(w, err, "audit actions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		// The filter list comes from what actually happened rather than from a
		// hardcoded set, which would drift from the handlers the moment one is
		// added.
		"actions": actions,
	})
}
