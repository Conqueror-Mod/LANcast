//go:build windows

package dataperm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"lancast/internal/mediatools"
)

/*
 * What an ordinary account can do with a protected data directory, tried
 * rather than read.
 *
 * Run unelevated, the test process is the unprivileged local account the
 * findings were about: Administrators is in its token for deny only, so the
 * DACL's Users entry is all it holds. It can apply the DACL to a folder it owns
 * (owners may always rewrite a DACL), and then every access below is the
 * system's answer, not the test's reading of an ACL. Elevated — as CI runs —
 * Administrators would grant everything, so the elevated test below reads the
 * result back through the #1096 guard instead.
 */
func TestAnOrdinaryAccountCanStatTheLibraryAndReadOnlyTheCertificate(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("elevated, Administrators can do everything; TestProtectReadsBackAsTheGuardRequires covers this run")
	}
	dir := t.TempDir()
	write(t, dir, "lancast.db", "the library")
	write(t, dir, "secrets.key", "secret")
	write(t, dir, filepath.Join("tls", "cert.pem"), "public")
	write(t, dir, filepath.Join("tls", "key.pem"), "private")
	write(t, dir, filepath.Join("backups", "lancast-backup-20990101-000000.db"), "old")
	t.Cleanup(func() { giveBack(t, dir) })

	r, err := protect(dir, false, mediatools.ServiceGuard())
	if err != nil {
		t.Fatal(err)
	}
	if !r.Applied {
		t.Fatal("protect reported nothing applied on a folder that had ProgramData-like defaults")
	}
	if err := ShareCert(filepath.Join(dir, "tls", "cert.pem")); err != nil {
		t.Fatal(err)
	}

	// The two reads the desktop app makes (sharedDataDir, certpin.SPKI).
	if _, err := os.Stat(filepath.Join(dir, "lancast.db")); err != nil {
		t.Errorf("the window can no longer tell the service's library exists: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "tls", "cert.pem")); err != nil || string(b) != "public" {
		t.Errorf("the window can no longer read the certificate it pins: %q, %v", b, err)
	}
	if _, err := os.ReadDir(dir); err != nil {
		t.Errorf("listing the top folder: %v", err)
	}

	// And nothing else.
	for _, name := range []string{"lancast.db", "secrets.key", filepath.Join("tls", "key.pem"),
		filepath.Join("backups", "lancast-backup-20990101-000000.db")} {
		if _, err := os.ReadFile(filepath.Join(dir, name)); !denied(err) {
			t.Errorf("an ordinary account read %s: %v", name, err)
		}
	}
	if _, err := os.ReadDir(filepath.Join(dir, "backups")); !denied(err) {
		t.Errorf("an ordinary account listed backups: %v", err)
	}
	for _, name := range []string{"planted.exe", filepath.Join("backups", "restore-pending.json"),
		filepath.Join("tls", "planted")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); !denied(err) {
			t.Errorf("an ordinary account created %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "update-staged"), 0o755); !denied(err) {
		t.Errorf("an ordinary account created a folder: %v", err)
	}

	// A second start finds it done and leaves the tree alone.
	again, err := protect(dir, false, mediatools.ServiceGuard())
	if err != nil {
		t.Fatal(err)
	}
	if again.Applied {
		t.Error("a folder already protected was rewritten, which walks the whole library on every start")
	}
}

// The entry an unprivileged account added to its own file must not survive.
// At the top level, because unelevated the walk cannot read the folders the
// DACL has just closed to it; the service walks as SYSTEM, and the elevated
// test below plants one a folder down.
func TestProtectRemovesAnEntryAddedInside(t *testing.T) {
	if windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("elevated; the access check below would pass through Administrators")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "lancast-faces.exe")
	write(t, dir, "lancast-faces.exe", "planted")
	grantUsers(t, p)
	t.Cleanup(func() { giveBack(t, dir) })

	r, err := protect(dir, false, mediatools.ServiceGuard())
	if err != nil {
		t.Fatal(err)
	}
	if r.Reclaimed == 0 {
		t.Error("nothing was reclaimed")
	}
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if s := sd.String(); strings.Contains(s, ";;;BU)") {
		t.Errorf("the Users entry survived: %s", s)
	}
	if err := os.WriteFile(p, []byte("again"), 0o644); !denied(err) {
		t.Errorf("an ordinary account could still rewrite the file: %v", err)
	}
}

// The real thing, owner included: needs an administrator, which CI is.
func TestProtectReadsBackAsTheGuardRequires(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("Protect takes ownership of a folder, which needs an elevated process")
	}
	base := filepath.Join(os.Getenv("ProgramData"), "lancast-dataperm-test")
	_ = os.RemoveAll(base)
	t.Cleanup(func() { os.RemoveAll(base) })
	write(t, base, "lancast.db", "the library")
	write(t, base, filepath.Join("tls", "cert.pem"), "public")
	write(t, base, filepath.Join("tls", "key.pem"), "private")
	planted := filepath.Join(base, "faces", "lancast-faces.exe")
	write(t, base, filepath.Join("faces", "lancast-faces.exe"), "planted")
	grantUsers(t, planted)

	r, err := Protect(base)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Applied || r.Reclaimed == 0 {
		t.Errorf("report = %+v, want applied and the planted entry reclaimed", r)
	}
	if err := ShareCert(filepath.Join(base, "tls", "cert.pem")); err != nil {
		t.Fatal(err)
	}

	g := mediatools.ServiceGuard()
	if err := g.CheckDir(base); err != nil {
		t.Errorf("the guard refuses the protected folder: %v", err)
	}
	for _, p := range []string{"lancast.db", "tls", filepath.Join("tls", "key.pem"), filepath.Join("tls", "cert.pem")} {
		if err := g.CheckFile(filepath.Join(base, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}

	root := sddl(t, base, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if !strings.HasPrefix(root, "O:BAD:P") {
		t.Errorf("root = %s, want Administrators as owner and a protected DACL", root)
	}
	// Users: listing the top folder, and the certificate. Nothing else.
	for p, want := range map[string]bool{
		"lancast.db":                     false,
		"tls":                            false,
		filepath.Join("tls", "key.pem"):  false,
		filepath.Join("tls", "cert.pem"): true,
	} {
		got := strings.Contains(sddl(t, filepath.Join(base, p), windows.DACL_SECURITY_INFORMATION), ";;;BU)")
		if got != want {
			t.Errorf("%s: Users entry = %v, want %v", p, got, want)
		}
	}

	again, err := Protect(base)
	if err != nil {
		t.Fatal(err)
	}
	if again.Applied {
		t.Error("a folder already protected was rewritten")
	}
}

func sddl(t *testing.T, p string, info windows.SECURITY_INFORMATION) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, info)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func denied(err error) bool { return errors.Is(err, os.ErrPermission) }

// grantUsers adds an explicit Users:(M) entry to p.
func grantUsers(t *testing.T, p string) {
	t.Helper()
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	cur, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: 0x1301bf, // modify
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users),
		},
	}}, cur)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

// giveBack hands the tree back to the test's own account so TempDir can
// remove it: as owner it may rewrite the DACL, and that propagates.
func giveBack(t *testing.T, dir string) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Log(err)
		return
	}
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;OICI;FA;;;" + tu.User.Sid.String() + ")")
	if err != nil {
		t.Log(err)
		return
	}
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil); err != nil {
		t.Log(err)
	}
	// Entries reclaimed above lost nothing of their owner, so a reset of each
	// to inherited-only is enough for the propagation to reach them.
	_ = filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err == nil && p != dir {
			_ = resetEntry(p, false)
		}
		return nil
	})
}
