//go:build windows

package selfupdate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

/*
 * An update staged in a locked folder arrives in the install folder with the
 * install folder's permissions, not the staging folder's.
 *
 * The fault this is for: the data directory became SYSTEM and Administrators
 * only (#1115), a rename keeps a file's descriptor, and the first in-app update
 * after that left LANcast.exe unreadable by the person signed in, so the window
 * and tray would not start. Reproduced here with two folders whose DACLs differ
 * the same way: "data" grants only this test's account, and "install" also
 * grants Users read and execute, inheritably, as Program Files does.
 */

// me is the SDDL SID of the account running the test.
func me(t *testing.T) string {
	t.Helper()
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return u.User.Sid.String()
}

// lockTo gives dir a protected DACL from sddl, inherited by what is made in it.
func lockTo(t *testing.T, dir, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

// usersMayRead reports whether path's DACL has an allow entry for BUILTIN\Users.
func usersMayRead(t *testing.T, path string) bool {
	t.Helper()
	s, err := dacl(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(s, ";;;BU)")
}

func lockedAndOpen(t *testing.T) (data, install string) {
	t.Helper()
	data, install = t.TempDir(), t.TempDir()
	lockTo(t, data, "D:P(A;OICI;FA;;;"+me(t)+")")
	lockTo(t, install, "D:P(A;OICI;FA;;;"+me(t)+")(A;OICI;0x1200a9;;;BU)")
	return data, install
}

// The bare fault: a rename keeps the locked descriptor.
func TestARenameKeepsTheStagingFoldersPermissions(t *testing.T) {
	data, install := lockedAndOpen(t)
	src := filepath.Join(data, "LANcast.exe")
	if err := os.WriteFile(src, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(install, "LANcast.exe")
	if err := os.Rename(src, dst); err != nil {
		t.Fatal(err)
	}
	if usersMayRead(t, dst) {
		t.Skip("this filesystem re-derived the descriptor on rename; the fault does not reproduce here")
	}
	if err := inheritFromParent(dst); err != nil {
		t.Fatal(err)
	}
	if !usersMayRead(t, dst) {
		t.Error("after inheritFromParent, Users still cannot read the program")
	}
}

// Apply, end to end, from a locked data directory.
func TestApplyGivesTheInstallFoldersPermissions(t *testing.T) {
	data, install := lockedAndOpen(t)
	if err := os.WriteFile(filepath.Join(install, "LANcast.exe"), []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage(t, data, "v0.7.0", map[string][]byte{
		"LANcast.exe":        []byte("new program"),
		"WebView2Loader.dll": []byte("new loader"),
	})
	if _, err := Apply(data, install, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, name := range []string{"LANcast.exe", "WebView2Loader.dll"} {
		if !usersMayRead(t, filepath.Join(install, name)) {
			t.Errorf("%s arrived without the install folder's permissions: Users cannot read it, "+
				"so the window and tray cannot start", name)
		}
	}
}

// An install an earlier Apply locked is mended by RepairPermissions, and an
// install that is already right is reported as unchanged.
func TestRepairPermissionsMendsALockedInstall(t *testing.T) {
	data, install := lockedAndOpen(t)
	for _, name := range []string{"LANcast.exe", "libmpv-2.dll"} {
		src := filepath.Join(data, name)
		if err := os.WriteFile(src, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(src, filepath.Join(install, name)); err != nil {
			t.Fatal(err)
		}
	}
	// Something a person put there is not the repair's business.
	other := filepath.Join(install, "notes.txt")
	if err := os.WriteFile(other, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	repaired, errs := RepairPermissions(install)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	for _, name := range []string{"LANcast.exe", "libmpv-2.dll"} {
		if !usersMayRead(t, filepath.Join(install, name)) {
			t.Errorf("%s is still unreadable by Users after the repair", name)
		}
	}
	if !usersMayRead(t, filepath.Join(install, "LANcast.exe")) || len(repaired) == 0 {
		t.Errorf("repaired = %v, want the locked files named", repaired)
	}

	again, errs := RepairPermissions(install)
	if len(errs) > 0 || len(again) != 0 {
		t.Errorf("a second repair = %v, %v; want nothing to do", again, errs)
	}
}
