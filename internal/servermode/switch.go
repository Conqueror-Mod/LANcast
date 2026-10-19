package servermode

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Service is the installed service, as much of it as a switch drives. The
// Windows implementation is the service control manager; tests use a fake
// that can be told to fail at any step.
type Service interface {
	Installed() bool
	// Stop returns once the service has stopped, not once it was asked to:
	// until then it holds the database open.
	Stop() error
	Start() error
	// SetAutomatic chooses whether Windows starts the service at boot.
	SetAutomatic(on bool) error
	// Install registers the service to start manually. ToService sets it to
	// start at boot only once it has answered.
	Install(addr string) error
	Uninstall() error
}

// Phases a switch reports, in the order they happen.
const (
	PhaseStopping   = "stopping"
	PhaseCopying    = "copying"
	PhaseVerifying  = "verifying"
	PhaseInstalling = "installing"
	PhaseStarting   = "starting"
	PhaseFinishing  = "finishing"
)

// Progress is what a switch says while it runs.
type Progress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done_bytes,omitempty"`
	Total int64  `json:"total_bytes,omitempty"`
}

// Env is everything a switch needs from outside: where the two folders are,
// and the parts that touch the machine.
type Env struct {
	// ServiceDir is the machine-wide folder (service.DefaultDataDir) and
	// UserDir the person's own (config.DefaultDataDir, resolved by the
	// unelevated app, ADR 0092 §2).
	ServiceDir, UserDir string
	// Addr is the listen address the service is installed with.
	Addr    string
	Service Service
	// Verify proves a copied database opens (store.VerifyCopy).
	Verify func(path string) error
	// Answers waits up to within for a server to answer at addr.
	Answers func(addr string, within time.Duration) bool
	// GrantUser gives the person full control of their folder before the
	// library is copied into it (ADR 0092 §7). Nil where there is nothing to
	// grant.
	GrantUser func(dir string) error
	// FreeBytes is the free space on dir's volume; ok=false when unknown.
	FreeBytes func(dir string) (free uint64, ok bool)
	Now       func() time.Time
	Report    func(Progress)
}

// Result is what a finished switch did, in words the confirmation shows.
type Result struct {
	To string `json:"to"`
	// Library is the folder the library is in now.
	Library string `json:"library"`
	// MovedAside is the source database's new name: the old copy, kept.
	MovedAside string `json:"moved_aside,omitempty"`
	// OldFolder holds the rest of the old copy, and OldBytes is about how
	// much space removing it would give back.
	OldFolder string `json:"old_folder,omitempty"`
	OldBytes  int64  `json:"old_bytes,omitempty"`
	// Superseded is a database that was already in the destination, set
	// aside rather than overwritten.
	Superseded string `json:"superseded,omitempty"`
	// Warning is something that went wrong after the library was safely
	// across — it is not a failure, but it needs saying.
	Warning string `json:"warning,omitempty"`
}

// serviceAnswerWait is how long a newly installed service gets to answer
// before the switch is undone. A cold start of the same database the app was
// just serving takes seconds; a minute is room for a slow disk, not a guess.
const serviceAnswerWait = time.Minute

// headroom is kept free on the destination beyond the copy itself, so a
// switch never fills a disk to the last byte with the server about to start
// writing to it.
const headroom = 256 << 20

func (e Env) report(p Progress) {
	if e.Report != nil {
		e.Report(p)
	}
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// copyAndProve is the part both directions share: plan, check room, copy,
// prove (ADR 0092 §3, §4). The copy is left under its incoming name for the
// caller to place when the rest of its order allows.
func (e Env) copyAndProve(src, dst string) (total int64, err error) {
	if sameDir(src, dst) {
		return 0, fmt.Errorf("the library is already in %s", dst)
	}
	p, err := planCopy(src)
	if err != nil {
		return 0, err
	}
	if e.FreeBytes != nil {
		if free, ok := e.FreeBytes(dst); ok && free < uint64(p.total)+headroom {
			return p.total, fmt.Errorf("the library is %s and %s has %s free; make room and switch again",
				humanBytes(p.total), dst, humanBytes(int64(free)))
		}
	}
	if err := clearIncoming(dst); err != nil {
		return p.total, err
	}
	e.report(Progress{Phase: PhaseCopying, Total: p.total})
	if err := copyLibrary(p, src, dst, func(done, total int64) {
		e.report(Progress{Phase: PhaseCopying, Done: done, Total: total})
	}); err != nil {
		_ = clearIncoming(dst)
		return p.total, err
	}
	e.report(Progress{Phase: PhaseVerifying})
	if err := prove(dst, e.Verify); err != nil {
		_ = clearIncoming(dst)
		return p.total, err
	}
	return p.total, nil
}

/*
 * ToApp moves the library out of the service and removes the service
 * (ADR 0092 §6). Afterwards the app starts its own server on UserDir.
 *
 * The order is what makes it safe to interrupt. The library is copied and
 * proven while the service is merely stopped, so until the copy is placed a
 * failure is undone by starting the service again. Then the service is set to
 * start manually — so that a reboot in the next few milliseconds cannot bring
 * it back over an empty folder — then the source is set aside, then the
 * service goes.
 */
func ToApp(e Env) (res Result, err error) {
	res = Result{To: "app", Library: e.UserDir}
	installed := e.Service.Installed()

	// The tail of an interrupted switch: the library already left, and an
	// idle registration was all that remained. Finishing is removing it.
	if !HasLibrary(e.ServiceDir) {
		if !installed {
			return res, errors.New("LANcast is not running as a service")
		}
		e.report(Progress{Phase: PhaseFinishing})
		if err := e.Service.Uninstall(); err != nil {
			return res, fmt.Errorf("remove the service: %w", err)
		}
		return res, nil
	}

	stopped := false
	defer func() {
		if err != nil && stopped {
			// Put it back the way it was found. The window then waits for
			// the service exactly as it does at login.
			_ = clearIncoming(e.UserDir)
			_ = e.Service.SetAutomatic(true)
			if serr := e.Service.Start(); serr != nil {
				err = fmt.Errorf("%w (and the service could not be started again: %v)", err, serr)
			}
		}
	}()

	if installed {
		e.report(Progress{Phase: PhaseStopping})
		if err := e.Service.Stop(); err != nil {
			return res, fmt.Errorf("stop the service: %w", err)
		}
		stopped = true
	}
	if e.GrantUser != nil {
		if err := e.GrantUser(e.UserDir); err != nil {
			return res, fmt.Errorf("give you your folder %s: %w", e.UserDir, err)
		}
	}
	total, err := e.copyAndProve(e.ServiceDir, e.UserDir)
	if err != nil {
		return res, err
	}

	e.report(Progress{Phase: PhaseFinishing})
	if installed {
		if err := e.Service.SetAutomatic(false); err != nil {
			return res, fmt.Errorf("stop the service starting at boot: %w", err)
		}
	}
	superseded, err := place(e.UserDir, e.now())
	res.Superseded = superseded
	if err != nil {
		return res, err
	}
	moved, err := setAside(e.ServiceDir, "moved", e.now())
	if err != nil {
		return res, e.uncommit(e.UserDir, err)
	}
	res.MovedAside, res.OldFolder, res.OldBytes = moved, e.ServiceDir, total
	stopped = false // nothing to undo from here: the library is across.

	if installed {
		if err := e.Service.Uninstall(); err != nil {
			res.Warning = "The library moved, but the service could not be removed (" + err.Error() +
				"). It will not start at boot. Switch again to finish removing it."
		}
	}
	return res, nil
}

// uncommit takes a placed copy's name away again when a later step fails, so
// the folder that was not running does not suddenly hold a library. A rename,
// like every other step: the copy is kept as "failed".
func (e Env) uncommit(dir string, cause error) error {
	if _, err := setAside(dir, "failed", e.now()); err != nil {
		return fmt.Errorf("%w (and the copy in %s could not be set aside: %v)", cause, dir, err)
	}
	return cause
}

/*
 * ToService moves the library into the machine-wide folder and installs the
 * service on it (ADR 0092 §6). The caller has already stopped the server the
 * app started.
 *
 * The service is registered to start manually before the copy takes its
 * name, and set to start at boot only once it has answered. Each half closes
 * an interruption: a boot-start registration over a folder with no library
 * would make an empty one at boot — which reads as everything deleted — and a
 * library with no registration is a folder the app cannot write.
 *
 * The proof here is stronger than a database opening: the service itself has
 * to answer, running as the account that will own the library from now on.
 * Until it does, the person's own library is untouched, and if it does not
 * the service is removed and the copy set aside, so the app can start its own
 * server again as though nothing had happened.
 */
func ToService(e Env) (res Result, err error) {
	res = Result{To: "service", Library: e.ServiceDir}
	if !HasLibrary(e.UserDir) {
		return res, fmt.Errorf("there is no library in %s to move", e.UserDir)
	}
	if e.Service.Installed() {
		if HasLibrary(e.ServiceDir) {
			return res, errors.New("LANcast is already running as a service")
		}
		// A registration with no library is the remains of an interrupted
		// switch. It serves nothing; it is replaced below.
		if err := e.Service.Uninstall(); err != nil {
			return res, fmt.Errorf("remove the old service registration: %w", err)
		}
	}

	total, err := e.copyAndProve(e.UserDir, e.ServiceDir)
	if err != nil {
		return res, err
	}

	e.report(Progress{Phase: PhaseInstalling})
	if err := e.Service.Install(e.Addr); err != nil {
		_ = clearIncoming(e.ServiceDir)
		return res, fmt.Errorf("install the service: %w", err)
	}
	superseded, err := place(e.ServiceDir, e.now())
	res.Superseded = superseded
	if err != nil {
		_ = e.Service.Uninstall()
		_ = clearIncoming(e.ServiceDir)
		return res, err
	}
	e.report(Progress{Phase: PhaseStarting})
	if err := e.Service.Start(); err != nil {
		return res, e.undoService(fmt.Errorf("start the service: %w", err))
	}
	if !e.Answers(e.Addr, serviceAnswerWait) {
		return res, e.undoService(fmt.Errorf("the service started but did not answer within %s", serviceAnswerWait))
	}

	e.report(Progress{Phase: PhaseFinishing})
	if err := e.Service.SetAutomatic(true); err != nil {
		res.Warning = "The service is running, but could not be set to start with Windows (" + err.Error() +
			"). Set it to Automatic in Windows Services."
	}
	moved, err := setAside(e.UserDir, "moved", e.now())
	if err != nil {
		// The service is serving the library. The old copy keeping its name
		// costs a second copy and nothing else, because the machine-wide
		// folder wins everywhere it is asked.
		res.Warning = "The service is running, but the old copy in " + e.UserDir +
			" could not be set aside (" + err.Error() + "). It is not being used."
		return res, nil
	}
	res.MovedAside, res.OldFolder, res.OldBytes = moved, e.UserDir, total
	return res, nil
}

// undoService removes a service that would not come up and sets its copy
// aside. Best-effort on the service, because the cause is the news.
func (e Env) undoService(cause error) error {
	_ = e.Service.Stop()
	_ = e.Service.Uninstall()
	return e.uncommit(e.ServiceDir, cause)
}

// humanBytes prints a size somebody decides about disk space by.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
