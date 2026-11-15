//go:build windows

package mediatools

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ServiceGuard is the guard a service applies to its tools: owner and DACL,
// read from the system.
func ServiceGuard() Guard { return NewACLGuard(systemReader{}) }

// systemReader reads a path's owner and DACL with GetNamedSecurityInfo.
type systemReader struct{}

const (
	aceAllowed              = 0 // ACCESS_ALLOWED_ACE_TYPE
	aceDenied               = 1 // ACCESS_DENIED_ACE_TYPE
	aceDeniedObject         = 6
	aceDeniedCallback       = 10
	aceDeniedCallbackObject = 12
)

func (systemReader) Read(path string) (Descriptor, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Descriptor{}, nil
	}
	if err != nil {
		return Descriptor{}, fmt.Errorf("read %s: %w", path, err)
	}
	d := Descriptor{Exists: true}
	// A junction reports ModeIrregular rather than ModeSymlink in Go 1.23+,
	// so the reparse-point attribute is the test that sees both.
	if attrs, ok := info.Sys().(*syscall.Win32FileAttributeData); ok &&
		attrs.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		d.Link = true
	} else if info.Mode()&os.ModeSymlink != 0 {
		d.Link = true
	}

	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return Descriptor{}, fmt.Errorf("read the permissions on %s: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return Descriptor{}, fmt.Errorf("read the owner of %s: %w", path, err)
	}
	d.Owner = owner.String()

	dacl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || (err == nil && dacl == nil) {
		d.NullDACL = true
		return d, nil
	}
	if err != nil {
		return Descriptor{}, fmt.Errorf("read the permissions on %s: %w", path, err)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return Descriptor{}, fmt.Errorf("read the permissions on %s: %w", path, err)
		}
		e := ACE{InheritOnly: ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0}
		switch ace.Header.AceType {
		case aceAllowed, aceDenied:
			// Both share ACCESS_ALLOWED_ACE's layout: header, mask, SID.
			e.Allow = ace.Header.AceType == aceAllowed
			e.Mask = uint32(ace.Mask)
			e.SID = (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		case aceDeniedObject, aceDeniedCallback, aceDeniedCallbackObject:
			continue // takes rights away; cannot make anything writable
		default:
			e.Allow, e.Unknown = true, true
		}
		d.ACEs = append(d.ACEs, e)
	}
	return d, nil
}

// Names are looked up only for a reason a person will read: a lookup can
// wait on a domain controller, and the PATH filter reads dozens of entries.
func init() { lookupName = accountName }

func accountName(sid string) string {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return ""
	}
	account, domain, _, err := s.LookupAccount("")
	if err != nil || account == "" {
		return ""
	}
	if domain != "" {
		return domain + `\` + account
	}
	return account
}

/*
 * protectedSDDL is what Protect writes: Administrators as owner, and a
 * protected DACL giving SYSTEM and Administrators full control and Users read
 * and execute, inherited by everything inside.
 *
 * Protected (P) is the point. The managed tools folder sits in
 * C:\ProgramData\LANcast, which inherits ProgramData's grant letting every user
 * add files and folders; inheriting that is exactly how a tools folder made by
 * the service could be filled by anybody.
 */
const protectedSDDL = "O:BAD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)"

// Protect creates dir if needed and gives it protectedSDDL's owner and DACL.
// Needs SYSTEM or an elevated administrator: anyone else cannot take a folder's
// ownership, which is the property being bought.
func Protect(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	sd, err := windows.SecurityDescriptorFromString(protectedSDDL)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|
			windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect %s (this needs an administrator): %w", dir, err)
	}
	return nil
}

// ownByAdministrators makes Administrators the owner of one file. A copy made
// by an elevated administrator is usually owned by Administrators already,
// but that is a token default, and the guard refuses a file its user owns.
func ownByAdministrators(path string) error {
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION, admins, nil, nil, nil); err != nil {
		return fmt.Errorf("give %s to Administrators: %w", path, err)
	}
	return nil
}
