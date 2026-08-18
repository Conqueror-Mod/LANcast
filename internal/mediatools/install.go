package mediatools

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

/*
 * Fetching the media tools on request (ADR 0043).
 *
 * ffmpeg is not bundled: this build is 160MB compressed and unpacks to nearly
 * 300MB, against a 17MB installer, and the majority of a library direct-plays
 * without ever needing it. It is also not fetched automatically — a media
 * server that contacts the internet without being asked has broken no
 * phone-home, and that principle has no convenience exception. Somebody presses
 * a button, having been told what is about to be downloaded and under which
 * licence.
 */

// ErrUnsupportedPlatform is returned where there is nothing sensible to fetch.
var ErrUnsupportedPlatform = errors.New("no pinned media-tools build for this platform")

// ErrChecksumMismatch is returned when the download is not the pinned build.
var ErrChecksumMismatch = errors.New("the download did not match its expected checksum")

/*
 * Source is one pinned build.
 *
 * Pinned to an exact release asset with an exact checksum, never to a "latest"
 * pointer. Following a moving target would mean playback behaviour changing
 * without a LANcast release — a new ffmpeg can change which files remux, which
 * encoders exist and what the argument list means — so a version bump is a code
 * change here on purpose.
 */
type Source struct {
	// URL is the exact asset. Not caller-supplied and not configurable: the
	// payload is an executable this server is about to run, and fetching an
	// address the request chose is the server-side request forgery the channel
	// endpoints already refuse.
	URL string
	// SHA256 is the hex digest of the archive, checked before anything is
	// unpacked.
	SHA256 string
	// SizeBytes lets the UI show a real total before the first byte arrives, and
	// lets a wrong-length download fail early rather than after 160MB.
	SizeBytes int64
	// Version, Licence and LicenceURL are shown before the download starts. A
	// download the user cannot identify is not consent.
	Version    string
	Licence    string
	LicenceURL string
}

/*
 * windowsAMD64 is the pinned Windows build.
 *
 * A **static GPL** build, and both halves are deliberate. Static because it
 * makes the install two self-contained files rather than a tree of DLLs whose
 * absence produces a loader error at playback time. GPL because that is the
 * build carrying x264 — without it there is no software H.264 encoder, and
 * transcoding would depend on a hardware encoder being present, turning a CPU
 * cost into a hardware requirement and making the same file playable on one
 * machine and not another.
 *
 * LANcast fetches this rather than redistributing it, so its own installer
 * carries no GPL obligation; the licence is named in the UI before the download
 * begins.
 */
var windowsAMD64 = Source{
	URL: "https://github.com/BtbN/FFmpeg-Builds/releases/download/" +
		"autobuild-2026-08-18-15-03/ffmpeg-n8.1.2-44-g7c533d0f86-win64-gpl-8.1.zip",
	SHA256:     "66e3797adad33063ae3f55c7eacb9f1bff604322a4e50225039626230fd0c0d1",
	SizeBytes:  168274317,
	Version:    "8.1.2 (n8.1.2-44-g7c533d0f86, win64 gpl static)",
	Licence:    "GPL v3",
	LicenceURL: "https://www.gnu.org/licenses/gpl-3.0.html",
}

/*
 * SourceForHost returns the build to fetch for this machine.
 *
 * Windows only, and that is not an oversight. Linux and macOS have package
 * managers that install ffmpeg better than this code will, keep it patched
 * afterwards, and put it somewhere Detect already looks. Downloading a tarball
 * behind their backs would be the worse option offered as the convenient one.
 */
func SourceForHost() (Source, error) {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return windowsAMD64, nil
	}
	return Source{}, ErrUnsupportedPlatform
}

// Stage names the step in progress, for a UI that has to say something truthful
// during a two-minute download.
type Stage string

const (
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageInstalling  Stage = "installing"
)

// Progress reports how far along an install is. BytesTotal is the pinned size,
// so it is known before the first byte rather than depending on a Content-Length
// the server may not send.
type Progress struct {
	Stage      Stage
	BytesDone  int64
	BytesTotal int64
}

// wanted are the binaries taken out of the archive. ffplay is deliberately left
// behind: it is another 146MB, it is a desktop player, and nothing in LANcast
// has ever invoked it.
var wanted = []string{"ffmpeg", "ffprobe"}

/*
 * Install downloads, verifies and unpacks the tools into dir.
 *
 * The ordering is the interesting part, and it is what makes a partial install
 * report as *absent* rather than as present-and-broken:
 *
 *  1. Download to a temporary file inside dir, so the later rename is on one
 *     volume and cannot fail halfway across a filesystem boundary.
 *  2. Hash while writing, and compare before opening the archive. An unverified
 *     archive is never unpacked, let alone executed.
 *  3. Unpack into a staging directory, then move the binaries into place with
 *     **ffprobe last** — because Detect and hasProbe key on ffprobe, so until
 *     the final rename lands the directory does not read as an install at all.
 *
 * Interrupt it anywhere and the worst outcome is wasted bytes.
 */
func Install(ctx context.Context, src Source, dir string, report func(Progress)) error {
	if report == nil {
		report = func(Progress) {}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create the tools directory: %w", err)
	}

	archive, err := download(ctx, src, dir, report)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	report(Progress{Stage: StageInstalling, BytesDone: src.SizeBytes, BytesTotal: src.SizeBytes})
	return unpack(archive, dir)
}

// download fetches the archive to a temporary file beside its destination,
// verifying the checksum as the bytes arrive rather than reading 160MB twice.
func download(ctx context.Context, src Source, dir string, report func(Progress)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return "", fmt.Errorf("could not request the download: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach the download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// A pin that has rotted fails here, loudly, which is the point of
		// pinning: it cannot quietly become a different build.
		return "", fmt.Errorf("the download answered %s", resp.Status)
	}

	tmp, err := os.CreateTemp(dir, ".mediatools-*.zip")
	if err != nil {
		return "", fmt.Errorf("could not create a temporary file: %w", err)
	}
	name := tmp.Name()
	// Removed by the caller on success; removed here on every failure path, so a
	// cancelled download does not leave 160MB behind.
	cleanup := func() {
		tmp.Close()
		os.Remove(name)
	}

	total := src.SizeBytes
	if total <= 0 {
		total = resp.ContentLength
	}
	sum := sha256.New()
	written, err := copyWithProgress(tmp, io.TeeReader(resp.Body, sum), total, report)
	if err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("could not finish writing the download: %w", err)
	}

	report(Progress{Stage: StageVerifying, BytesDone: written, BytesTotal: total})
	got := hex.EncodeToString(sum.Sum(nil))
	if !strings.EqualFold(got, src.SHA256) {
		os.Remove(name)
		return "", fmt.Errorf("%w: got %s", ErrChecksumMismatch, got)
	}
	return name, nil
}

// copyWithProgress streams the body, reporting as it goes and honouring
// cancellation between chunks. A 160MB download that cannot be cancelled is a
// button nobody should press.
func copyWithProgress(dst io.Writer, src io.Reader, total int64, report func(Progress)) (int64, error) {
	buf := make([]byte, 1<<20)
	var done int64
	var lastReported int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return done, fmt.Errorf("could not write the download: %w", werr)
			}
			done += int64(n)
			// Every few megabytes rather than every chunk: this is a polled
			// status, and 160 reports a second is noise the UI cannot use.
			if done-lastReported >= 4<<20 {
				lastReported = done
				report(Progress{Stage: StageDownloading, BytesDone: done, BytesTotal: total})
			}
		}
		if err == io.EOF {
			report(Progress{Stage: StageDownloading, BytesDone: done, BytesTotal: total})
			return done, nil
		}
		if err != nil {
			return done, fmt.Errorf("the download stopped early: %w", err)
		}
	}
}

/*
 * unpack takes the two binaries out of the archive and into dir.
 *
 * Entries are matched on their **base name only** and written to a path this
 * function builds, never to the path inside the archive. An archive that names
 * an entry `../../windows/system32/ffprobe.exe` therefore extracts to
 * `ffprobe.exe` in the tools directory like any other — the zip-slip class of
 * bug is absent by construction rather than by a check that has to be right.
 */
func unpack(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("the download is not a readable archive: %w", err)
	}
	defer zr.Close()

	staging, err := os.MkdirTemp(dir, ".staging-*")
	if err != nil {
		return fmt.Errorf("could not create a staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	found := map[string]string{}
	for _, f := range zr.File {
		/*
		 * Zip entry names are slash-delimited by the format's own spec, whatever
		 * platform wrote them — so `path.Base` after normalising backslashes,
		 * not `filepath.Base`. On Linux a backslash is an ordinary filename
		 * character, so filepath.Base of a Windows-style entry returns the whole
		 * string: the binary is not recognised, and the install fails claiming
		 * the archive lacks ffprobe. Found by the zip-slip test failing on the
		 * Linux runner while passing on Windows.
		 */
		base := path.Base(strings.ReplaceAll(f.Name, `\`, "/"))
		for _, w := range wanted {
			if !strings.EqualFold(base, exeName(w)) {
				continue
			}
			out := filepath.Join(staging, exeName(w))
			if err := extractFile(f, out); err != nil {
				return err
			}
			found[w] = out
		}
	}
	for _, w := range wanted {
		if found[w] == "" {
			return fmt.Errorf("the archive did not contain %s", exeName(w))
		}
	}

	/*
	 * ffprobe last, deliberately.
	 *
	 * Detect and hasProbe both key on ffprobe, so the directory does not read as
	 * an install until that final rename lands. An interrupted move leaves the
	 * tools reporting absent, which is recoverable by pressing the button again;
	 * reporting present with half a toolchain would turn one clear failure into
	 * an unbounded set of confusing ones.
	 */
	order := []string{"ffmpeg", "ffprobe"}
	for _, w := range order {
		dst := filepath.Join(dir, exeName(w))
		// Windows will not rename onto an existing file, and a reinstall over a
		// working install is the ordinary case rather than the exception.
		_ = os.Remove(dst)
		if err := os.Rename(found[w], dst); err != nil {
			return fmt.Errorf("could not put %s in place: %w", exeName(w), err)
		}
	}
	return nil
}

// extractFile writes one archive entry to an exact path, executable.
func extractFile(f *zip.File, out string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("could not read %s from the archive: %w", f.Name, err)
	}
	defer rc.Close()

	dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("could not write %s: %w", filepath.Base(out), err)
	}
	if _, err := io.Copy(dst, rc); err != nil {
		dst.Close()
		return fmt.Errorf("could not unpack %s: %w", filepath.Base(out), err)
	}
	return dst.Close()
}
