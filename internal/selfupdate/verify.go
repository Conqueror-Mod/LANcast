package selfupdate

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"lancast/internal/release"
)

/*
 * Apply proves the staging again (R10-1).
 *
 * The signature and the archive's digest used to be checked once, by the
 * downloader, before Stage wrote anything; Apply then trusted whatever the
 * staging directory held. That directory is in the data directory, and as a
 * Windows service that was C:\ProgramData\LANcast with ProgramData's grant
 * letting every user add files. A hand-written staged.json and a LANcast.exe
 * of anybody's making were moved into Program Files by the service, as
 * SYSTEM, at its next clean stop; a name like ..\..\Windows\System32\x.dll
 * was moved anywhere.
 *
 * The data directory is closed now (internal/dataperm), and this does not rely
 * on it. Stage keeps the release's own proof beside the files — the checksum
 * list, its signature and the archive the files came out of — and Apply checks
 * all of it again before it moves anything, and every file once more where it
 * lands, in the install directory that only an administrator can write.
 */

// Proof is what a staged update is checked against: a release's checksum
// list, the signature over it, and the archive it lists.
type Proof struct {
	Checksums, Signature, Archive []byte
}

// The proof's names in the staging directory. None can collide with an
// install file, which are the only other names Stage writes.
const (
	proofChecksums = "checksums.txt"
	proofSignature = "checksums.txt.sig"
	proofArchive   = "release.archive"
)

// InstallFiles are the names an update may take out of a release archive and
// place in the install directory. Named rather than "everything in the zip":
// the archive also carries a README and a licence, and an updater that
// overwrites files nobody asked it to touch is an updater nobody trusts. Apply
// refuses any other name, so this list is also the bound on what a staging can
// do.
var InstallFiles = map[string]bool{
	// One program since ADR 0080. The two old names are gone from this list
	// on purpose: an archive is only ever unpacked into the install it was
	// built for, and staging a LANcast-Server.exe now would bring back a file
	// nothing runs.
	"LANcast.exe":        true,
	"WebView2Loader.dll": true,
	// The desktop player (ADR 0067/0069). Absent from an older archive, which
	// is fine: the map filters what the zip holds rather than demanding it.
	"libmpv-2.dll": true,
	// Achievements in the desktop player (ADR 0112); absent from older archives.
	"lancast-cheevos.dll": true,
	"lancast":             true,
}

// TakenFromArchive reports whether an archive entry is one an update installs:
// a bare name, and one of InstallFiles. A zip entry with a path is refused
// rather than flattened, because an archive is an untrusted container even
// when its digest is signed.
func TakenFromArchive(name string) bool {
	return name == path.Base(name) && InstallFiles[name]
}

/*
 * ArchiveName is the asset this platform installs from: goreleaser's
 * LANcast_<version>_<os>_<arch>, with the leading v stripped from the tag.
 *
 * Apply derives it from the staged version rather than reading it from the
 * staging, which binds the version to a signed name: a genuine older release
 * staged under a newer version's number does not match.
 *
 * Renamed from lancast_… with the single executable (ADR 0080 §5, amended),
 * and the rename is the point. Updaters from before it look for the old name
 * and keep only the files they know by name; an archive holding LANcast.exe
 * plus the two DLLs would have had them stage the DLLs alone, restart on the
 * old programs, and offer the same update for ever. Under a name they do not
 * look for, they stop with "release vX has no lancast_…zip" instead, and the
 * release notes say to run the installer — which that one release needs
 * anyway, to re-register the service under the new name.
 */
func ArchiveName(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("LANcast_%s_windows_%s.zip", v, runtime.GOARCH)
	}
	return fmt.Sprintf("LANcast_%s_%s_%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
}

// prove checks a proof for version and returns the SHA-256 of every install
// file in its archive: what each staged file must be.
func prove(version string, p Proof) (map[string]string, error) {
	v, err := release.VerifyChecksums(p.Checksums, p.Signature)
	if err != nil {
		return nil, err
	}
	if err := v.CheckArtifact(ArchiveName(version), p.Archive); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(p.Archive), int64(len(p.Archive)))
	if err != nil {
		return nil, fmt.Errorf("release archive: %w", err)
	}
	want := map[string]string{}
	for _, f := range zr.File {
		if !TakenFromArchive(f.Name) {
			continue
		}
		if _, dup := want[f.Name]; dup {
			return nil, fmt.Errorf("release archive: %s appears twice", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("release archive: %s: %w", f.Name, err)
		}
		h := sha256.New()
		_, err = io.Copy(h, rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("release archive: %s: %w", f.Name, err)
		}
		want[f.Name] = hex.EncodeToString(h.Sum(nil))
	}
	return want, nil
}

// checkNames refuses a file list that is not exactly the archive's install
// files: a name outside InstallFiles, one that could leave the install
// directory, a repeat, or a subset (half a release, with a program that does
// not match its DLLs).
func checkNames(names []string, want map[string]string) error {
	seen := map[string]bool{}
	for _, name := range names {
		if !safeStagedName(name) || !InstallFiles[name] {
			return fmt.Errorf("%q is not a file LANcast installs", name)
		}
		if seen[name] {
			return fmt.Errorf("%s is listed twice", name)
		}
		seen[name] = true
		if _, ok := want[name]; !ok {
			return fmt.Errorf("%s is not in the signed release", name)
		}
	}
	if len(seen) != len(want) {
		return fmt.Errorf("the staging holds %d of the release's %d files", len(seen), len(want))
	}
	return nil
}

// checkStaged is Apply's proof: everything the staging says, checked against
// the release's signature. It returns each file's expected digest, for the
// check after the move.
func checkStaged(staging string, m Manifest) (map[string]string, error) {
	var p Proof
	for name, dst := range map[string]*[]byte{
		proofChecksums: &p.Checksums, proofSignature: &p.Signature, proofArchive: &p.Archive,
	} {
		b, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			return nil, fmt.Errorf("the release's proof is missing: %w", err)
		}
		*dst = b
	}
	want, err := prove(m.Version, p)
	if err != nil {
		return nil, err
	}
	if err := checkNames(m.Files, want); err != nil {
		return nil, err
	}
	for _, name := range m.Files {
		got, err := fileDigest(filepath.Join(staging, name))
		if err != nil {
			return nil, fmt.Errorf("staged %s is missing: %w", name, err)
		}
		if got != want[name] {
			return nil, fmt.Errorf("staged %s is not the file the release holds", name)
		}
	}
	return want, nil
}

func fileDigest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
