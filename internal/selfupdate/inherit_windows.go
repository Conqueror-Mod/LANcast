//go:build windows

package selfupdate

import (
	"golang.org/x/sys/windows"
)

/*
 * inheritFromParent gives path the permissions its folder hands down, and no
 * others.
 *
 * A file renamed into a folder keeps the security descriptor it had where it
 * was. The update is staged in the data directory, which since #1115 is
 * SYSTEM and Administrators only, so every file an update moved into
 * C:\Program Files\LANcast arrived readable by nobody else. The service, as
 * SYSTEM, ran on; the window and the tray, which run the same LANcast.exe as
 * the person signed in, could not start: "Access is denied" (seen on the first
 * in-app update to 0.11.1, from a build that already had #1115).
 *
 * An empty DACL set with UNPROTECTED_DACL_SECURITY_INFORMATION is what
 * `icacls /reset` does: no explicit entries, and the folder's inheritable ones
 * applied afresh. Program Files gives Users read and execute.
 */
func inheritFromParent(path string) error {
	// "D:" is a DACL with no entries; ACLFromEntries panics given none.
	sd, err := windows.SecurityDescriptorFromString("D:")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}

// dacl reads path's DACL as SDDL, so a repair can tell whether it changed
// anything.
func dacl(path string) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}
