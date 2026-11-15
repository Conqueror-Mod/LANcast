//go:build windows

package mediatools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

/*
 * The guard against real ACLs, which the rest of the tests stand in for.
 *
 * A folder made here is the shape R09-1 was found in: under the user's
 * profile, owned by the user, and — made explicit with an entry giving Users
 * modify rights — writable without elevation. The service must refuse it,
 * and say so in words.
 */
func TestServiceGuardRefusesAUserWritableFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffprobe.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	grantUsersModify(t, dir)

	l := &Locator{Guard: ServiceGuard()}
	err := l.check(dir)
	if err == nil {
		t.Fatal("a folder every user can write was accepted as a source of tools for the service")
	}
	msg := err.Error()
	if !strings.Contains(msg, "owned by") && !strings.Contains(msg, "can be changed by") &&
		!strings.Contains(msg, "the folder above it") {
		t.Errorf("the refusal does not say why: %q", msg)
	}
	t.Logf("refused: %s", msg)

	// Owned by the user, it would be refused on owner alone. Read the real
	// DACL and judge it as though Administrators owned the folder, so the
	// decoding of the Users entry is what is being tested.
	d, err := systemReader{}.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Owner = sidAdministrators
	if err := (aclGuard{}).judge(dir, d, dirWrite, "the folder"); err == nil ||
		!strings.Contains(err.Error(), "can be changed by") || !strings.Contains(err.Error(), "Users") {
		t.Errorf("the Users entry was not read as a write grant: %v", err)
	}

	// And the reason survives into what the server reports.
	keepPath(t)
	r := l.Resolve(dir)
	if r.OK && pathKey(r.Dir) == pathKey(dir) {
		t.Fatal("Resolve chose the refused folder")
	}
	if len(r.Refused) == 0 || pathKey(r.Refused[0].Dir) != pathKey(dir) || r.Refused[0].Reason == "" {
		t.Errorf("refusals = %+v, want the folder with its reason", r.Refused)
	}
}

// The other half: a folder Windows itself protects passes, so the rules are
// not refusing everything. System32 is owned by TrustedInstaller, under C:\
// and C:\Windows, which grant users nothing that could move it.
func TestServiceGuardAcceptsSystem32(t *testing.T) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		t.Skip(err)
	}
	g := ServiceGuard()
	if err := g.CheckDir(sys); err != nil {
		t.Fatalf("System32 refused: %v", err)
	}
	if err := g.CheckFile(filepath.Join(sys, "where.exe")); err != nil {
		t.Errorf("System32's where.exe refused: %v", err)
	}
}

// Protect itself needs an administrator, so it runs only when the tests do.
func TestProtectMakesAFolderTheGuardAccepts(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("Protect takes ownership of a folder, which needs an elevated process")
	}
	base := filepath.Join(os.Getenv("ProgramData"), "lancast-protect-test")
	t.Cleanup(func() { os.RemoveAll(base) })
	if err := Protect(base); err != nil {
		t.Fatal(err)
	}
	if err := ServiceGuard().CheckDir(base); err != nil {
		t.Errorf("a folder Protect made was refused: %v", err)
	}
}

// grantUsersModify adds BUILTIN\Users:(OI)(CI)(M) to dir, inherited, the
// grant a careless manual install leaves behind.
func grantUsersModify(t *testing.T, dir string) {
	t.Helper()
	users, err := windows.StringToSid(sidUsers)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	cur, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_READ | windows.GENERIC_WRITE | windows.GENERIC_EXECUTE | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users),
		},
	}}, cur)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}
