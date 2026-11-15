package mediatools

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ManagedDir is the tools folder a server keeps in its data directory: where
// the in-app download puts ffmpeg, and where an installer copies a per-user
// one.
func ManagedDir(dataDir string) string { return filepath.Join(dataDir, "tools") }

// Result is where the tools were found, if anywhere, and what was turned down
// on the way.
type Result struct {
	Dir     string
	OK      bool
	Refused []Refusal
}

/*
 * Locator settles which directory supplies ffmpeg and ffprobe to this process,
 * and remembers the answer for the server's status.
 *
 * With a nil Guard it is the lookup the server has always had: the configured
 * directory, then beside the executable, then the managed folder, then PATH.
 *
 * With a Guard — the service — each candidate holding ffprobe has to pass it,
 * and a refusal is kept with its reason. PATH is then rewritten for this
 * process to the chosen directory followed by only the PATH entries the guard
 * passes. That part is not optional: probe, transcode, the marker worker and
 * the rest all call exec.LookPath("ffmpeg"), so a refused directory still on
 * PATH would be run anyway, and so would a user-writable folder somebody
 * once added to the machine's PATH.
 */
type Locator struct {
	Guard   Guard
	Managed string
	// protect is Protect; tests replace it, since taking ownership of a
	// folder needs an administrator.
	protect func(string) error
	// owner is ownByAdministrators, for the same reason.
	owner func(string) error

	mu   sync.Mutex
	last Result
}

// Last is the most recent Resolve's answer.
func (l *Locator) Last() Result {
	if l == nil {
		return Result{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.last
	out.Refused = append([]Refusal(nil), l.last.Refused...)
	return out
}

// Resolve finds the tools, puts their directory first on this process's PATH,
// and reports the directory in use.
func (l *Locator) Resolve(configured string) Result {
	var cands []string
	if configured != "" {
		cands = append(cands, configured)
	}
	cands = append(cands, selfDirs()...)
	if l.Managed != "" {
		cands = append(cands, l.Managed)
	}
	path := os.Getenv("PATH")
	cands = append(cands, pathEntries(path)...)

	var res Result
	seen := map[string]bool{}
	for _, dir := range cands {
		key := pathKey(dir)
		if seen[key] {
			continue
		}
		seen[key] = true
		if !hasProbe(dir) {
			continue
		}
		if err := l.check(dir); err != nil {
			res.Refused = append(res.Refused, Refusal{Dir: dir, Reason: err.Error()})
			continue
		}
		res.Dir, res.OK = dir, true
		break
	}

	os.Setenv("PATH", l.rewritePath(res.Dir, path))

	l.mu.Lock()
	l.last = res
	l.mu.Unlock()
	return res
}

// check applies the guard to a directory holding the tools: the folder, then
// ffprobe, then ffmpeg when it is there. A folder with ffprobe and no ffmpeg
// can still probe; transcode then reports itself unavailable rather than
// borrowing an ffmpeg from somewhere unchecked, since PATH is filtered too.
func (l *Locator) check(dir string) error {
	if l.Guard == nil {
		return nil
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%q is not an absolute path", dir)
	}
	if err := l.Guard.CheckDir(dir); err != nil {
		return err
	}
	for _, name := range wanted {
		p := filepath.Join(dir, exeName(name))
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) && name != binary {
			continue
		}
		if err := l.Guard.CheckFile(p); err != nil {
			return err
		}
	}
	return nil
}

// rewritePath puts chosen first and, under a guard, drops every entry the
// guard does not pass. An entry with no tools in it needs only its folder to
// pass; one holding ffmpeg or ffprobe needs the binaries to pass as well.
func (l *Locator) rewritePath(chosen, path string) string {
	out := []string{}
	seen := map[string]bool{}
	add := func(p string) {
		if k := pathKey(p); !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	if chosen != "" {
		add(chosen)
	}
	for _, p := range pathEntries(path) {
		if seen[pathKey(p)] {
			continue
		}
		if l.Guard != nil && !l.pathEntryOK(p) {
			continue
		}
		add(p)
	}
	return strings.Join(out, string(os.PathListSeparator))
}

func (l *Locator) pathEntryOK(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	if hasAnyTool(p) {
		return l.check(p) == nil
	}
	return l.Guard.CheckDir(p) == nil
}

func hasAnyTool(dir string) bool {
	for _, name := range wanted {
		if _, err := os.Lstat(filepath.Join(dir, exeName(name))); err == nil {
			return true
		}
	}
	return false
}

// pathKey compares directories the way the platform does: case-blind on
// Windows, where C:\FFmpeg and c:\ffmpeg are one folder.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if os.PathSeparator == '\\' {
		return strings.ToLower(p)
	}
	return p
}

// Prepare makes dir ready to receive a tools install: created, and under a
// guard protected and then checked, so an install never lands in a folder the
// service would refuse to run it from.
func (l *Locator) Prepare(dir string) error {
	if l == nil || l.Guard == nil {
		return os.MkdirAll(dir, 0o755)
	}
	protect := l.protect
	if protect == nil {
		protect = Protect
	}
	if err := protect(dir); err != nil {
		return err
	}
	return l.Guard.CheckDir(dir)
}

/*
 * ForService settles which directory a service should record, at install.
 *
 * found is what the installing user can see, usually Detect's answer. When it
 * passes the guard it is recorded as it is: a machine-wide install under
 * Program Files, say. When it does not — a winget install under the user's
 * AppData, the case R09-1 was found on — the binaries are copied into the
 * protected managed folder and that is recorded instead. The copy is the
 * installing administrator's decision to trust that ffmpeg, taken once with
 * UAC's consent; the service never copies from a folder it refused, since that
 * would launder whatever is in the folder by the next start.
 */
func ForService(g Guard, dataDir, found string) (string, error) {
	return forService(&Locator{Guard: g}, dataDir, found)
}

func forService(l *Locator, dataDir, found string) (string, error) {
	if found == "" {
		return "", nil
	}
	if l.check(found) == nil {
		return found, nil
	}
	dst := ManagedDir(dataDir)
	if err := l.adopt(found, dst); err != nil {
		return "", err
	}
	return dst, nil
}

/*
 * Adopt copies ffmpeg, ffprobe and any DLLs beside them from src into the
 * protected folder dst, and checks the result.
 *
 * Links are followed on the way in — winget's Links folder holds only links —
 * and the copies are plain files owned by whoever runs this. They are written
 * to a staging folder inside dst, which inherits its protection, and moved
 * into place with ffprobe last, so an interrupted copy reads as absent, as an
 * interrupted download does.
 */
func Adopt(g Guard, src, dst string) error { return (&Locator{Guard: g}).adopt(src, dst) }

func (l *Locator) adopt(src, dst string) error {
	if err := l.Prepare(dst); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(dst, ".adopt-*")
	if err != nil {
		return fmt.Errorf("could not create a staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	names := []string{exeName("ffmpeg")}
	if dlls, _ := filepath.Glob(filepath.Join(src, "*.dll")); len(dlls) > 0 {
		for _, d := range dlls {
			names = append(names, filepath.Base(d))
		}
	}
	if _, err := os.Stat(filepath.Join(src, exeName("ffmpeg"))); err != nil {
		names = names[1:] // ffprobe alone still probes
	}
	names = append(names, exeName(binary)) // last, deliberately
	for _, n := range names {
		if err := copyFile(filepath.Join(src, n), filepath.Join(staging, n)); err != nil {
			return err
		}
	}
	for _, n := range names {
		to := filepath.Join(dst, n)
		_ = os.Remove(to)
		if err := os.Rename(filepath.Join(staging, n), to); err != nil {
			return fmt.Errorf("could not put %s in place: %w", n, err)
		}
		if l.Guard != nil {
			if err := l.own(to); err != nil {
				return err
			}
		}
	}
	if err := l.check(dst); err != nil {
		return fmt.Errorf("the copied tools still do not pass: %w", err)
	}
	return nil
}

func (l *Locator) own(path string) error {
	if l.owner != nil {
		return l.owner(path)
	}
	return ownByAdministrators(path)
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", from, err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", to, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("could not copy %s: %w", filepath.Base(from), err)
	}
	return out.Close()
}
