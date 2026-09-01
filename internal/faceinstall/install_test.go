package faceinstall

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
	"testing"
)

/*
 * Fetching the face models, with no network involved.
 *
 * Everything here serves its own bytes from an httptest server, because the
 * properties worth testing are about what this code does with a payload rather
 * than about whether GitHub is up: that a wrong digest is refused, that a
 * refused download leaves nothing behind under a real name, and that the
 * runtime is extracted from the archive it ships inside.
 *
 * The pinned URLs and digests are checked separately, by being pinned — a test
 * that fetched them would be testing the internet.
 */

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func serve(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAVerifiedDownloadIsInstalled(t *testing.T) {
	body := []byte("pretend this is an ONNX model")
	srv := serve(t, body)
	dir := t.TempDir()

	a := Asset{Name: "model.onnx", URL: srv.URL, SHA256: digest(body),
		SizeBytes: int64(len(body))}
	if err := Install(context.Background(), []Asset{a}, dir, nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "model.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Error("the installed file is not what was served")
	}
}

/*
 * The check that matters.
 *
 * A model is a file this server loads and a runtime is a library it executes.
 * A wrong digest is refused, and refused *before* anything appears under its
 * real name — a half-written or substituted model does not fail loudly, it
 * produces embeddings that are simply wrong, and every face in the library
 * would be grouped against them.
 */
func TestAWrongChecksumIsRefused(t *testing.T) {
	srv := serve(t, []byte("not what was expected"))
	dir := t.TempDir()

	a := Asset{Name: "model.onnx", URL: srv.URL,
		SHA256: digest([]byte("something else")), SizeBytes: 21}
	err := Install(context.Background(), []Asset{a}, dir, nil)
	if err == nil {
		t.Fatal("a mismatched download was accepted")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("error was %v, want a checksum mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); !os.IsNotExist(err) {
		t.Error("the file exists under its real name after a failed verification")
	}
}

// And nothing is left lying about: a directory full of .part files after a
// failed install is a directory somebody has to clean up by hand.
func TestAFailedDownloadLeavesNoDebris(t *testing.T) {
	srv := serve(t, []byte("wrong"))
	dir := t.TempDir()

	a := Asset{Name: "model.onnx", URL: srv.URL, SHA256: digest([]byte("right")), SizeBytes: 5}
	_ = Install(context.Background(), []Asset{a}, dir, nil)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("left behind: %s", e.Name())
	}
}

/*
 * The runtime arrives inside a zip whose top directory carries a version, so
 * the entry is matched on its suffix rather than on the whole path — otherwise
 * the URL and the path would have to change together, which is one of them
 * being forgotten.
 */
func TestTheRuntimeIsExtractedFromItsArchive(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("onnxruntime-win-x64-9.9.9/lib/onnxruntime.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("MZ pretend library")); err != nil {
		t.Fatal(err)
	}
	// A decoy with the right base name in the wrong place, which a match on
	// base name alone would happily take.
	other, _ := zw.Create("onnxruntime-win-x64-9.9.9/doc/onnxruntime.dll.txt")
	_, _ = other.Write([]byte("not the library"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := serve(t, buf.Bytes())
	dir := t.TempDir()
	a := Asset{
		Name: "onnxruntime.dll", URL: srv.URL, SHA256: digest(buf.Bytes()),
		SizeBytes: int64(buf.Len()), ExtractFromZip: "lib/onnxruntime.dll",
	}
	if err := Install(context.Background(), []Asset{a}, dir, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "onnxruntime.dll"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "MZ pretend library" {
		t.Errorf("extracted %q", got)
	}
}

// Progress is reported against the total of everything, not per file: somebody
// watching wants to know when it will finish, not which of three files is in
// flight.
func TestProgressCountsEveryAsset(t *testing.T) {
	one := []byte("first payload")
	two := []byte("second payload, longer")
	s1, s2 := serve(t, one), serve(t, two)
	dir := t.TempDir()

	assets := []Asset{
		{Name: "a.onnx", URL: s1.URL, SHA256: digest(one), SizeBytes: int64(len(one))},
		{Name: "b.onnx", URL: s2.URL, SHA256: digest(two), SizeBytes: int64(len(two))},
	}
	want := TotalBytes(assets)

	var maxDone int64
	var sawTotal int64
	err := Install(context.Background(), assets, dir, func(p Progress) {
		if p.BytesDone > maxDone {
			maxDone = p.BytesDone
		}
		sawTotal = p.BytesTotal
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawTotal != want {
		t.Errorf("reported a total of %d, want %d", sawTotal, want)
	}
	if maxDone != want {
		t.Errorf("progress reached %d of %d", maxDone, want)
	}
}

// Installed answers about the real names, so a UI can offer an install rather
// than a re-install — and does not count a zero-length file as present.
func TestInstalledIsAboutCompleteFiles(t *testing.T) {
	dir := t.TempDir()
	if Installed(dir) {
		t.Error("reported installed on an empty directory")
	}
	for _, a := range models {
		if err := os.WriteFile(filepath.Join(dir, a.Name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if Installed(dir) {
		t.Error("reported installed with the runtime missing")
	}
	if err := os.WriteFile(filepath.Join(dir, "onnxruntime.dll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Installed(dir) {
		t.Error("an empty file counted as an installed runtime")
	}
	if err := os.WriteFile(filepath.Join(dir, "onnxruntime.dll"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Installed(dir) {
		t.Error("reported not installed with everything present")
	}
}

/*
 * The pinned set is stated, not assembled by hand at the call site.
 *
 * This is the list somebody reads before consenting to a download, so every
 * entry has to carry a size and a licence — an asset with neither is a download
 * nobody can identify, and a download nobody can identify is not consent.
 */
func TestEveryPinnedAssetCanBeDescribed(t *testing.T) {
	all := append(append([]Asset{}, models...), runtimeWindowsAMD64)
	for _, a := range all {
		if a.Name == "" || a.URL == "" {
			t.Errorf("asset %+v is missing a name or URL", a)
		}
		if len(a.SHA256) != 64 {
			t.Errorf("%s has no usable digest", a.Name)
		}
		if a.SizeBytes <= 0 {
			t.Errorf("%s has no size, so no progress bar can be honest", a.Name)
		}
		if a.Licence == "" || a.LicenceURL == "" {
			t.Errorf("%s does not name its licence", a.Name)
		}
	}
	// And the models are pinned to a commit rather than a branch: a moving ref
	// would let the bytes change under a fixed digest, which breaks every new
	// install while every existing one carries on working.
	for _, a := range models {
		if !contains(a.URL, zooCommit) {
			t.Errorf("%s is not pinned to a commit", a.Name)
		}
		if contains(a.URL, "/main/") {
			t.Errorf("%s points at a moving ref", a.Name)
		}
	}
}

func contains(s, sub string) bool {
	return bytes.Contains([]byte(s), []byte(sub))
}
