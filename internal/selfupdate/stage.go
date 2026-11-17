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
// effect the next time the service starts. Nothing kills itself and no second
// process is needed.
//
// The renamed file is swept on a later startup, and "later" is doing real work
// in that sentence. It used to say the old executable is deleted on the next
// startup "once it is no longer running", which assumed the only thing running
// it was the process being replaced. The server tray is resident and runs out
// of the same directory, so after one update it holds the renamed file open
// indefinitely — and a *fixed* backup name then made every subsequent update
// fail, because the next rename had to replace a mapped image:
//
//	rename LANcast-Server.exe LANcast-Server.exe.old: Access is denied.
//
// Backups are therefore stamped and never reused. A sweep takes what it can and
// leaves what is still mapped for the next one.
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// Stage writes verified files into the staging directory, with the release's
// proof beside them, and records a manifest.
//
// The files must be exactly the install files of proof's archive, and the
// proof must verify for version: Stage checks it here too, so a staging that
// Apply would refuse is never written.
//
// The manifest is written last, on purpose: it is the marker that a staged
// update is complete, so an interrupted download leaves a directory with no
// manifest, which Pending reports as nothing to do.
func Stage(dataDir, version string, proof Proof, files map[string][]byte, now int64) error {
	if version == "" {
		return fmt.Errorf("selfupdate: staging needs a version")
	}
	if len(files) == 0 {
		return fmt.Errorf("selfupdate: nothing to stage")
	}
	names := make([]string, 0, len(files))
	for name := range files {
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
		names = append(names, name)
	}
	want, err := prove(version, proof)
	if err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	if err := checkNames(names, want); err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	for name, body := range files {
		if digest(body) != want[name] {
			return fmt.Errorf("selfupdate: %s is not the file the release holds", name)
		}
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
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o755); err != nil {
			return fmt.Errorf("selfupdate: writing %s: %w", name, err)
		}
		m.Files = append(m.Files, name)
	}
	for name, body := range map[string][]byte{
		proofChecksums: proof.Checksums, proofSignature: proof.Signature, proofArchive: proof.Archive,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			return fmt.Errorf("selfupdate: writing %s: %w", name, err)
		}
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
// The staging is proved again first (verify.go): the signature over the
// checksum list, the archive against it, and every staged file against the
// archive, with names limited to InstallFiles. newer, when set, must accept
// the staged version, so a genuine older release staged after startup's own
// check cannot be installed as a downgrade. A staging that fails any of that
// is discarded. Each file is checked once more where it lands, and a failure
// partway rolls back what was already done. An install that did not update is
// a recoverable disappointment; an install missing its executable, or running
// somebody else's, is not.
func Apply(dataDir, installDir string, newer func(version string) bool) (Manifest, error) {
	m, ok := Pending(dataDir)
	if !ok {
		return Manifest{}, os.ErrNotExist
	}
	staging := Dir(dataDir)

	if newer != nil && !newer(m.Version) {
		_ = os.RemoveAll(staging)
		return m, fmt.Errorf("selfupdate: staged %s is not newer than this build, so it was discarded", m.Version)
	}

	/*
	 * Check first. Moving half an update and discovering the rest is missing is
	 * the failure this ordering exists to prevent.
	 *
	 * And when the set *is* incomplete, the staging directory is thrown away
	 * rather than left to be retried. A manifest naming files that are no
	 * longer beside it can never be satisfied — every restart re-reads it,
	 * fails here, and changes nothing — so keeping it does not preserve an
	 * update, it preserves a permanent refusal to update.
	 *
	 * The comment at the end of this function used to call exactly that state
	 * harmless. It is not: an install wedged here stays on its old version for
	 * ever while the log repeats one line nobody reads. Clearing it costs a
	 * re-download and puts the next check back in charge.
	 */
	want, err := checkStaged(staging, m)
	if err != nil {
		_ = os.RemoveAll(staging)
		return m, fmt.Errorf("selfupdate: the staged update was refused and discarded, "+
			"and will be fetched again: %w", err)
	}
	afterCheck()

	type undo struct{ from, to string }
	var done []undo
	// added are files the update put where nothing was, which rolling back
	// removes rather than leaves.
	var added []string

	rollback := func() {
		for _, p := range added {
			_ = os.Remove(p)
		}
		// Reverse order, so a file renamed out of the way goes back before
		// anything that was moved on top of it.
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.Rename(done[i].to, done[i].from)
		}
	}

	/*
	 * A backup name nothing can still be holding.
	 *
	 * This used to be a fixed `.old`, cleared first with a best-effort Remove
	 * and the comment "a leftover from an earlier update would make this rename
	 * fail". That was the right worry and the wrong remedy, because the leftover
	 * that matters cannot be removed at all.
	 *
	 * The server tray is a resident process running out of the install
	 * directory. When an update renames the running image to `.old`, that
	 * process keeps the file mapped — for as long as it lives, which is until
	 * somebody quits it. `Remove` then fails, and the next update's rename has
	 * to *replace* a mapped image, which Windows refuses:
	 *
	 *	rename LANcast-Server.exe LANcast-Server.exe.old: Access is denied.
	 *
	 * Measured on the reporting install: v0.8.31 and v0.8.32 applied cleanly,
	 * v0.8.33 failed with exactly that. The first update after the tray starts
	 * works and every one after it fails, permanently — which is the worst
	 * shape a bug can have, because it looks like a one-off.
	 *
	 * A unique suffix sidesteps the whole question. Nothing is replaced, so
	 * nothing can be held; CleanupOld sweeps whatever it can whenever it can,
	 * and a file still mapped by a live process is simply swept later.
	 */
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)

	for _, name := range m.Files {
		target := filepath.Join(installDir, name)
		backup := target + ".old." + stamp

		existed := false
		if _, err := os.Stat(target); err == nil {
			existed = true
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
		if !existed {
			added = append(added, target)
		}
		/*
		 * The install folder's permissions, not staging's. A rename keeps the
		 * descriptor the file had in the data directory, which is SYSTEM and
		 * Administrators only, and the window and tray then cannot run it
		 * (inherit_windows.go). A file that cannot be given them is not
		 * installed: an update that leaves the app unable to start is worse
		 * than none.
		 */
		if err := inheritFromParent(target); err != nil {
			if existed {
				_ = os.Remove(target)
			}
			rollback()
			return m, fmt.Errorf("selfupdate: giving %s the install folder's permissions: %w", name, err)
		}
		// Once more where it landed. Between the check above and this move the
		// staged file is reachable through the data directory; here it is in a
		// folder only an administrator can write, so what this reads is what
		// the next start runs.
		if got, err := fileDigest(target); err != nil || got != want[name] {
			if existed {
				_ = os.Remove(target)
			}
			rollback()
			_ = os.RemoveAll(staging)
			return m, fmt.Errorf("selfupdate: %s changed after it was checked; the install is unchanged "+
				"and the staged update was discarded", name)
		}
	}

	// Only now is the update committed. A crash between the moves and here
	// leaves a manifest naming files that are already installed and no longer
	// staged; the next Apply finds them missing, discards the staging directory
	// and asks for the release again. Wasteful, and recoverable, which is the
	// property that matters — an earlier version of this comment called that
	// state harmless and left it to wedge instead.
	_ = os.RemoveAll(staging)
	return m, nil
}

/*
 * DiscardStaged throws away a staged update without applying it.
 *
 * For the case a staged update stops being wanted rather than failing: an
 * install that overtakes it. The updater stages a release and applies it on a
 * clean shutdown, but an installer *force-kills* the service, so the staging
 * survives an install that has already delivered a newer build — and is then
 * offered for ever as "ready to install", naming a version older than the one
 * running. Applying it would be a downgrade.
 *
 * Reported as a v0.8.33 banner on a machine running v0.8.34.
 */
func DiscardStaged(dataDir string) error {
	return os.RemoveAll(Dir(dataDir))
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
		// Both spellings: `.old` from installs that predate the stamped name,
		// and `.old.<stamp>` from this one. A sweep that only knew the new one
		// would leave the very file that caused this to be written for ever.
		if e.IsDir() || !isBackupName(e.Name()) {
			continue
		}
		if os.Remove(filepath.Join(installDir, e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// isBackupName reports whether a file is one Apply left behind.
//
// `x.exe.old` is what earlier versions wrote; `x.exe.old.<stamp>` is what this
// one writes. Matching both is what stops an upgrade from stranding the file
// whose immovability caused the stamp to exist.
func isBackupName(name string) bool {
	if filepath.Ext(name) == ".old" {
		return true
	}
	return strings.Contains(name, ".old.")
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

// afterCheck runs between Apply's proof and its moves. Tests swap a staged
// file here, to show that the check where it lands is what catches it.
var afterCheck = func() {}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
