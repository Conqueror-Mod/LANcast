// Package selfupdate stages a verified release and swaps it in on shutdown, so
// the next start runs the new version.
//
// The constraint that shapes everything: a running process holds its own
// executable open on Windows, so it cannot overwrite itself. It *can* rename
// itself — NTFS permits moving a file with an open handle — which is what makes
// the swap possible at all:
//
//	LANcast-Server.exe      ->  LANcast-Server.exe.old      (rename, allowed)
//	staged/LANcast-Server.exe -> LANcast-Server.exe          (move into place)
//
// The loaded image keeps running from the renamed file; the new binary takes
// effect the next time the service starts. Nothing kills itself, no second
// process is needed, and the old executable is deleted on the next startup once
// it is no longer running.
//
// Applied at shutdown rather than startup, which is what makes it one restart
// instead of two. A swap at startup would still be running the old image it
// just replaced.
//
// Failure is designed to be inert. Everything is verified before anything is
// moved, and a move that fails puts the original back — the worst outcome is an
// install that did not update, never one that cannot start.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirName is the staging directory, kept inside the data directory rather than
// beside the executable: the data directory is writable by the account the
// service runs as, and Program Files is not writable by anything else.
const DirName = "update-staged"

// manifestName records what was staged and which version it is, so a startup
// that finds a staging directory knows whether it is a leftover or a pending
// update, and the log can name the version rather than "an update".
const manifestName = "staged.json"

// Manifest describes a staged update.
type Manifest struct {
	Version string `json:"version"`
	// Files maps the name to place in the install directory to the staged file.
	// Recorded rather than inferred from a directory listing so a stray file
	// dropped into staging is never installed.
	Files []string `json:"files"`
	// StagedAt is unix seconds, for reporting how long an update has been
	// waiting for a restart.
	StagedAt int64 `json:"staged_at"`
}

// Dir is the staging directory inside a data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, DirName) }

// Stage writes verified files into the staging directory and records a
// manifest.
//
// The manifest is written last, on purpose: it is the marker that a staged
// update is complete, so an interrupted download leaves a directory with no
// manifest, which Pending reports as nothing to do.
func Stage(dataDir, version string, files map[string][]byte, now int64) error {
	if version == "" {
		return fmt.Errorf("selfupdate: staging needs a version")
	}
	if len(files) == 0 {
		return fmt.Errorf("selfupdate: nothing to stage")
	}
	dir := Dir(dataDir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("selfupdate: clearing staging: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}

	m := Manifest{Version: version, StagedAt: now}
	for name, body := range files {
		// Flat names only. A staged path containing a separator could be
		// written outside the staging directory, and later moved anywhere the
		// service can write — which is everywhere.
		//
		// Checked against both separators explicitly rather than through
		// filepath.Base, which is platform-dependent by design: on Linux
		// `..\evil.exe` is a legal single filename and passes Base unchanged,
		// so the guard would admit a name that escapes the moment the same data
		// directory is used on Windows. Caught by CI, which runs on Linux.
		if !safeStagedName(name) {
			return fmt.Errorf("selfupdate: refusing a staged path %q", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o755); err != nil {
			return fmt.Errorf("selfupdate: writing %s: %w", name, err)
		}
		m.Files = append(m.Files, name)
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), raw, 0o644)
}

// Pending reports a complete staged update, if there is one.
func Pending(dataDir string) (Manifest, bool) {
	raw, err := os.ReadFile(filepath.Join(Dir(dataDir), manifestName))
	if err != nil {
		return Manifest{}, false
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil || m.Version == "" || len(m.Files) == 0 {
		return Manifest{}, false
	}
	return m, true
}

// Discard removes a staged update. Used when it turns out to be unwanted — a
// version that was rolled back, or a check that now reports something newer.
func Discard(dataDir string) error { return os.RemoveAll(Dir(dataDir)) }

// Apply moves a staged update into installDir. Call it on the way down: the
// files it replaces may be running, and the new ones take effect at the next
// start.
//
// Every staged file is checked to exist before anything is moved, and a failure
// partway rolls back what was already done. An install that did not update is a
// recoverable disappointment; an install missing its executable is not.
func Apply(dataDir, installDir string) (Manifest, error) {
	m, ok := Pending(dataDir)
	if !ok {
		return Manifest{}, os.ErrNotExist
	}
	staging := Dir(dataDir)

	// Check first. Moving half an update and discovering the rest is missing is
	// the failure this ordering exists to prevent.
	for _, name := range m.Files {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			return m, fmt.Errorf("selfupdate: staged %s is missing: %w", name, err)
		}
	}

	type undo struct{ from, to string }
	var done []undo

	rollback := func() {
		// Reverse order, so a file renamed out of the way goes back before
		// anything that was moved on top of it.
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.Rename(done[i].to, done[i].from)
		}
	}

	for _, name := range m.Files {
		target := filepath.Join(installDir, name)
		backup := target + ".old"

		// Clear a previous .old first: a leftover from an earlier update would
		// make this rename fail and abort an otherwise fine install.
		_ = os.Remove(backup)

		if _, err := os.Stat(target); err == nil {
			// Rename rather than delete. The file may be this very process's
			// executable, which cannot be deleted while running but can be
			// moved aside.
			if err := os.Rename(target, backup); err != nil {
				rollback()
				return m, fmt.Errorf("selfupdate: moving %s aside: %w", name, err)
			}
			done = append(done, undo{from: target, to: backup})
		}
		if err := os.Rename(filepath.Join(staging, name), target); err != nil {
			rollback()
			return m, fmt.Errorf("selfupdate: installing %s: %w", name, err)
		}
	}

	// Only now is the update committed. Removing staging last means a crash
	// between the moves and here leaves a manifest that Apply would replay,
	// which is harmless — the files it names are already in place and the
	// staged copies are gone, so the next Apply reports them missing and
	// changes nothing.
	_ = os.RemoveAll(staging)
	return m, nil
}

// CleanupOld deletes the .old executables left by a previous Apply.
//
// Called at startup, when the previous image is no longer running and the file
// can finally be deleted. Failures are ignored: a leftover .old costs disk and
// nothing else, and refusing to start over one would be absurd.
func CleanupOld(installDir string) int {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return 0
	}
	var removed int
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".old" {
			continue
		}
		if os.Remove(filepath.Join(installDir, e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// safeStagedName reports whether a name may be written into staging and later
// moved into the install directory.
//
// Deliberately strict and deliberately platform-independent: a staged file may
// be produced on one operating system and applied on another, so a name is
// judged by the same rules everywhere rather than by whatever the running
// platform happens to treat as a separator.
func safeStagedName(name string) bool {
	switch name {
	case "", ".", "..":
		return false
	}
	if strings.ContainsAny(name, `/\:`) {
		return false
	}
	// A leading dot is not a separator problem, but nothing LANcast installs
	// starts with one, and allowing it invites confusion with the .old files
	// Apply creates.
	return !strings.HasPrefix(name, ".")
}
