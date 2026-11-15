// Package mediatools makes ffmpeg and ffprobe findable by the server process.
//
// This exists because of a specific, silent failure: a Windows service runs as
// LocalSystem, whose PATH does not include a per-user ffmpeg install (winget puts
// it under the installing user's AppData). probe and transcode both resolve their
// binaries with exec.LookPath, so under a service they simply find nothing —
// every item stays unprobed, every playback decision falls back to direct play,
// and files the browser cannot decode are handed to it with no error anywhere.
// ADR 0016 called this out; recording the directory and putting it on the
// process PATH is how it is honoured.
package mediatools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// binary is the name to look for; the ffmpeg install that provides it provides
// ffmpeg too, so locating one locates both.
const binary = "ffprobe"

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// Ensure puts dir at the front of this process's PATH so exec.LookPath finds the
// tools there. An empty dir, or one without ffprobe in it, is ignored — a
// configured-but-wrong path should not break a PATH that already works.
func Ensure(dir string) bool {
	if dir == "" {
		return false
	}
	if !hasProbe(dir) {
		return false
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return true
}

/*
 * Detect returns the directory containing ffprobe, or "" if it cannot be found.
 *
 * Beside this executable first, then PATH. Nothing is guessed: it used to try
 * `C:\ffmpeg\bin` and `%ProgramData%\ffmpeg\bin` as well, which any local
 * account can create, and the service then ran what they put there as SYSTEM
 * (R09-1). An install that is in neither place is found by its recorded
 * directory, or fetched from Settings.
 *
 * Ours-first is the deliberate part. Tools sitting next to the server, or in a
 * `tools` directory beside it, are there because LANcast put them there — a
 * known, pinned build — and a stale copy earlier on somebody's PATH should not
 * win over one we installed on purpose. It also sidesteps the failure this
 * package exists for entirely: a directory found relative to os.Executable()
 * needs no PATH at all, so a service running as LocalSystem resolves it exactly
 * as a desktop process does.
 *
 * Detect does not judge what it finds; a service runs only what its Guard
 * passes (see Locator), and the installer copies a per-user find into the
 * protected tools folder (Adopt).
 */
func Detect() string {
	for _, dir := range selfDirs() {
		if hasProbe(dir) {
			return dir
		}
	}
	if p, err := exec.LookPath(binary); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(p)
	}
	return ""
}

/*
 * selfDirs are the places beside this executable worth looking.
 *
 * The executable's own directory covers a bundled or hand-dropped copy; a
 * `tools` subdirectory covers a managed install that would rather not scatter
 * two more binaries next to the ones the installer wrote.
 *
 * Symlinks are resolved so a server reached through one does not look beside
 * the link instead of beside the binary.
 */
func selfDirs() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	return []string{dir, filepath.Join(dir, "tools")}
}

// hasProbe reports whether dir contains an ffprobe executable.
func hasProbe(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, exeName(binary)))
	return err == nil && !info.IsDir()
}

// Resolve settles where the tools are for this process: it honours a configured
// directory, otherwise detects one, and reports the directory in use (empty when
// the tools genuinely are not installed). Callers persist a newly detected
// directory so a service that cannot see PATH still finds them next start.
//
// It is the Locator with no guard, for a server a person started.
func Resolve(configured string) (dir string, ok bool) {
	r := (&Locator{}).Resolve(configured)
	return r.Dir, r.OK
}

// Describe is a short, honest status line for logs and diagnostics.
func Describe(dir string, ok bool) string {
	if !ok {
		return "not found"
	}
	if dir == "" {
		return "on PATH"
	}
	return strings.TrimRight(dir, string(os.PathSeparator))
}
