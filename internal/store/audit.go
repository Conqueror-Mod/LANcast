package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AuditEvent is one recorded act (ADR 0026).
//
// It is deliberately self-contained: actor_name and summary are stored already
// resolved, so an event stays readable after the account that caused it and the
// row it names are both gone. That is not denormalisation for speed — it is the
// difference between an audit log and a set of dangling references.
type AuditEvent struct {
	ID         int64  `json:"id"`
	At         int64  `json:"at"`
	ActorID    string `json:"actor_id"`
	ActorName  string `json:"actor_name"`
	Action     string `json:"action"`
	TargetKind string `json:"target_kind,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Summary    string `json:"summary"`
	Detail     string `json:"detail,omitempty"`
}

// AppendAudit records an event. At is stamped here when the caller leaves it
// zero, so callers cannot disagree about what "now" means.
//
// Callers must not fail a request because this failed: the mutation being
// recorded has already happened, and refusing it after the fact is worse than
// losing the record (ADR 0026).
func (s *Store) AppendAudit(ctx context.Context, e AuditEvent) error {
	if e.Action == "" || e.Summary == "" {
		return fmt.Errorf("audit: action and summary are required")
	}
	if e.At == 0 {
		e.At = time.Now().Unix()
	}
	if e.ActorName == "" {
		e.ActorName = e.ActorID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_event
			(at, actor_id, actor_name, action, target_kind, target_id, summary, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.ActorID, e.ActorName, e.Action,
		nullEmpty(e.TargetKind), nullEmpty(e.TargetID), e.Summary, nullEmpty(e.Detail))
	if err != nil {
		return fmt.Errorf("append audit: %w", err)
	}
	return nil
}

// AuditFilter narrows a listing. Empty fields do not filter.
type AuditFilter struct {
	Action  string
	ActorID string
	Limit   int
	Offset  int
}

// ListAudit returns events newest first, with the total matching the filter so
// a caller can page without guessing when it has reached the end.
//
// Ordering ties break on id: two events in the same second still have a stable
// order, and without that a paged reader can see a row twice or miss one.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, int, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var where []string
	var args []any
	if f.Action != "" {
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}
	if f.ActorID != "" {
		where = append(where, "actor_id = ?")
		args = append(args, f.ActorID)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_event`+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("list audit: count: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, at, actor_id, actor_name, action,
		       COALESCE(target_kind, ''), COALESCE(target_id, ''),
		       summary, COALESCE(detail, '')
		FROM audit_event`+clause+`
		ORDER BY at DESC, id DESC
		LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.At, &e.ActorID, &e.ActorName, &e.Action,
			&e.TargetKind, &e.TargetID, &e.Summary, &e.Detail); err != nil {
			return nil, 0, fmt.Errorf("list audit: %w", err)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// AuditActions lists the distinct actions present, so a client can offer a
// filter built from what actually happened rather than from a hardcoded list
// that drifts from the handlers.
func (s *Store) AuditActions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT action FROM audit_event ORDER BY action`)
	if err != nil {
		return nil, fmt.Errorf("audit actions: %w", err)
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, fmt.Errorf("audit actions: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
