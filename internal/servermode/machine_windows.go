//go:build windows

package servermode

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// FreeBytes is the space free to the caller on dir's volume. dir need not
// exist yet: the nearest existing parent is asked instead, which is the same
// volume.
func FreeBytes(dir string) (uint64, bool) {
	for d := dir; d != ""; {
		if _, err := os.Stat(d); err == nil {
			p, err := windows.UTF16PtrFromString(d)
			if err != nil {
				return 0, false
			}
			var free, total, totalFree uint64
			if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
				return 0, false
			}
			return free, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return 0, false
}

/*
 * GrantUser gives one account full control of dir, inherited by everything
 * that is created inside it from now on (ADR 0092 §7).
 *
 * The switch away from the service is run by an administrator's elevated
 * process, which writes the copy. The per-user folder's inherited permissions
 * already include its owner, so this is normally a no-op in effect — but
 * "normally" is the profile having its default permissions, and it is the
 * person's server that must open this library, not the administrator's. So it
 * is stated rather than assumed, and done before the copy so every file
 * inherits it as it is written.
 *
 * sid is the person's, passed in by the unelevated app: an elevated process
 * started with another administrator's credentials is that other account.
 */
func GrantUser(dir, sid string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	who, err := windows.StringToSid(sid)
	if err != nil {
		return fmt.Errorf("read the account %q: %w", sid, err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the permissions on %s: %w", dir, err)
	}
	current, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read the permissions on %s: %w", dir, err)
	}
	merged, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(who),
		},
	}}, current)
	if err != nil {
		return fmt.Errorf("add you to the permissions on %s: %w", dir, err)
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION, nil, nil, merged, nil); err != nil {
		return fmt.Errorf("set the permissions on %s: %w", dir, err)
	}
	return nil
}
