package mediatools

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

/*
 * Which directories may supply the tools to a service (R09-1).
 *
 * The service runs as LocalSystem on Windows, so whatever ffmpeg.exe it
 * starts runs as SYSTEM. Before this guard it ran the one in whatever folder
 * it had recorded or guessed: a winget install under the user's AppData, which
 * any unelevated process of that user can replace, or `C:\ffmpeg\bin`, which
 * any local account can create. Either was a way to SYSTEM that skipped UAC.
 *
 * So, as the service, a directory supplies tools only when nobody but
 * SYSTEM, Administrators and TrustedInstaller can change what is in it — read
 * from its owner and DACL on every start, not trusted from install time, since
 * a folder's permissions can change after it was recorded. Its ancestors must
 * not let anybody else rename it away and put another in its place, and the two
 * binaries must be protected themselves: a file planted while the folder was
 * still open keeps its planter as owner after the folder is closed.
 *
 * The rules are here, apart from the system calls, so they are tested against
 * descriptors a test writes rather than ACLs it would need to be an
 * administrator to build. guard_windows.go and guard_unix.go only read.
 */

// Guard decides whether a directory, and a binary in it, may be run by this
// process. A nil Guard accepts everything: a server started by a person runs
// tools with that person's own rights, and there is no boundary to defend.
type Guard interface {
	// CheckDir checks a directory and the folders above it.
	CheckDir(dir string) error
	// CheckFile checks one file inside a directory CheckDir accepted.
	CheckFile(path string) error
}

// Refusal is a directory that held the tools and was not used, and why. It is
// logged and reported in the server's status, because the alternative is a
// silent "ffmpeg not found" beside an ffmpeg the person can see.
type Refusal struct {
	Dir    string `json:"directory"`
	Reason string `json:"reason"`
}

// The well-known accounts that may own or write a tools directory.
const (
	sidSystem           = "S-1-5-18"
	sidAdministrators   = "S-1-5-32-544"
	sidTrustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
)

func trustedSID(sid string) bool {
	switch sid {
	case sidSystem, sidAdministrators, sidTrustedInstaller:
		return true
	}
	return false
}

// Access rights, from winnt.h. Defined here rather than taken from x/sys so
// the rules build and are tested on every platform.
const (
	rightWriteData    = 0x00000002 // a file's contents; on a folder, add a file
	rightAppendData   = 0x00000004 // on a folder, add a subfolder
	rightDeleteChild  = 0x00000040
	rightDelete       = 0x00010000
	rightWriteDAC     = 0x00040000
	rightWriteOwner   = 0x00080000
	rightGenericAll   = 0x10000000
	rightGenericWrite = 0x40000000
)

const (
	// dirWrite is anything that lets a holder put a binary in the folder, take
	// one out, or give themselves the right to.
	dirWrite = rightWriteData | rightAppendData | rightDeleteChild | rightDelete |
		rightWriteDAC | rightWriteOwner | rightGenericAll | rightGenericWrite
	// fileWrite is anything that lets a holder change or replace the binary.
	fileWrite = rightWriteData | rightAppendData | rightDelete |
		rightWriteDAC | rightWriteOwner | rightGenericAll | rightGenericWrite
	// ancestorWrite is anything that lets a holder move the folder below away
	// and make another of the same name: rename a child, be renamed, or
	// rewrite the permissions to allow either. Adding a file or a folder to an
	// ancestor is not on the list; ProgramData lets every user do that, and it
	// moves nothing that is already there.
	ancestorWrite = rightDeleteChild | rightDelete | rightWriteDAC | rightWriteOwner | rightGenericAll
)

// ACE is one entry of a DACL, as much of it as the rules need.
type ACE struct {
	SID  string
	Name string // for the reason; the SID when it cannot be looked up
	Mask uint32
	// Allow is false for a deny entry, which can only take rights away and so
	// is ignored. A rule that misread a deny would refuse, never accept.
	Allow bool
	// InheritOnly entries apply to what is created inside, not to the object.
	InheritOnly bool
	// Unknown is an allow entry of a kind the reader does not decode (an
	// object or callback ACE). Refused, since it cannot be shown harmless.
	Unknown bool
}

// Descriptor is what the rules read about one path.
type Descriptor struct {
	Exists    bool
	Link      bool // a symbolic link or junction
	Owner     string
	OwnerName string
	NullDACL  bool // no DACL at all: everyone has every right
	ACEs      []ACE
}

// SecurityReader reads a path's descriptor. The Windows one asks the system;
// tests hand in a map.
type SecurityReader interface {
	Read(path string) (Descriptor, error)
}

// aclGuard applies the rules to whatever its reader returns.
type aclGuard struct{ r SecurityReader }

// NewACLGuard is a Guard over r. Exported for the Windows test and the
// service wiring; most callers want ServiceGuard.
func NewACLGuard(r SecurityReader) Guard { return aclGuard{r} }

func (g aclGuard) CheckDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%q is not an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	d, err := g.r.Read(dir)
	if err != nil {
		return err
	}
	if !d.Exists {
		return fmt.Errorf("%s does not exist", dir)
	}
	if err := g.judge(dir, d, dirWrite, "the folder"); err != nil {
		return err
	}
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		a, err := g.r.Read(p)
		if err != nil {
			return err
		}
		if a.Exists {
			if err := g.judge(p, a, ancestorWrite, "the folder above it, "+p+","); err != nil {
				return err
			}
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

func (g aclGuard) CheckFile(path string) error {
	d, err := g.r.Read(path)
	if err != nil {
		return err
	}
	if !d.Exists {
		return fmt.Errorf("%s does not exist", path)
	}
	return g.judge(path, d, fileWrite, filepath.Base(path))
}

// judge applies one rule set to one descriptor. what names the object in the
// reason, which is read by a person in Settings.
func (g aclGuard) judge(path string, d Descriptor, mask uint32, what string) error {
	if d.Link {
		return fmt.Errorf("%s is a link; LANcast runs tools only from the folder they really live in", what)
	}
	if !trustedSID(d.Owner) {
		return fmt.Errorf("%s is owned by %s, not by Administrators or SYSTEM", what, display(d.OwnerName, d.Owner))
	}
	if d.NullDACL {
		return fmt.Errorf("%s has no access list, so everyone can change it", what)
	}
	for _, e := range d.ACEs {
		if !e.Allow || e.InheritOnly || trustedSID(e.SID) {
			continue
		}
		if e.Unknown {
			return fmt.Errorf("%s has a permission entry for %s that LANcast cannot read", what, display(e.Name, e.SID))
		}
		if e.Mask&mask != 0 {
			return fmt.Errorf("%s can be changed by %s", what, display(e.Name, e.SID))
		}
	}
	return nil
}

// lookupName turns a SID into DOMAIN\name; set on Windows.
var lookupName = func(string) string { return "" }

func display(name, sid string) string {
	if name != "" {
		return name
	}
	if n := lookupName(sid); n != "" {
		return n
	}
	if sid == "" {
		return "an unknown account"
	}
	return sid
}

/*
 * modeGuard is the same idea for Unix permissions.
 *
 * A systemd unit may run LANcast as its own account or as root; either way a
 * binary that another account can replace is a way into this one. A folder or
 * binary is refused when it is owned by anybody but root or this process's own
 * user, or when group or others may write it. Ancestors may be writable by
 * others only with the sticky bit, which is what stops /tmp's other users
 * renaming what is not theirs.
 *
 * Links are followed here, unlike on Windows: /bin is a link to /usr/bin on a
 * merged-/usr system and a snap's ffmpeg is a link into /snap, and the target
 * is checked where it really lives.
 */
type modeGuard struct {
	st   Statter
	self uint32
}

// FileMode is what the Unix rules read about one path.
type FileMode struct {
	Exists bool
	UID    uint32
	Mode   fs.FileMode // permission bits plus ModeSticky
}

// Statter reads a path's mode, and resolves links. Tests hand in a map.
type Statter interface {
	Stat(path string) (FileMode, error)
	Resolve(path string) (string, error)
}

// NewModeGuard is a Guard over st for a process running as uid self.
func NewModeGuard(st Statter, self uint32) Guard { return modeGuard{st, self} }

func (g modeGuard) CheckDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%q is not an absolute path", dir)
	}
	real, err := g.st.Resolve(filepath.Clean(dir))
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	m, err := g.st.Stat(real)
	if err != nil {
		return err
	}
	if !m.Exists {
		return fmt.Errorf("%s does not exist", dir)
	}
	if err := g.judge(m, false, "the folder "+real); err != nil {
		return err
	}
	for p := filepath.Dir(real); ; p = filepath.Dir(p) {
		a, err := g.st.Stat(p)
		if err != nil {
			return err
		}
		if a.Exists {
			if err := g.judge(a, true, "the folder above it, "+p+","); err != nil {
				return err
			}
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

func (g modeGuard) CheckFile(path string) error {
	real, err := g.st.Resolve(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if filepath.Dir(real) != filepath.Dir(filepath.Clean(path)) {
		// A link out of the checked folder: the folder it points into has to
		// pass as well, or the check above proved nothing about this file.
		if err := g.CheckDir(filepath.Dir(real)); err != nil {
			return fmt.Errorf("%s links to %s: %w", filepath.Base(path), real, err)
		}
	}
	m, err := g.st.Stat(real)
	if err != nil {
		return err
	}
	if !m.Exists {
		return fmt.Errorf("%s does not exist", path)
	}
	return g.judge(m, false, filepath.Base(path))
}

func (g modeGuard) judge(m FileMode, ancestor bool, what string) error {
	if m.UID != 0 && m.UID != g.self {
		return fmt.Errorf("%s is owned by uid %d, not by root or the account LANcast runs as", what, m.UID)
	}
	w := m.Mode.Perm() & 0o022
	if w == 0 {
		return nil
	}
	if ancestor && m.Mode&fs.ModeSticky != 0 {
		return nil
	}
	who := "its group"
	if w&0o002 != 0 {
		who = "every account"
	}
	return fmt.Errorf("%s can be written by %s", what, who)
}

// pathEntries splits a PATH value, dropping empty entries.
func pathEntries(path string) []string {
	var out []string
	for _, p := range filepath.SplitList(path) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
