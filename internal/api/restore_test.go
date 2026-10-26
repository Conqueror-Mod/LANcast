package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lancast/internal/backup"
	"lancast/internal/store"
)

// takeOne takes a backup through the API and returns its name.
func takeOne(t *testing.T, h *harness) string {
	t.Helper()
	resp := h.authed(t, "POST", "/api/backups", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups = %d", resp.StatusCode)
	}
	var f backup.File
	decode(t, resp, &f)
	return f.Name
}

// restartable gives the harness a relaunch that records being asked rather
// than stopping anything, and returns the count of asks.
func restartable(h *harness, err error) *int {
	n := 0
	h.srvAPI.relaunch = func() error {
		n++
		return err
	}
	return &n
}

func TestRestoreRequiresAdmin(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	resp := h.do(t, "POST", "/api/backups/"+backup.NewName(nowish())+"/restore", nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		t.Fatal("restore without a session was accepted")
	}
}

func TestRestoreStagesTakesASafetyBackupAndRestarts(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	asks := restartable(h, nil)
	name := takeOne(t, h)

	resp := h.authed(t, "POST", "/api/backups/"+name+"/restore", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restore = %d, want 202", resp.StatusCode)
	}
	var got struct {
		Staged       backup.Pending `json:"staged"`
		SafetyBackup backup.File    `json:"safety_backup"`
	}
	decode(t, resp, &got)

	if *asks != 1 {
		t.Fatalf("restart asked %d times, want once", *asks)
	}
	if got.Staged.Name != name || got.Staged.RequestedBy != testUser || got.Staged.SafetyBackup != got.SafetyBackup.Name {
		t.Fatalf("staged = %+v, safety = %+v", got.Staged, got.SafetyBackup)
	}
	// The safety backup is an ordinary one taken by hand, so nothing rotates it away.
	listed := backupsOf(t, h)
	var safety *backup.File
	for i := range listed.Backups {
		if listed.Backups[i].Name == got.SafetyBackup.Name {
			safety = &listed.Backups[i]
		}
	}
	if safety == nil || safety.Scheduled || safety.PreMigration || !safety.Restorable {
		t.Fatalf("safety backup in the list = %+v", safety)
	}
	if listed.Staged == nil || listed.Staged.Name != name || !listed.CanRestore {
		t.Fatalf("list says staged=%+v can_restore=%v", listed.Staged, listed.CanRestore)
	}

	// A second press while one waits is refused, not stacked.
	again := h.authed(t, "POST", "/api/backups/"+name+"/restore", nil)
	again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("second restore = %d, want 409", again.StatusCode)
	}

	// And it is audited, into the database being replaced.
	events, _, err := h.st.ListAudit(context.Background(), store.AuditFilter{Action: "backup.restore"})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit = %v, %v", events, err)
	}
}

func TestRestoreRefusedWhereNothingWouldRestart(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	name := takeOne(t, h)

	resp := h.authed(t, "POST", "/api/backups/"+name+"/restore", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("restore = %d, want 412", resp.StatusCode)
	}
	listed := backupsOf(t, h)
	if listed.Staged != nil || listed.CanRestore || len(listed.Backups) != 1 {
		t.Fatalf("a refused restore changed something: %+v", listed)
	}
}

func TestRestoreUnstagesWhenTheRestartCannotStart(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	restartable(h, errors.New("no helper"))
	name := takeOne(t, h)

	resp := h.authed(t, "POST", "/api/backups/"+name+"/restore", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("restore = %d, want 500", resp.StatusCode)
	}
	if backupsOf(t, h).Staged != nil {
		t.Fatal("left staged with no restart coming: it would fire at some later, unrelated start")
	}
}

func TestRestoreRefusesWhatCannotBeRestored(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")
	restartable(h, nil)

	missing := h.authed(t, "POST", "/api/backups/"+backup.NewName(nowish())+"/restore", nil)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("missing = %d, want 404", missing.StatusCode)
	}
	escape := h.authed(t, "POST", "/api/backups/..%2Ftest.db/restore", nil)
	escape.Body.Close()
	if escape.StatusCode == http.StatusAccepted {
		t.Error("an escaping name was accepted")
	}

	bad := backup.NewName(nowish().Add(-time.Hour))
	dir := backup.New(h.dataDir)
	dir.Create()
	if err := os.WriteFile(filepath.Join(dir.Path(), bad), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := h.authed(t, "POST", "/api/backups/"+bad+"/restore", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("unreadable = %d, want 409", resp.StatusCode)
	}
	if backupsOf(t, h).Staged != nil {
		t.Error("a refused restore was staged")
	}
}

func nowish() time.Time { return time.Now() }
