package mediatools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

/*
 * The installer's refusals.
 *
 * Downloading and unpacking is the easy half. What is worth testing is what it
 * declines to do: run an archive it has not verified, trust a path from inside
 * one, or leave a directory that reports as installed when it is not.
 */

// zipWith builds an archive containing the named entries, each holding body.
// Paths are as given, so a test can put an entry wherever it likes — including
// somewhere it should not.
func zipWith(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serve hosts a body and returns a Source pinned to it, with the correct digest.
func serve(t *testing.T, body []byte) (Source, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return Source{
		URL:       srv.URL + "/ffmpeg.zip",
		SHA256:    hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(body)),
		Version:   "test",
		Licence:   "GPL v3",
	}, srv
}

// realLayout mirrors the pinned build: the binaries sit two directories deep,
// beside an ffplay nobody asked for.
func realLayout() map[string]string {
	return map[string]string{
		"ffmpeg-n8.1.2-win64-gpl/bin/" + exeName("ffmpeg"):  "FFMPEG-BODY",
		"ffmpeg-n8.1.2-win64-gpl/bin/" + exeName("ffprobe"): "FFPROBE-BODY",
		"ffmpeg-n8.1.2-win64-gpl/bin/" + exeName("ffplay"):  "FFPLAY-BODY",
		"ffmpeg-n8.1.2-win64-gpl/LICENSE":                   "gpl",
	}
}

func TestInstallTakesTheToolsOutOfANestedArchive(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	dir := t.TempDir()

	if err := Install(context.Background(), src, dir, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ffmpeg", "ffprobe"} {
		body, err := os.ReadFile(filepath.Join(dir, exeName(want)))
		if err != nil {
			t.Fatalf("%s missing after install: %v", want, err)
		}
		if !strings.Contains(string(body), strings.ToUpper(want)) {
			t.Errorf("%s holds the wrong bytes: %q", want, body)
		}
	}
	// And the directory now reads as an install to the code that decides that.
	if !hasProbe(dir) {
		t.Error("hasProbe false after a successful install")
	}
}

// ffplay is another 146MB of desktop player that nothing here invokes.
func TestInstallLeavesFfplayBehind(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	dir := t.TempDir()
	if err := Install(context.Background(), src, dir, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, exeName("ffplay"))); err == nil {
		t.Error("ffplay was installed; only ffmpeg and ffprobe are wanted")
	}
}

/*
 * The gate that matters: an archive whose bytes are not the pinned bytes is
 * never unpacked, so nothing unverified is ever written where the server will
 * execute it.
 */
func TestInstallRefusesAnArchiveThatIsNotThePinnedOne(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	src.SHA256 = strings.Repeat("0", 64) // pin something else

	dir := t.TempDir()
	err := Install(context.Background(), src, dir, nil)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want a checksum mismatch", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("directory is not empty after a rejected download: %v", entries)
	}
	if hasProbe(dir) {
		t.Error("a rejected download left the directory reading as installed")
	}
}

/*
 * Zip slip, absent by construction.
 *
 * Entries are matched on base name and written to a path this package builds, so
 * an entry claiming to be `../../../ffprobe.exe` lands in the tools directory
 * like any other. The assertion is that nothing appears outside dir.
 */
func TestInstallIgnoresPathsFromInsideTheArchive(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "tools")

	src, _ := serve(t, zipWith(t, map[string]string{
		"../../../../" + exeName("ffmpeg"):  "FFMPEG-BODY",
		"..\\..\\..\\" + exeName("ffprobe"): "FFPROBE-BODY",
	}))
	if err := Install(context.Background(), src, dir, nil); err != nil {
		t.Fatal(err)
	}

	if !hasProbe(dir) {
		t.Error("the tools did not land in the tools directory")
	}
	// Nothing escaped upward.
	for _, name := range []string{exeName("ffmpeg"), exeName("ffprobe")} {
		if _, err := os.Stat(filepath.Join(parent, name)); err == nil {
			t.Errorf("%s escaped into the parent directory", name)
		}
	}
}

// An archive missing what we came for fails before touching the destination,
// rather than installing half a toolchain.
func TestInstallRejectsAnArchiveWithoutFfprobe(t *testing.T) {
	src, _ := serve(t, zipWith(t, map[string]string{
		"bin/" + exeName("ffmpeg"): "FFMPEG-BODY",
	}))
	dir := t.TempDir()

	err := Install(context.Background(), src, dir, nil)
	if err == nil || !strings.Contains(err.Error(), exeName("ffprobe")) {
		t.Fatalf("error = %v, want a complaint about the missing ffprobe", err)
	}
	if hasProbe(dir) {
		t.Error("directory reads as installed after a failed install")
	}
}

// A cancelled install leaves nothing behind, including the 160MB it was part
// way through writing.
func TestCancelledInstallLeavesNothing(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Install(ctx, src, dir, nil); err == nil {
		t.Fatal("a cancelled install reported success")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("cancelled install left %v behind", entries)
	}
}

// Reinstalling over a working install is ordinary, not exceptional — and on
// Windows a rename will not overwrite, so this is the case that catches it.
func TestInstallOverwritesAnExistingInstall(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	dir := t.TempDir()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if err := os.WriteFile(filepath.Join(dir, exeName(name)), []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := Install(context.Background(), src, dir, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, exeName("ffprobe")))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "OLD" {
		t.Error("the existing ffprobe was not replaced")
	}
}

// Progress has to reach the total, or a UI bar stops short of the end on a
// download that actually finished.
func TestProgressReachesTheTotal(t *testing.T) {
	body := zipWith(t, realLayout())
	src, _ := serve(t, body)
	dir := t.TempDir()

	var last Progress
	stages := map[Stage]bool{}
	if err := Install(context.Background(), src, dir, func(p Progress) {
		last = p
		stages[p.Stage] = true
	}); err != nil {
		t.Fatal(err)
	}
	if !stages[StageDownloading] || !stages[StageVerifying] || !stages[StageInstalling] {
		t.Errorf("stages reported = %v, want all three", stages)
	}
	if last.BytesDone != last.BytesTotal {
		t.Errorf("final progress %d/%d, want them equal", last.BytesDone, last.BytesTotal)
	}
}

// The pinned source is only offered where there is something pinned, and it
// carries what the UI has to show before downloading 160MB.
func TestSourceForHostIsPinnedAndDescribed(t *testing.T) {
	src, err := SourceForHost()
	if errors.Is(err, ErrUnsupportedPlatform) {
		t.Skip("no pinned build for this platform, which is the documented case")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(src.SHA256) != 64 {
		t.Errorf("checksum %q is not a sha256 digest", src.SHA256)
	}
	if src.SizeBytes <= 0 {
		t.Error("no pinned size, so a UI cannot show a total before downloading")
	}
	for _, field := range []string{src.Version, src.Licence, src.LicenceURL} {
		if field == "" {
			t.Error("a download the user cannot identify is not consent")
		}
	}
	// Not a moving pointer: "latest" would mean playback behaviour changing
	// without a LANcast release.
	if strings.Contains(src.URL, "/latest/") {
		t.Error("the pinned URL follows a moving target")
	}
}

/*
 * An archive written with Windows separators unpacks on any platform.
 *
 * Zip entry names are slash-delimited by the format's spec, but archives in the
 * wild are not always written that way -- and on Linux a backslash is an
 * ordinary filename character, so filepath.Base of "bin\\ffprobe.exe" is the
 * whole string and matches nothing. The install then fails claiming the archive
 * lacks ffprobe, which is a confusing way to describe a separator.
 *
 * Caught by the Linux runner while Windows passed, which is why it is a test of
 * its own rather than a line in the zip-slip one.
 */
func TestBackslashEntriesAreRecognisedOnEveryPlatform(t *testing.T) {
	src, _ := serve(t, zipWith(t, map[string]string{
		`ffmpeg-build\bin\` + exeName("ffmpeg"):  "FFMPEG-BODY",
		`ffmpeg-build\bin\` + exeName("ffprobe"): "FFPROBE-BODY",
	}))
	dir := t.TempDir()

	if err := Install(context.Background(), src, dir, nil); err != nil {
		t.Fatal(err)
	}
	if !hasProbe(dir) {
		t.Error("a Windows-separated archive did not install")
	}
}

/*
 * A download that goes silent is abandoned, not waited on for ever.
 *
 * The reported symptom was "it hangs at 0%", and http.DefaultClient is why:
 * it has no timeout of any kind, so a connection that accepts the request and
 * then sends nothing sits there indefinitely with nothing in the log. There is
 * deliberately no *total* timeout -- 160MB over a slow line is legitimately
 * minutes, and a wall clock cannot tell that from a hang -- so what is detected
 * is silence.
 */
func TestAStalledDownloadIsAbandonedWithAnError(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the stall watchdog")
	}
	// A server that sends a byte and then nothing, holding the connection open.
	held := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		<-held
	}))
	/*
	 * Cleanups run last-registered-first, so the release of the handler must be
	 * registered *after* the server: httptest.Server.Close waits for handlers to
	 * return, and closing it while this one is parked on `held` deadlocks the
	 * test binary. Learned by hanging a ten-minute test run.
	 */
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(held) })

	src := Source{URL: srv.URL, SHA256: strings.Repeat("0", 64), SizeBytes: 1000000}

	// The watchdog's own timeout is a minute, which is too long for a test, so
	// this asserts the wiring by cancelling instead: the same path, the same
	// cleanup, and no 160MB left behind.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	dir := t.TempDir()
	err := Install(ctx, src, dir, nil)
	if err == nil {
		t.Fatal("a stalled download reported success")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("a stalled download left %v behind", entries)
	}
}

// The watchdog has to be long enough that a slow but working download is never
// killed. A minute of complete silence is stuck; ten seconds is a bad line.
func TestStallTimeoutIsGenerous(t *testing.T) {
	if stallTimeout < 30*time.Second {
		t.Errorf("stall timeout %s is short enough to kill a slow download", stallTimeout)
	}
}

// Progress is reported before any bytes arrive, so a UI can show a total and a
// stage immediately -- an empty state cannot be told apart from a hang.
func TestProgressIsReportedBeforeTheFirstByte(t *testing.T) {
	src, _ := serve(t, zipWith(t, realLayout()))
	dir := t.TempDir()

	var first Progress
	seen := false
	if err := Install(context.Background(), src, dir, func(p Progress) {
		if !seen {
			first, seen = p, true
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !seen || first.BytesDone != 0 || first.BytesTotal == 0 {
		t.Errorf("first report = %+v, want a zeroed one carrying the total", first)
	}
}
