//go:build windows

package singleton

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// globalDACL lets any process on the machine *see* the name, and nothing more.
//
// SYNCHRONIZE|MUTANT_QUERY_STATE is enough to open the mutex and therefore to
// answer "is a server already running"; it does not permit deleting it or
// changing its security. Full control stays with SYSTEM and the administrators
// group, which is who creates it.
//
// This is the whole point of the fix: a service creating a Global mutex under
// the default DACL produces an object a desktop user cannot open, so the
// cross-session check it exists for silently stops working.
const globalDACL = "D:(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x00100001;;;WD)"

// acquire creates a named mutex. Windows keeps the name alive for as long as any
// handle to it is open, so a second process asking for the same name is told it
// already exists — that is the whole guard.
//
// The Global namespace is tried first so the check spans sessions: the server
// runs as a service in session 0 while a double-click happens in the user's
// session, and a session-local name would never see it.
//
// The subtlety that broke this: CreateMutex answers ERROR_ACCESS_DENIED both
// when the object exists and cannot be opened *and* when the caller lacks the
// privilege to create a Global object at all. Treating that as "cannot create,
// fall back to a local name" made the guard fail open — a desktop launch never
// saw the service's server and cheerfully started a second one. Treating it as
// "already running" would be worse: a standard user with no privilege could
// never start LANcast at all.
//
// So the existence question is asked with OpenMutex, which needs no privilege
// and whose error distinguishes the two: ERROR_FILE_NOT_FOUND means genuinely
// absent, ERROR_ACCESS_DENIED means present and not ours to open.
func acquire(name string) (Release, bool, error) {
	global := `Global\` + name

	switch globalState(global) {
	case nameHeld:
		// Someone already has it, in this session or another.
		return func() {}, false, nil
	case nameAbsent:
		if release, held, err := createMutex(global, true); err == nil {
			return release, held, nil
		}
		// No privilege to create a Global object. Fall through to the
		// session-local name: a weaker guard is better than refusing to start.
	}
	return createMutex(name, false)
}

// nameState is what the Global namespace says about a name.
type nameState int

const (
	// nameAbsent: no such object; it is ours to create.
	nameAbsent nameState = iota
	// nameHeld: the object exists, so another instance is running.
	nameHeld
	// nameUnknown: the question could not be answered. The caller falls back
	// rather than guessing, because both wrong answers are bad — refusing to
	// start, or starting a duplicate.
	nameUnknown
)

func globalState(name string) nameState {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nameUnknown
	}
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, p)
	if err == nil {
		windows.CloseHandle(h)
		return nameHeld
	}
	return classifyOpenError(err)
}

// classifyOpenError maps OpenMutex's failure onto what it says about existence.
//
// Access denied is the interesting one: it can only be returned for an object
// that exists, because a name that is absent fails the lookup before any
// security check. That is precisely the case a service creates for a desktop
// user, and reading it as "absent" is what let two servers run at once.
func classifyOpenError(err error) nameState {
	switch err {
	case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND:
		return nameAbsent
	case windows.ERROR_ACCESS_DENIED:
		return nameHeld
	default:
		return nameUnknown
	}
}

// createMutex takes the name. When global is set the object is created with a
// DACL other sessions can open, so the next process to ask gets a truthful
// answer instead of access denied.
func createMutex(name string, global bool) (Release, bool, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return func() {}, false, err
	}

	var sa *windows.SecurityAttributes
	if global {
		if built, err := globalSecurityAttributes(); err == nil {
			sa = built
		}
		// A descriptor that will not build is not worth failing over: the
		// mutex is still created, just with the default DACL, which is the
		// behaviour this replaced rather than something new.
	}

	h, err := windows.CreateMutex(sa, false, p)
	// ERROR_ALREADY_EXISTS is returned alongside a valid handle: the mutex was
	// opened, not created, which means another process is holding the name.
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return func() {}, false, nil
	}
	if err != nil {
		return func() {}, false, err
	}
	return func() { windows.CloseHandle(h) }, true, nil
}

func globalSecurityAttributes() (*windows.SecurityAttributes, error) {
	sd, err := windows.SecurityDescriptorFromString(globalDACL)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	return sa, nil
}
