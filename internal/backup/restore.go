package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lancast/internal/store"
)

/*
 * Restoring from Settings: stage it, then restart.
 *
 * A running server cannot restore itself in place. Restoring replaces the
 * database it holds open, and a server swapping the file it is reading is how
 * a restore becomes the incident it was meant to undo (ADR 0058). So the
 * request only *stages* a restore: it writes a marker naming the backup, and
 * the server restarts. The next start applies the marker before it opens the
 * database, with the same RestoreSnapshot the `restore` command uses, so the
 * file is swapped while nothing holds it.
 *
 * Two rules keep this safe without anybody at the machine:
 *
 *   - The marker is removed **before** the restore is attempted. A backup that
 *     makes the restore fail, or the server crash, must not be retried on
 *     every start for ever. One attempt, then the old database stays.
 *   - What happened is written down (restore-result.json), so Settings can say
 *     "restored from X" or "the restore failed, and why" after the restart,
 *     when the request that asked for it is long gone.
 */

const (
	pendingFile = "restore-pending.json"
	resultFile  = "restore-result.json"
)

// ErrNothingStaged reports a call to cancel when no restore is staged.
var ErrNothingStaged = errors.New("no restore is staged")

// Pending is a staged restore: which backup, and who asked.
type Pending struct {
	Name        string `json:"name"`
	RequestedBy string `json:"requested_by"`
	RequestedAt int64  `json:"requested_at"`
	// SafetyBackup is the backup taken of the database being replaced, just
	// before staging, so restoring the wrong one can be undone from Settings.
	SafetyBackup string `json:"safety_backup,omitempty"`
}

// Outcome is what the last staged restore did, read back after the restart.
type Outcome struct {
	Pending
	AppliedAt int64 `json:"applied_at"`
	OK        bool  `json:"ok"`
	// Error is why not, in a sentence, when OK is false.
	Error string `json:"error,omitempty"`
	// SchemaFrom is the revision the backup held; SchemaTo what it holds now,
	// after migrating forward. Equal when nothing was migrated.
	SchemaFrom int `json:"schema_from,omitempty"`
	SchemaTo   int `json:"schema_to,omitempty"`
}

func (d *Dir) pendingPath() string { return filepath.Join(d.path, pendingFile) }
func (d *Dir) resultPath() string  { return filepath.Join(d.path, resultFile) }

/*
 * Stage records a restore for the next start. The name is resolved and the
 * file inspected here, so a request for a backup that cannot be restored is
 * refused now, while somebody is looking, rather than discovered after the
 * restart.
 */
func (d *Dir) Stage(p Pending) error {
	path, err := d.Resolve(p.Name)
	if err != nil {
		return err
	}
	if _, err := store.InspectSnapshot(path); err != nil {
		return err
	}
	if err := d.Create(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// Written to a temporary name and renamed, so a crash mid-write leaves no
	// half a marker for the next start to misread.
	tmp := d.pendingPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("stage restore: %w", err)
	}
	if err := os.Rename(tmp, d.pendingPath()); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("stage restore: %w", err)
	}
	return nil
}

// Unstage removes a staged restore, for when the restart it was waiting on
// could not be started.
func (d *Dir) Unstage() error {
	err := os.Remove(d.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return ErrNothingStaged
	}
	return err
}

/*
 * ApplyPending performs a staged restore, if there is one, onto the database at
 * dbPath. It must run before anything opens that database.
 *
 * Returns nil when nothing was staged. A failed restore is an Outcome with OK
 * false, not an error: the caller's job is to log it and carry on starting
 * with the database that is there, which RestoreSnapshot has put back.
 */
func (d *Dir) ApplyPending(ctx context.Context, dbPath string, now time.Time) *Outcome {
	b, err := os.ReadFile(d.pendingPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	/*
	 * Who wrote the marker, read before it is removed (R10-2).
	 *
	 * As a Windows service the backups folder used to let every local account
	 * add files. A marker and a database of anybody's making then replaced
	 * the library at the next start, and their author signed in as an
	 * administrator. The folder is closed now (internal/dataperm); this does
	 * not depend on it. A file keeps its creator as owner, so a planted
	 * marker or backup is told apart from one the server wrote by asking the
	 * guard who owns it.
	 */
	var refused error
	if d.guard != nil {
		if gerr := d.guard.CheckFile(d.pendingPath()); gerr != nil {
			refused = fmt.Errorf("the staged restore was not written by LANcast, so it was ignored: %w", gerr)
		}
	}
	// Gone before anything else happens: one attempt, never a loop.
	os.Remove(d.pendingPath())

	out := &Outcome{AppliedAt: now.Unix()}
	if err != nil {
		out.Error = "the staged restore could not be read: " + err.Error()
		d.writeResult(out)
		return out
	}
	if err := json.Unmarshal(b, &out.Pending); err != nil && refused == nil {
		out.Error = "the staged restore could not be read: " + err.Error()
		d.writeResult(out)
		return out
	}
	if refused != nil {
		out.Error = refused.Error()
		d.writeResult(out)
		return out
	}

	path, err := d.Resolve(out.Name)
	if err != nil {
		out.Error = err.Error()
		d.writeResult(out)
		return out
	}
	if d.guard != nil {
		if gerr := d.guard.CheckFile(path); gerr != nil {
			out.Error = fmt.Sprintf("%s was not written by LANcast or an administrator, so it was not restored: %v", out.Name, gerr)
			d.writeResult(out)
			return out
		}
	}
	res, err := store.RestoreSnapshot(ctx, dbPath, path)
	if err != nil {
		out.Error = err.Error()
		d.writeResult(out)
		return out
	}
	out.OK = true
	out.SchemaFrom = res.Snapshot.SchemaVersion
	out.SchemaTo = res.SchemaVersionAfter
	d.writeResult(out)
	return out
}

func (d *Dir) writeResult(o *Outcome) {
	b, err := json.Marshal(o)
	if err != nil {
		return
	}
	if d.Create() != nil {
		return
	}
	os.WriteFile(d.resultPath(), b, 0o600)
}

// LastRestore is what the most recent staged restore did, or nil when none
// has run on this data directory.
func (d *Dir) LastRestore() *Outcome {
	b, err := os.ReadFile(d.resultPath())
	if err != nil {
		return nil
	}
	var o Outcome
	if json.Unmarshal(b, &o) != nil {
		return nil
	}
	return &o
}

// Staged is the restore waiting for the next start, or nil.
func (d *Dir) Staged() *Pending {
	b, err := os.ReadFile(d.pendingPath())
	if err != nil {
		return nil
	}
	var p Pending
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return &p
}

/*
 * RecordOutcome writes a staged restore into the audit log of the database it
 * produced (R07-8).
 *
 * The request that asked for it audits into the database being replaced, so
 * after the restart the record survived only in the safety backup, and the
 * log an administrator reads afterwards said nothing about a restore having
 * happened, who asked, or where the previous state went. Called once the
 * store is open, after ApplyPending. A failed restore is recorded too, in the
 * database that stayed, because "somebody tried to put this back and it did
 * not take" belongs in the same history.
 *
 * Who asked is known by name only: the marker carries the name, and the
 * account behind it may not exist in a database from before it was made.
 */
func RecordOutcome(ctx context.Context, st *store.Store, out *Outcome) error {
	if out == nil {
		return nil
	}
	return st.AppendAudit(ctx, outcomeAudit(out))
}

// outcomeAudit is the audit event for a restore's outcome.
func outcomeAudit(out *Outcome) store.AuditEvent {
	ev := store.AuditEvent{
		At:         out.AppliedAt,
		ActorName:  out.RequestedBy,
		Action:     "backup.restore",
		TargetKind: "backup",
		TargetID:   out.Name,
	}
	if ev.ActorName == "" {
		ev.ActorName = "unknown"
	}
	detail := map[string]any{"ok": out.OK}
	if out.SafetyBackup != "" {
		detail["safety_backup"] = out.SafetyBackup
	}
	if out.OK {
		ev.Summary = fmt.Sprintf("Restored the backup %s", out.Name)
		if out.SafetyBackup != "" {
			ev.Summary += fmt.Sprintf(" (the database before it was kept as %s)", out.SafetyBackup)
		}
		detail["schema_from"] = out.SchemaFrom
		detail["schema_to"] = out.SchemaTo
	} else {
		ev.Summary = fmt.Sprintf("A restore of the backup %s did not take, so the database that was there was kept: %s",
			out.Name, out.Error)
		detail["error"] = out.Error
	}
	if b, err := json.Marshal(detail); err == nil {
		ev.Detail = string(b)
	}
	return ev
}
