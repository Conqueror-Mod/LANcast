package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/backup"
	"lancast/internal/store"
)

// openStore reads a backup file as a database, to assert things about what is
// inside one.
func openStore(path string) (*store.Store, error) { return store.Open(path) }

// backupsOf lists through the API, which is also the assertion that listing
// works after whatever the test just did.
func backupsOf(t *testing.T, h *harness) backupsResponse {
	t.Helper()
	resp := h.authed(t, "GET", "/api/backups", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/backups = %d", resp.StatusCode)
	}
	var got backupsResponse
	decode(t, resp, &got)
	return got
}

func TestBackupsRequireAdmin(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	// Every one of them, rather than a representative sample: a backup is a
	// complete copy of the library including account rows, so an endpoint
	// accidentally left open is a database read for anybody.
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/backups"},
		{"POST", "/api/backups"},
		{"GET", "/api/backups/lancast-backup-20260903-120000.db"},
		{"DELETE", "/api/backups/lancast-backup-20260903-120000.db"},
	} {
		resp := h.do(t, c.method, c.path, nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated ||
			resp.StatusCode == http.StatusNoContent {
			t.Errorf("%s %s without a session = %d, want a refusal", c.method, c.path, resp.StatusCode)
		}
	}
}

// A server nobody has backed up answers with an empty list, not an error and
// not a 404.
func TestListBackupsOnAFreshServer(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	got := backupsOf(t, h)
	if len(got.Backups) != 0 {
		t.Errorf("backups = %v, want none", got.Backups)
	}
	// The folder is shown so a person can get the file off this disk, which is
	// the only thing that makes it a backup.
	if got.Folder == "" {
		t.Error("no folder reported")
	}
	// The client cannot restore and must not pretend it can.
	if !strings.Contains(got.RestoreCommand, "restore") {
		t.Errorf("restore command = %q", got.RestoreCommand)
	}
}

func TestCreateBackupThenListAndDownloadIt(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	resp := h.authed(t, "POST", "/api/backups", nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/backups = %d, want 201", resp.StatusCode)
	}
	var made backup.File
	decode(t, resp, &made)

	if !strings.HasPrefix(made.Name, backup.Prefix) || !strings.HasSuffix(made.Name, backup.Ext) {
		t.Errorf("name = %q", made.Name)
	}
	if made.Bytes <= 0 || !made.Restorable || made.SchemaVersion == 0 {
		t.Errorf("created backup reported as %+v", made)
	}

	listed := backupsOf(t, h)
	if len(listed.Backups) != 1 || listed.Backups[0].Name != made.Name {
		t.Fatalf("list = %+v, want the one just taken", listed.Backups)
	}

	// The download is the point of the feature: a backup on the same disk as
	// the database is not yet protecting anything.
	dl := h.authed(t, "GET", "/api/backups/"+made.Name, nil)
	defer dl.Body.Close()
	if dl.StatusCode != http.StatusOK {
		t.Fatalf("download = %d", dl.StatusCode)
	}
	if cd := dl.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") ||
		!strings.Contains(cd, made.Name) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if n := dl.ContentLength; n != made.Bytes {
		t.Errorf("downloaded %d bytes, want %d", n, made.Bytes)
	}
}

// Two backups in the same second must both succeed. The name is only
// second-resolution and a snapshot takes well under one, so this is what a
// double-clicked button does.
func TestTwoBackupsInTheSameSecond(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	names := map[string]bool{}
	for i := 0; i < 2; i++ {
		resp := h.authed(t, "POST", "/api/backups", nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("backup %d = %d, want 201", i, resp.StatusCode)
		}
		var made backup.File
		decode(t, resp, &made)
		if names[made.Name] {
			t.Fatalf("second backup reused the name %q", made.Name)
		}
		names[made.Name] = true
	}
	if got := backupsOf(t, h); len(got.Backups) != 2 {
		t.Errorf("list holds %d backups, want 2", len(got.Backups))
	}
}

/*
 * The containment boundary, exercised through HTTP rather than only through
 * the package that implements it — this is where a name from outside actually
 * arrives, and a route pattern that decoded a path segment differently than
 * expected would not be caught anywhere else.
 */
func TestBackupNameCannotEscapeTheFolder(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	for _, name := range []string{
		"..%2Flancast.db",
		"..%5Clancast.db",
		"lancast.db",
		"notes.txt",
		"%2Fetc%2Fpasswd",
	} {
		resp := h.authed(t, "GET", "/api/backups/"+name, nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("GET a backup named %q succeeded", name)
		}

		del := h.authed(t, "DELETE", "/api/backups/"+name, nil)
		del.Body.Close()
		if del.StatusCode == http.StatusNoContent {
			t.Errorf("DELETE a backup named %q succeeded", name)
		}
	}

	// And the live database is still there, which is the thing all of that was
	// protecting.
	if _, err := os.Stat(filepath.Join(h.dataDir, "test.db")); err != nil {
		t.Errorf("the live database is gone: %v", err)
	}
}

func TestDownloadingAMissingBackupIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	resp := h.authed(t, "GET", "/api/backups/lancast-backup-20260101-000000.db", nil)
	wantError(t, resp, http.StatusNotFound, "not_found")
}

func TestDeleteBackup(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	resp := h.authed(t, "POST", "/api/backups", nil)
	var made backup.File
	decode(t, resp, &made)

	del := h.authed(t, "DELETE", "/api/backups/"+made.Name, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", del.StatusCode)
	}
	if got := backupsOf(t, h); len(got.Backups) != 0 {
		t.Errorf("list still holds %+v", got.Backups)
	}

	// Deleting it twice is a 404, not a silent success — a client that thinks
	// it deleted something that was not there has been told the wrong thing.
	again := h.authed(t, "DELETE", "/api/backups/"+made.Name, nil)
	wantError(t, again, http.StatusNotFound, "not_found")
}

// Both deliberate acts land in the audit log. Deleting the copy of the library
// is exactly what somebody wants to look up afterwards (ADR 0026).
func TestBackupActionsAreAudited(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	resp := h.authed(t, "POST", "/api/backups", nil)
	var made backup.File
	decode(t, resp, &made)
	h.authed(t, "DELETE", "/api/backups/"+made.Name, nil).Body.Close()

	audit := h.authed(t, "GET", "/api/audit", nil)
	var got struct {
		Events []struct {
			Action   string `json:"action"`
			TargetID string `json:"target_id"`
		} `json:"events"`
	}
	decode(t, audit, &got)

	seen := map[string]bool{}
	for _, e := range got.Events {
		seen[e.Action] = true
		if strings.HasPrefix(e.Action, "backup.") && e.TargetID != made.Name {
			t.Errorf("%s recorded target %q, want %q", e.Action, e.TargetID, made.Name)
		}
	}
	for _, want := range []string{"backup.create", "backup.delete"} {
		if !seen[want] {
			t.Errorf("%s is not in the audit log", want)
		}
	}
}

// Backups taken through the API carry no sessions, which is the property
// ADR 0058's amendment moved to snapshot time — asserted here too because this
// is where a backup a person can download is actually produced.
func TestBackupTakenThroughTheAPIHoldsNoSessions(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery staple")

	resp := h.authed(t, "POST", "/api/backups", nil)
	var made backup.File
	decode(t, resp, &made)

	path := filepath.Join(h.dataDir, "backups", made.Name)
	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	n, err := st.CountSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the downloadable backup carries %d sessions, want 0", n)
	}
}
