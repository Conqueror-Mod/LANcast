package store

import (
	"context"
	"path/filepath"
	"testing"
)

func auditStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// The claim the whole table shape rests on: an event outlives the account that
// caused it. A foreign key to users would either cascade the evidence away or
// block the deletion, and "who deleted this library" is asked precisely when
// the account is gone (ADR 0026).
func TestAuditEventOutlivesItsActor(t *testing.T) {
	st := auditStore(t)
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "u1", "mallory", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendAudit(ctx, AuditEvent{
		ActorID: u.ID, ActorName: u.Name, Action: "library.delete",
		TargetKind: "library", TargetID: "3",
		Summary: `Removed library "Films" (1226 items) — files left on disk`,
	}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	events, total, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(events) != 1 {
		t.Fatalf("event did not survive the account: total=%d len=%d", total, len(events))
	}
	if events[0].ActorName != "mallory" {
		t.Errorf("actor_name = %q, want %q — the name must be frozen, not joined",
			events[0].ActorName, "mallory")
	}
	if events[0].Summary == "" {
		t.Error("summary is empty; it must stay readable without the rows it names")
	}
}

// Newest first, with ties broken on id. Without the tie-break, two events in the
// same second have no stable order and a paged reader can see one twice or miss
// one entirely.
func TestAuditOrderingIsStableWithinASecond(t *testing.T) {
	st := auditStore(t)
	ctx := context.Background()

	for _, a := range []string{"first", "second", "third"} {
		if err := st.AppendAudit(ctx, AuditEvent{
			ActorID: "u1", ActorName: "chris", Action: "item.edit",
			Summary: a, At: 1000, // identical timestamps on purpose
		}); err != nil {
			t.Fatal(err)
		}
	}

	events, _, err := st.ListAudit(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[0].Summary != "third" || events[2].Summary != "first" {
		t.Errorf("order = %q, %q, %q; want newest first",
			events[0].Summary, events[1].Summary, events[2].Summary)
	}

	// Paging must not repeat or skip across the tie.
	page1, total, _ := st.ListAudit(ctx, AuditFilter{Limit: 2, Offset: 0})
	page2, _, _ := st.ListAudit(ctx, AuditFilter{Limit: 2, Offset: 2})
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	seen := map[string]bool{}
	for _, e := range append(page1, page2...) {
		if seen[e.Summary] {
			t.Fatalf("%q appeared on two pages", e.Summary)
		}
		seen[e.Summary] = true
	}
	if len(seen) != 3 {
		t.Errorf("paging saw %d of 3 events", len(seen))
	}
}

func TestAuditFilters(t *testing.T) {
	st := auditStore(t)
	ctx := context.Background()

	add := func(actor, action string) {
		t.Helper()
		if err := st.AppendAudit(ctx, AuditEvent{
			ActorID: actor, ActorName: actor, Action: action, Summary: actor + " did " + action,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("chris", "library.delete")
	add("chris", "item.match")
	add("sam", "item.match")

	byAction, total, _ := st.ListAudit(ctx, AuditFilter{Action: "item.match"})
	if total != 2 || len(byAction) != 2 {
		t.Errorf("action filter: total=%d len=%d, want 2 and 2", total, len(byAction))
	}
	byActor, total, _ := st.ListAudit(ctx, AuditFilter{ActorID: "chris"})
	if total != 2 || len(byActor) != 2 {
		t.Errorf("actor filter: total=%d len=%d, want 2 and 2", total, len(byActor))
	}
	both, total, _ := st.ListAudit(ctx, AuditFilter{Action: "item.match", ActorID: "sam"})
	if total != 1 || len(both) != 1 {
		t.Fatalf("combined filter: total=%d len=%d, want 1 and 1", total, len(both))
	}

	actions, err := st.AuditActions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[0] != "item.match" || actions[1] != "library.delete" {
		t.Errorf("actions = %v, want the two distinct ones sorted", actions)
	}
}

// An event with no action or no summary is not an audit record, it is a row
// that will read as a blank line to whoever needs it most.
func TestAuditRejectsEmptyEvent(t *testing.T) {
	st := auditStore(t)
	ctx := context.Background()

	if err := st.AppendAudit(ctx, AuditEvent{ActorID: "u1", Summary: "no action"}); err == nil {
		t.Error("an event with no action was accepted")
	}
	if err := st.AppendAudit(ctx, AuditEvent{ActorID: "u1", Action: "item.edit"}); err == nil {
		t.Error("an event with no summary was accepted")
	}
}

// An empty log returns an empty array rather than null, so a client has one
// shape to render.
func TestAuditEmptyIsAnArray(t *testing.T) {
	st := auditStore(t)
	events, total, err := st.ListAudit(context.Background(), AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if events == nil {
		t.Error("events is nil; want an empty slice")
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
}
