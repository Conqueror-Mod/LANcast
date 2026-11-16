//go:build windows

package dataperm

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"lancast/internal/mediatools"
)

/*
 * dirSDDL is the data directory's descriptor.
 *
 *   O:BA                 owned by Administrators, so no other account holds
 *                        the owner's implicit right to rewrite the DACL
 *   D:P                  protected: nothing inherited from ProgramData, whose
 *                        grant letting every user add files is the whole fault
 *   (A;OICI;FA;;;SY)     SYSTEM, full control, inherited by everything inside
 *   (A;OICI;FA;;;BA)     Administrators, the same
 *   (A;;0x1200a9;;;BU)   Users may list and traverse this one folder. No OI or
 *                        CI, so nothing inside inherits it: they can see that
 *                        lancast.db exists and cannot open it.
 *
 * It is mediatools.Protect's descriptor (the tools folder, #1096) with the
 * Users entry narrowed from "read everything" to "list the top folder".
 */
const dirSDDL = "O:BAD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x1200a9;;;BU)"

// fileGenericRead is FILE_GENERIC_READ. A generic right written into an ACE
// on an existing file is not mapped by the access check, so the specific
// rights are spelled out.
const fileGenericRead = 0x00120089

// Protect gives dir the descriptor above, unless it has it already. Needs
// SYSTEM or an elevated administrator: setting Administrators as owner is the
// part an ordinary account cannot do.
func Protect(dir string) (Report, error) {
	return protect(dir, true, mediatools.ServiceGuard())
}

// protect is Protect with the owner optional, so a test that is not elevated
// can apply the DACL to a folder it owns and see what an unelevated account
// can then do with it.
func protect(dir string, owner bool, g mediatools.Guard) (Report, error) {
	var r Report
	if !filepath.IsAbs(dir) {
		return r, fmt.Errorf("%q is not an absolute path", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return r, fmt.Errorf("create %s: %w", dir, err)
	}
	same, err := matches(dir, owner)
	if err != nil {
		return r, err
	}
	if same {
		return r, nil
	}
	if err := apply(dir, owner); err != nil {
		return r, err
	}
	r.Applied = true
	r.Reclaimed, r.Links = reclaim(dir, owner, g)
	return r, nil
}

// wantSDDL is the descriptor protect writes, in the form the system reads it
// back in, so the comparison is of like with like.
func wantSDDL(owner bool) (string, error) {
	s := dirSDDL
	if !owner {
		s = s[len("O:BA"):]
	}
	sd, err := windows.SecurityDescriptorFromString(s)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}

func securityInfo(owner bool) windows.SECURITY_INFORMATION {
	if owner {
		return windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION
	}
	return windows.DACL_SECURITY_INFORMATION
}

// matches reports whether dir already has the descriptor. A comparison, not
// an interpretation: anything other than exactly this is rewritten, and the
// rules about what is safe stay in mediatools.
func matches(dir string, owner bool) (bool, error) {
	want, err := wantSDDL(owner)
	if err != nil {
		return false, err
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, securityInfo(owner))
	if err != nil {
		return false, fmt.Errorf("read the permissions on %s: %w", dir, err)
	}
	return sd.String() == want, nil
}

// apply writes the descriptor. SetNamedSecurityInfo re-derives the inherited
// entries of everything below, which is what takes ProgramData's Users grants
// off the files already there.
func apply(dir string, owner bool) error {
	sd, err := windows.SecurityDescriptorFromString(dirSDDL)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	info := securityInfo(owner) | windows.PROTECTED_DACL_SECURITY_INFORMATION
	var o *windows.SID
	if owner {
		if o, _, err = sd.Owner(); err != nil {
			return err
		}
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, info, o, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect %s (this needs an administrator): %w", dir, err)
	}
	return nil
}

/*
 * reclaim takes back what was made while the folder was open.
 *
 * Re-deriving inherited entries is not enough on its own. A file or folder an
 * unprivileged account created stays owned by it, and an owner may always
 * rewrite an object's DACL, so it could open its own folder again; and an
 * entry it added explicitly is not inherited, so the rewrite above leaves it.
 * Each entry the #1096 guard refuses is therefore given to Administrators
 * with only what it inherits. Links are left alone and reported.
 *
 * Only on the start that applies the descriptor: it reads every entry, and
 * once the folder is closed nothing new can need it.
 */
func reclaim(dir string, owner bool, g mediatools.Guard) (n int, links []string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == dir {
			return nil // unreadable: skipped, and reported by whatever needs it
		}
		if g.CheckFile(p) == nil {
			return nil
		}
		if isLink(p) {
			links = append(links, p)
			return nil
		}
		if resetEntry(p, owner) == nil {
			n++
		}
		return nil
	})
	return n, links
}

// isLink is the guard's own test: the reparse-point attribute, which a
// junction carries as well as a symbolic link.
func isLink(p string) bool {
	info, err := os.Lstat(p)
	if err != nil {
		return false
	}
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok &&
		a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return true
	}
	return info.Mode()&os.ModeSymlink != 0
}

// resetEntry gives p to Administrators with an empty, unprotected DACL: what
// it inherits from the protected folder above and nothing of its own.
func resetEntry(p string, owner bool) error {
	sd, err := windows.SecurityDescriptorFromString("O:BAD:")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	info := securityInfo(owner) | windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	var o *windows.SID
	if owner {
		if o, _, err = sd.Owner(); err != nil {
			return err
		}
	}
	return windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, info, o, nil, dacl, nil)
}

// ShareCert lets Users read the server's public certificate, the one file
// inside the data directory the unelevated window must open (certpin.SPKI):
// beyond loopback the window trusts the server by that key, and without it the
// window never loads. key.pem beside it is not touched. Called after the
// certificate is loaded or made, since a new certificate is a new file. A
// missing file is not an error: a loopback-only server has none.
func ShareCert(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	users, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the permissions on %s: %w", path, err)
	}
	cur, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read the permissions on %s: %w", path, err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: fileGenericRead,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(users),
		},
	}}, cur)
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("let Users read %s: %w", path, err)
	}
	return nil
}

// protectedSummary is what LogReport says was done.
const protectedSummary = "data directory protected: SYSTEM and Administrators only, Users may list its top folder"
