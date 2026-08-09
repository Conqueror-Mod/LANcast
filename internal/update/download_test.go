package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lancast/internal/release"
	"lancast/internal/selfupdate"
)

// fakeRelease serves a release the way GitHub does: a JSON document with asset
// URLs, a checksums file, a detached signature, and the archive itself.
type fakeRelease struct {
	tag      string
	archive  []byte
	checks   []byte
	sig      []byte
	omitSig  bool
	srv      *httptest.Server
	archName string
}

func newFakeRelease(t *testing.T, tag string, files map[string][]byte) *fakeRelease {
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

	f := &fakeRelease{tag: tag, archive: archive, archName: archiveName(tag)}
	sum := sha256.Sum256(archive)
	f.checks = []byte(hex.EncodeToString(sum[:]) + "  " + f.archName + "\n")
	return f
}

// sign installs a keypair into the release package and signs the checksums.
func (f *fakeRelease) sign(t *testing.T) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release.SetKeyForTest(hex.EncodeToString(pub)))
	f.sig = []byte(hex.EncodeToString(ed25519.Sign(priv, f.checks)) + "\n")
}

func (f *fakeRelease) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) { w.Write(f.checks) })
	mux.HandleFunc("/checksums.txt.sig", func(w http.ResponseWriter, r *http.Request) { w.Write(f.sig) })
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(f.archive) })
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		assets := []map[string]string{
			{"name": "checksums.txt", "browser_download_url": base + "/checksums.txt"},
			{"name": f.archName, "browser_download_url": base + "/archive"},
		}
		if !f.omitSig {
			assets = append(assets, map[string]string{
				"name": "checksums.txt.sig", "browser_download_url": base + "/checksums.txt.sig"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "assets": assets})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f.srv.URL + "/release"
}

// The whole path: look up, verify, download, check the digest, extract, stage.
func TestDownloadVerifiesAndStages(t *testing.T) {
	f := newFakeRelease(t, "v9.9.9", map[string][]byte{
		"LANcast-Server.exe": []byte("new server"),
		"LANcast-Client.exe": []byte("new client"),
		"README.md":          []byte("not an install file"),
	})
	f.sign(t)
	endpoint := f.start(t)

	data := t.TempDir()
	c := New("0.6.1")
	if err := c.downloadFrom(context.Background(), data, endpoint); err != nil {
		t.Fatalf("DownloadAndStage: %v", err)
	}

	m, ok := selfupdate.Pending(data)
	if !ok || m.Version != "v9.9.9" {
		t.Fatalf("staged = %+v, %v", m, ok)
	}
	// The README is in the archive and must not be staged: an updater that
	// overwrites files nobody asked it to touch is one nobody trusts.
	for _, name := range m.Files {
		if name == "README.md" {
			t.Error("README.md was staged")
		}
	}
	if len(m.Files) != 2 {
		t.Errorf("staged %v, want the two executables", m.Files)
	}
}

// The attack the signature exists to stop: an archive swapped for another,
// with a checksums file that matches it.
func TestDownloadRefusesATamperedArchive(t *testing.T) {
	f := newFakeRelease(t, "v9.9.9", map[string][]byte{"LANcast-Server.exe": []byte("real")})
	f.sign(t)
	// Swap the archive after signing, so the signature covers the old digest.
	f.archive = []byte("malicious payload")
	endpoint := f.start(t)

	data := t.TempDir()
	c := New("0.6.1")
	err := c.downloadFrom(context.Background(), data, endpoint)
	if err == nil {
		t.Fatal("a tampered archive was accepted")
	}
	if !strings.Contains(err.Error(), "checksum") && !strings.Contains(err.Error(), "match") {
		t.Errorf("error = %v; it should name the digest mismatch", err)
	}
	if _, ok := selfupdate.Pending(data); ok {
		t.Error("a tampered archive was staged")
	}
}

// An unsigned release is refused for automatic installation. It remains
// installable by hand, which is a different path.
func TestDownloadRefusesAnUnsignedRelease(t *testing.T) {
	f := newFakeRelease(t, "v9.9.9", map[string][]byte{"LANcast-Server.exe": []byte("x")})
	f.sign(t)
	f.omitSig = true
	endpoint := f.start(t)

	data := t.TempDir()
	c := New("0.6.1")
	err := c.downloadFrom(context.Background(), data, endpoint)
	if err == nil || !strings.Contains(err.Error(), "not signed") {
		t.Fatalf("err = %v, want a refusal naming the missing signature", err)
	}
	if _, ok := selfupdate.Pending(data); ok {
		t.Error("an unsigned release was staged")
	}
}

// A build that cannot verify must refuse before it downloads anything at all.
func TestDownloadRefusesWhenTheBuildCannotVerify(t *testing.T) {
	t.Cleanup(release.SetKeyForTest(""))
	c := New("0.6.1")
	err := c.DownloadAndStage(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// Nothing newer is not an error condition worth staging for.
func TestDownloadDeclinesAnOlderRelease(t *testing.T) {
	f := newFakeRelease(t, "v0.1.0", map[string][]byte{"LANcast-Server.exe": []byte("x")})
	f.sign(t)
	endpoint := f.start(t)

	c := New("9.9.9")
	err := c.downloadFrom(context.Background(), t.TempDir(), endpoint)
	if err == nil || !strings.Contains(err.Error(), "no newer release") {
		t.Fatalf("err = %v, want a refusal to downgrade", err)
	}
}

func TestArchiveNameMatchesGoreleaser(t *testing.T) {
	got := archiveName("v0.6.1")
	if !strings.HasPrefix(got, "lancast_0.6.1_") {
		t.Errorf("archiveName = %q; the leading v must be stripped", got)
	}
	if strings.Contains(got, "vv") {
		t.Errorf("archiveName = %q", got)
	}
}
