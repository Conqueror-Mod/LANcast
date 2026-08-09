package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// withKey swaps the compiled-in key for a test one. The real constant is empty
// until the maintainer generates a keypair, so every meaningful test has to
// provide its own.
func withKey(t *testing.T) (ed25519.PrivateKey, func([]byte) []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	prev := activeKeyHex
	activeKeyHex = hex.EncodeToString(pub)
	t.Cleanup(func() { activeKeyHex = prev })

	sign := func(body []byte) []byte {
		return []byte(hex.EncodeToString(ed25519.Sign(priv, body)) + "\n")
	}
	return priv, sign
}

func checksumsFor(name string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
}

func TestVerifiesAndMatchesAnArtifact(t *testing.T) {
	_, sign := withKey(t)
	artifact := []byte("the installer bytes")
	checks := checksumsFor("LANcast-Setup-9.9.9.exe", artifact)

	v, err := VerifyChecksums(checks, sign(checks))
	if err != nil {
		t.Fatalf("VerifyChecksums: %v", err)
	}
	if err := v.CheckArtifact("LANcast-Setup-9.9.9.exe", artifact); err != nil {
		t.Errorf("CheckArtifact: %v", err)
	}
}

// The attack this exists to stop: someone serves both the download and a
// checksums file that matches it. Without a signature that is indistinguishable
// from a real release.
func TestTamperedChecksumsAreRefused(t *testing.T) {
	_, sign := withKey(t)
	real := checksumsFor("LANcast-Setup-9.9.9.exe", []byte("the installer bytes"))
	sig := sign(real)

	tampered := checksumsFor("LANcast-Setup-9.9.9.exe", []byte("something else entirely"))
	if _, err := VerifyChecksums(tampered, sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("VerifyChecksums on a swapped list = %v, want ErrBadSignature", err)
	}
}

func TestArtifactThatDoesNotMatchIsRefused(t *testing.T) {
	_, sign := withKey(t)
	checks := checksumsFor("LANcast-Setup-9.9.9.exe", []byte("the installer bytes"))
	v, err := VerifyChecksums(checks, sign(checks))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.CheckArtifact("LANcast-Setup-9.9.9.exe", []byte("swapped")); !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("CheckArtifact on swapped bytes = %v, want ErrDigestMismatch", err)
	}
}

// Something not in the signed list is not part of the release. Allowing it
// would let an extra file be appended to a release and installed.
func TestUnlistedArtifactIsRefused(t *testing.T) {
	_, sign := withKey(t)
	checks := checksumsFor("LANcast-Setup-9.9.9.exe", []byte("the installer bytes"))
	v, _ := VerifyChecksums(checks, sign(checks))

	if err := v.CheckArtifact("something-else.exe", []byte("x")); err == nil {
		t.Error("an artifact absent from the signed list was accepted")
	}
}

// A release from before signing existed is distinguishable from a bad one. It
// may be installed by hand; it may never be installed automatically.
func TestUnsignedIsItsOwnAnswer(t *testing.T) {
	_, _ = withKey(t)
	checks := checksumsFor("x.exe", []byte("y"))
	if _, err := VerifyChecksums(checks, nil); !errors.Is(err, ErrUnsigned) {
		t.Errorf("missing signature = %v, want ErrUnsigned", err)
	}
}

// The case that decides whether this is a safety feature or decoration: a build
// with no key compiled in must refuse, not wave everything through.
func TestABuildWithNoKeyRefuses(t *testing.T) {
	prev := activeKeyHex
	activeKeyHex = ""
	t.Cleanup(func() { activeKeyHex = prev })

	if Signable() {
		t.Error("Signable() is true with no key")
	}
	checks := checksumsFor("x.exe", []byte("y"))
	if _, err := VerifyChecksums(checks, []byte("00")); err == nil {
		t.Error("a build with no key accepted a release; it must refuse")
	}
}

func TestParseChecksumsSkipsNoise(t *testing.T) {
	body := []byte("# a comment\n\n" +
		hex.EncodeToString(make([]byte, 32)) + "  real.exe\n" +
		"short  bogus.exe\n")
	got := parseChecksums(body)
	if len(got) != 1 {
		t.Fatalf("parsed %d entries, want 1: %v", len(got), got)
	}
	if _, ok := got["real.exe"]; !ok {
		t.Error("the valid line was not parsed")
	}
}
