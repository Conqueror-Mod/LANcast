package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCreatesOnFirstRunAndIsStableAfter(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if _, err := os.Stat(KeyPath(dir)); err != nil {
		t.Fatalf("no key persisted: %v", err)
	}

	// The point of the whole package: the second call is the same server.
	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate again: %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Error("the identity changed across a restart")
	}
}

// Two data directories are two servers. This is the half of ADR 0044 that says
// identity belongs to the data directory.
func TestTwoDataDirectoriesAreTwoServers(t *testing.T) {
	a, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("two data directories produced one identity")
	}
}

/*
 * The rule that separates this package from tlscert.
 *
 * tlscert treats a corrupt certificate as a cache miss and generates a new one,
 * correctly, so a server does not die of a bad file. If this did the same, the
 * server would come back as a different peer and every pin would break with
 * nothing to explain it — which is the argument ADR 0044 gives for not simply
 * pinning the TLS certificate. So: an error, and the bad file left alone.
 */
func TestCorruptKeyIsAnErrorAndIsNotReplaced(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"not pem", []byte("this is not a key")},
		{"empty", []byte("")},
		{"wrong pem type", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2, 3}})},
		{"pem with rubbish inside", pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: []byte{1, 2, 3}})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(Dir(dir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(KeyPath(dir), tc.data, 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := LoadOrCreate(dir); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}

			// And the original bytes are still there. Silently overwriting the
			// evidence would destroy the only copy of whatever went wrong.
			after, err := os.ReadFile(KeyPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(tc.data) {
				t.Error("the unreadable key was overwritten")
			}
		})
	}
}

// An Ed25519 build must not silently adopt a key of another algorithm.
func TestWrongAlgorithmIsCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	// An X25519-shaped mistake is unlikely; an RSA key copied in by hand is not.
	rsa, err := x509.MarshalPKCS8PrivateKey(genRSA(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(KeyPath(dir), pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: rsa}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestFingerprintShape(t *testing.T) {
	id, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp := id.Fingerprint()

	// 256 bits at 5 bits per character, unpadded.
	if len(fp) != 52 {
		t.Errorf("fingerprint is %d characters, want 52: %q", len(fp), fp)
	}
	// Base32's alphabet and nothing else — no case to get wrong, no punctuation
	// to mishear. A stray '=' would mean padding crept back in.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	for _, r := range fp {
		if !strings.ContainsRune(alphabet, r) {
			t.Fatalf("fingerprint contains %q, outside the base32 alphabet: %q", r, fp)
		}
	}
}

// The fingerprint is of the public key, so anybody holding it computes the same
// value without the private half.
func TestFingerprintOfMatchesTheIdentity(t *testing.T) {
	id, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := FingerprintOf(id.Public()), id.Fingerprint(); got != want {
		t.Errorf("FingerprintOf = %s, identity says %s", got, want)
	}
}

func TestGroupingIsCosmeticAndReversible(t *testing.T) {
	id, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp := id.Fingerprint()
	grouped := id.Grouped()

	if grouped == fp {
		t.Error("Grouped added no separators")
	}
	if strings.Count(grouped, "-") != len(fp)/groupSize-1 {
		t.Errorf("grouped = %q, unexpected separator count", grouped)
	}
	if Normalize(grouped) != fp {
		t.Errorf("Normalize(%q) = %q, want %q", grouped, Normalize(grouped), fp)
	}
}

// What somebody actually does with a fingerprint read off a screen.
func TestNormalizeAcceptsWhatPeopleType(t *testing.T) {
	id, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp := id.Fingerprint()
	g := id.Grouped()

	for name, typed := range map[string]string{
		"grouped":           g,
		"lower case":        strings.ToLower(g),
		"spaces not dashes": strings.ReplaceAll(g, "-", " "),
		"colons":            strings.ReplaceAll(g, "-", ":"),
		"run together":      fp,
		"leading trailing":  "  " + g + "\n",
		"mixed separators":  strings.Replace(g, "-", " ", 3),
	} {
		if got := Normalize(typed); got != fp {
			t.Errorf("%s: Normalize = %q, want %q", name, got, fp)
		}
	}
}

// The key is secret, and the permissions are part of that claim.
//
// Skipped on Windows rather than softened to pass there: Go reports 0666 for any
// readable file on NTFS because access is an ACL question, so an assertion that
// held everywhere would have to accept 0666 and would then prove nothing on the
// platform where it means something. CI builds on Linux, which is where this
// runs for real.
func TestKeyFileIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not the access control mechanism on Windows")
	}
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(KeyPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("key mode is %#o, want no group or other bits", mode)
	}
}

// A half-written key must not become a new identity on the next start. The temp
// file is the mechanism; this asserts it does not survive as the real one.
func TestNoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("left a temp file behind: %s", e.Name())
		}
	}
}

// Signer is the only way out for the private half, and it must be the same key.
func TestSignerIsTheIdentitysKey(t *testing.T) {
	id, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("a message this server stands behind")
	sig := ed25519.Sign(id.Signer().(ed25519.PrivateKey), msg)
	if !ed25519.Verify(id.Public(), msg, sig) {
		t.Error("a signature from Signer does not verify against Public")
	}
}

func genRSA(t *testing.T) any {
	t.Helper()
	// 1024 is deliberately small: this key is never used for anything, and a
	// 2048-bit generation is a visible fraction of the package's test time.
	k, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
