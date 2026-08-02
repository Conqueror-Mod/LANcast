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

// Detect returns the directory containing ffprobe, or "" if it cannot be found.
// It checks PATH first, then the machine-wide locations a service can actually
// see. Per-user install dirs are deliberately not guessed at here: the service
// account's home is not the installing user's, so the install-time record is what
// covers that case.
func Detect() string {
	if p, err := exec.LookPath(binary); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(p)
	}
	for _, dir := range commonDirs() {
		if hasProbe(dir) {
			return dir
		}
	}
	return ""
}

// hasProbe reports whether dir contains an ffprobe executable.
func hasProbe(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, exeName(binary)))
	return err == nil && !info.IsDir()
}

// commonDirs are machine-wide install locations worth checking when PATH does
// not have the tools — the usual places a package manager or a manual install
// puts them.
func commonDirs() []string {
	if runtime.GOOS == "windows" {
		var out []string
		for _, base := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			os.Getenv("ProgramData"),
		} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, "ffmpeg", "bin"),
				filepath.Join(base, "chocolatey", "bin"),
			)
		}
		return append(out, `C:\ffmpeg\bin`)
	}
	return []string{"/usr/local/bin", "/usr/bin", "/opt/ffmpeg/bin", "/snap/bin"}
}

// Resolve settles where the tools are for this process: it honours a configured
// directory, otherwise detects one, and reports the directory in use (empty when
// the tools genuinely are not installed). Callers persist a newly detected
// directory so a service that cannot see PATH still finds them next start.
func Resolve(configured string) (dir string, ok bool) {
	if Ensure(configured) {
		return configured, true
	}
	found := Detect()
	if found == "" {
		return "", false
	}
	// Detect may have found it on PATH already; Ensure is harmless then.
	Ensure(found)
	return found, true
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
