package selfupdate

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/release"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// signedRelease builds what a published release is — an archive holding
// files, a checksum list naming the archive, and a signature over the list —
// and installs the signing key for the test.
func signedRelease(t *testing.T, version string, files map[string][]byte) Proof {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	checks := []byte(hex.EncodeToString(sum[:]) + "  " + ArchiveName(version) + "\n")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release.SetKeyForTest(hex.EncodeToString(pub)))
	return Proof{
		Checksums: checks,
		Signature: []byte(hex.EncodeToString(ed25519.Sign(priv, checks))),
		Archive:   archive,
	}
}

// stage stages a genuine release of files.
func stage(t *testing.T, data, version string, files map[string][]byte) {
	t.Helper()
	if err := Stage(data, version, signedRelease(t, version, files), files, 1000); err != nil {
		t.Fatal(err)
	}
}

func TestStageThenApplyReplacesTheInstall(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(install, "LANcast.exe"), []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}

	stage(t, data, "v0.7.0", map[string][]byte{
		"LANcast.exe":        []byte("new program"),
		"WebView2Loader.dll": []byte("new loader"),
	})

	m, ok := Pending(data)
	if !ok || m.Version != "v0.7.0" {
		t.Fatalf("Pending = %+v, %v", m, ok)
	}

	if _, err := Apply(data, install, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := read(t, filepath.Join(install, "LANcast.exe")); got != "new program" {
		t.Errorf("program = %q, want the new one", got)
	}
	if got := read(t, filepath.Join(install, "WebView2Loader.dll")); got != "new loader" {
		t.Errorf("loader = %q, want the new one", got)
	}
	// The replaced file is moved aside rather than deleted, because it may be
	// the running process's own image. The name is stamped rather than fixed —
	// see backupOf and the test below for why — so it is found by pattern.
	if got := read(t, backupOf(t, install, "LANcast.exe")); got != "old program" {
		t.Errorf("the previous program was not preserved, got %q", got)
	}
	if _, ok := Pending(data); ok {
		t.Error("staging survived a successful apply")
	}
}

// The failure that must never happen: an install left without its executable.
// A staging that fails its check moves nothing.
func TestApplyRollsBackOnFailure(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(install, "LANcast.exe"), []byte("old a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "WebView2Loader.dll"), []byte("old b"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage(t, data, "v0.7.0", map[string][]byte{
		"LANcast.exe":        []byte("new a"),
		"WebView2Loader.dll": []byte("new b"),
	})

	// Delete one staged file behind Apply's back, so the pre-flight check fires
	// before anything has been moved.
	if err := os.Remove(filepath.Join(Dir(data), "WebView2Loader.dll")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(data, install, nil); err == nil {
		t.Fatal("Apply succeeded with a staged file missing")
	}

	if got := read(t, filepath.Join(install, "LANcast.exe")); got != "old a" {
		t.Errorf("LANcast.exe = %q; the install was modified despite the failure", got)
	}
	if got := read(t, filepath.Join(install, "WebView2Loader.dll")); got != "old b" {
		t.Errorf("WebView2Loader.dll = %q; the install was modified despite the failure", got)
	}
}

// An interrupted download leaves files with no manifest, which must read as
// nothing to do rather than as a half-update to install.
func TestIncompleteStagingIsNotPending(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(Dir(data), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(data), "LANcast.exe"), []byte("half"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := Pending(data); ok {
		t.Error("a staging directory with no manifest was reported as pending")
	}
}

// A staged name that escapes the staging directory would be moved anywhere the
// service can write, which is everywhere.
func TestStageRefusesPathsThatEscape(t *testing.T) {
	data := t.TempDir()
	for _, bad := range []string{
		"../evil.exe",
		`..\evil.exe`,
		"sub/evil.exe",
		"",
		".",
	} {
		files := map[string][]byte{bad: []byte("x")}
		if err := Stage(data, "v1.0.0", signedRelease(t, "v1.0.0", files), files, 1); err == nil {
			t.Errorf("Stage accepted the path %q", bad)
		}
	}
}

// Staging twice keeps only the newer one. A stale file from an abandoned update
// must not ride along with the next.
func TestStagingReplacesAnEarlierOne(t *testing.T) {
	data := t.TempDir()
	stage(t, data, "v0.7.0", map[string][]byte{"LANcast.exe": []byte("a")})
	stage(t, data, "v0.8.0", map[string][]byte{"WebView2Loader.dll": []byte("b")})
	m, _ := Pending(data)
	if m.Version != "v0.8.0" || len(m.Files) != 1 || m.Files[0] != "WebView2Loader.dll" {
		t.Errorf("manifest = %+v, want only the newer staging", m)
	}
	if _, err := os.Stat(filepath.Join(Dir(data), "LANcast.exe")); err == nil {
		t.Error("a file from the abandoned staging survived")
	}
}

func TestCleanupOldRemovesPreviousImages(t *testing.T) {
	install := t.TempDir()
	for _, n := range []string{"LANcast-Server.exe.old", "LANcast-Client.exe.old"} {
		if err := os.WriteFile(filepath.Join(install, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(install, "LANcast-Server.exe"), []byte("live"), 0o755); err != nil {
		t.Fatal(err)
	}

	if n := CleanupOld(install); n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if _, err := os.Stat(filepath.Join(install, "LANcast-Server.exe")); err != nil {
		t.Error("CleanupOld removed a live executable")
	}
}

// Applying with nothing staged is not an error worth propagating; it is the
// normal case on every start.
func TestApplyWithNothingStaged(t *testing.T) {
	if _, err := Apply(t.TempDir(), t.TempDir(), nil); !os.IsNotExist(err) {
		t.Errorf("Apply with nothing staged = %v, want ErrNotExist", err)
	}
}

// backupOf finds the file Apply moved aside for name, whatever it stamped it.
func backupOf(t *testing.T, dir, name string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), name+".old") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("no backup of %s in %v", name, entries)
	return ""
}

/*
 * Two updates in a row, with the first backup impossible to remove.
 *
 * This is the reported failure, reduced. The server tray is resident and runs
 * out of the install directory, so after one update it holds the renamed image
 * open for as long as it lives. With a fixed `.old` the next update had to
 * *replace* a mapped file, and Windows refuses:
 *
 *	rename LANcast-Server.exe LANcast-Server.exe.old: Access is denied.
 *
 * Measured on the reporting install: v0.8.31 and v0.8.32 applied cleanly and
 * v0.8.33 failed with exactly that. The first update after the tray starts
 * works and every one after it fails — permanently, and looking like a one-off.
 *
 * A file that cannot be removed is the thing to simulate, and an open handle is
 * not portable; a *directory* where the backup would go is refused by Remove
 * and by Rename on every platform, which is the same obstruction for this
 * purpose.
 */
func TestASecondUpdateSurvivesAnImmovableBackup(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	exe := filepath.Join(install, "LANcast.exe")
	if err := os.WriteFile(exe, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Something that cannot be removed or replaced, sitting exactly where a
	// fixed backup name would want to go.
	if err := os.Mkdir(exe+".old", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(exe+".old", "held"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stage(t, data, "v2", map[string][]byte{"LANcast.exe": []byte("v2")})
	if _, err := Apply(data, install, nil); err != nil {
		t.Fatalf("Apply refused to update past an immovable backup: %v", err)
	}
	if got := read(t, exe); got != "v2" {
		t.Errorf("program = %q, want v2 — the update did not take", got)
	}
}

// And the sweep takes both spellings, or the file whose immovability caused the
// stamp would be left behind for ever by the version that fixed it.
func TestCleanupTakesOldAndStampedBackups(t *testing.T) {
	install := t.TempDir()
	for _, n := range []string{"a.exe.old", "b.exe.old.k3j4h", "keep.exe"} {
		if err := os.WriteFile(filepath.Join(install, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := CleanupOld(install); got != 2 {
		t.Errorf("removed %d, want 2", got)
	}
	if _, err := os.Stat(filepath.Join(install, "keep.exe")); err != nil {
		t.Error("cleanup removed something that was not a backup")
	}
}

/*
 * An incomplete staging is discarded rather than retried for ever.
 *
 * This is the state the reporting install was left in: a manifest naming three
 * files with only one of them beside it, because an earlier Apply consumed two
 * and then failed. Nothing can satisfy that manifest, so every restart read it,
 * refused, and changed nothing — the install stayed on its old version while
 * the log repeated one line.
 *
 * Throwing it away costs a re-download and hands the next check back the
 * ability to do its job.
 */
func TestAnUnsatisfiableStagingIsDiscarded(t *testing.T) {
	data, install := t.TempDir(), t.TempDir()
	stage(t, data, "v2", map[string][]byte{
		"LANcast.exe":        []byte("a"),
		"WebView2Loader.dll": []byte("b"),
	})
	// One of them vanishes, exactly as a half-finished Apply leaves it.
	if err := os.Remove(filepath.Join(Dir(data), "WebView2Loader.dll")); err != nil {
		t.Fatal(err)
	}

	if _, err := Apply(data, install, nil); err == nil {
		t.Fatal("Apply accepted a staging it could not satisfy")
	}
	if _, ok := Pending(data); ok {
		t.Error("the unsatisfiable staging survived, so every later start " +
			"repeats the same refusal and the install never updates")
	}
}

/*
 * A staged update the running build has already overtaken.
 *
 * The updater applies a staging on a clean shutdown, and an installer
 * force-kills the service — so a staging can outlive an install that already
 * delivered a newer build. It was then offered for ever as "ready to install",
 * naming a version older than the one running, and applying it would have been
 * a downgrade. Reported as a v0.8.33 banner on a machine running v0.8.34.
 */
func TestDiscardStagedLeavesNothingPending(t *testing.T) {
	data := t.TempDir()
	stage(t, data, "v0.8.33", map[string][]byte{"LANcast.exe": []byte("older")})
	if _, ok := Pending(data); !ok {
		t.Fatal("nothing staged to discard")
	}

	if err := DiscardStaged(data); err != nil {
		t.Fatalf("DiscardStaged: %v", err)
	}
	if _, ok := Pending(data); ok {
		t.Error("the superseded staging survived, so the banner keeps offering " +
			"a version older than the one running")
	}
}

// Discarding what is not there is not an error: the check runs on every start,
// and the overwhelmingly common case is nothing staged at all.
func TestDiscardStagedOnAnEmptyDataDir(t *testing.T) {
	if err := DiscardStaged(t.TempDir()); err != nil {
		t.Errorf("DiscardStaged with nothing staged: %v", err)
	}
}

// --- R10-1: what Apply must refuse, whoever wrote the staging ---

// assertRefused applies and checks that nothing in install changed, nothing
// was written beside it, and the staging is gone.
func assertRefused(t *testing.T, data, install string, newer func(string) bool) error {
	t.Helper()
	_, err := Apply(data, install, newer)
	if err == nil {
		t.Fatal("Apply installed the staging")
	}
	if got := read(t, filepath.Join(install, "LANcast.exe")); got != "genuine" {
		t.Errorf("LANcast.exe = %q; the install was changed", got)
	}
	entries, _ := os.ReadDir(install)
	if len(entries) != 1 {
		t.Errorf("the install directory holds %d entries, want only LANcast.exe", len(entries))
	}
	if _, ok := Pending(data); ok {
		t.Error("the refused staging is still pending, so every stop retries it")
	}
	return err
}

func newInstall(t *testing.T) (data, install string) {
	t.Helper()
	data, install = t.TempDir(), filepath.Join(t.TempDir(), "Program Files", "LANcast")
	if err := os.MkdirAll(install, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(install, "LANcast.exe"), []byte("genuine"), 0o755); err != nil {
		t.Fatal(err)
	}
	return data, install
}

// The review's proving test, as a regression: a manifest and files written by
// hand, with nothing from Stage, including a name that leaves the install
// directory. It used to install both.
func TestApplyRefusesAHandWrittenStaging(t *testing.T) {
	signedRelease(t, "v99.0.0", map[string][]byte{"LANcast.exe": []byte("x")}) // a key is installed
	data, install := newInstall(t)
	st := Dir(data)
	if err := os.MkdirAll(st, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st, "LANcast.exe"), []byte("planted"), 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(filepath.Dir(filepath.Dir(install)), "escape.dll")
	if err := os.WriteFile(filepath.Join(filepath.Dir(data), "escape.dll"), []byte("planted"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := json.Marshal(Manifest{Version: "v99.0.0", Files: []string{"LANcast.exe", "../../escape.dll"}})
	if err := os.WriteFile(filepath.Join(st, manifestName), m, 0o644); err != nil {
		t.Fatal(err)
	}

	err := assertRefused(t, data, install, nil)
	t.Logf("refused: %v", err)
	if _, err := os.Stat(escape); err == nil {
		t.Error("a file was written outside the install directory")
	}
}

// A genuine staging whose file is replaced afterwards: the planted copy has the
// right name, and the signature still verifies, but the bytes are not the
// release's.
func TestApplyRefusesAStagedFileReplacedAfterStaging(t *testing.T) {
	data, install := newInstall(t)
	stage(t, data, "v2.0.0", map[string][]byte{"LANcast.exe": []byte("release")})
	if err := os.WriteFile(filepath.Join(Dir(data), "LANcast.exe"), []byte("planted"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, data, install, nil)
}

// A name added to a genuine manifest, outside the install set or outside the
// directory, is refused even though every listed file it shares with the
// release matches.
func TestApplyRefusesANameOutsideTheInstallSet(t *testing.T) {
	for _, extra := range []string{"version.dll", `..\..\Windows\System32\x.dll`, "LANcast.exe"} {
		t.Run(extra, func(t *testing.T) {
			data, install := newInstall(t)
			stage(t, data, "v2.0.0", map[string][]byte{"LANcast.exe": []byte("release")})
			m, _ := Pending(data)
			m.Files = append(m.Files, extra)
			raw, _ := json.Marshal(m)
			if err := os.WriteFile(filepath.Join(Dir(data), manifestName), raw, 0o644); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(extra, `\`) {
				_ = os.WriteFile(filepath.Join(Dir(data), extra), []byte("planted"), 0o755)
			}
			assertRefused(t, data, install, nil)
		})
	}
}

// Half a release: a manifest naming fewer files than the archive holds, which
// would pair a new program with old DLLs.
func TestApplyRefusesASubsetOfTheRelease(t *testing.T) {
	data, install := newInstall(t)
	stage(t, data, "v2.0.0", map[string][]byte{
		"LANcast.exe": []byte("release"), "libmpv-2.dll": []byte("player"),
	})
	m, _ := Pending(data)
	m.Files = []string{"LANcast.exe"}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(Dir(data), manifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, data, install, nil)
}

// A signature by any key but the release key.
func TestApplyRefusesAProofSignedByAnotherKey(t *testing.T) {
	data, install := newInstall(t)
	files := map[string][]byte{"LANcast.exe": []byte("release")}
	stage(t, data, "v2.0.0", files)
	other := signedRelease(t, "v2.0.0", map[string][]byte{"LANcast.exe": []byte("release")})
	_ = other // signedRelease installed a new key; the staged signature is the old one's
	assertRefused(t, data, install, nil)
}

// A genuine release staged under a newer version's number: the archive's
// signed name carries the real version, which does not match.
func TestApplyRefusesAReleaseUnderAnotherVersion(t *testing.T) {
	data, install := newInstall(t)
	stage(t, data, "v0.9.0", map[string][]byte{"LANcast.exe": []byte("old release")})
	m, _ := Pending(data)
	m.Version = "v99.0.0"
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(Dir(data), manifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	assertRefused(t, data, install, nil)
}

// And one honestly labelled but older than the running build: a downgrade.
func TestApplyRefusesAVersionThatIsNotNewer(t *testing.T) {
	data, install := newInstall(t)
	stage(t, data, "v0.9.0", map[string][]byte{"LANcast.exe": []byte("old release")})
	err := assertRefused(t, data, install, func(v string) bool { return v != "v0.9.0" })
	if !strings.Contains(err.Error(), "not newer") {
		t.Errorf("err = %v, want it to say the version is not newer", err)
	}
}

// A file swapped between the check and the move is caught where it lands, and
// the install is put back.
func TestApplyChecksEachFileWhereItLands(t *testing.T) {
	data, install := newInstall(t)
	stage(t, data, "v2.0.0", map[string][]byte{
		"LANcast.exe": []byte("release"), "WebView2Loader.dll": []byte("loader"),
	})
	afterCheck = func() {
		_ = os.WriteFile(filepath.Join(Dir(data), "WebView2Loader.dll"), []byte("planted"), 0o755)
	}
	t.Cleanup(func() { afterCheck = func() {} })
	assertRefused(t, data, install, nil)
}
