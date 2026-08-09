// Package release verifies that a downloaded LANcast build is one the project
// published.
//
// This is what makes automatic installation defensible. Auto-install means a
// process running as LocalSystem fetching a binary from the internet and
// executing it, which is a remote-code-execution path the moment the
// distribution channel is not trustworthy. A checksum published beside the
// download does not help: it proves the bytes arrived intact, not that they are
// the bytes the project produced, because it comes from the same place.
//
// So a release carries a signature over its checksums file, made with a key
// whose public half is compiled into this binary. Verification is offline and
// depends on nothing that was downloaded alongside it.
//
// The key is separate from the plugin project key (ADR 0021) on purpose. Plugin
// provenance and release provenance are different trust domains, and one key
// serving both means a compromise of either is a compromise of everything.
package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsigned means the release published no signature. It is deliberately
// distinct from a bad signature: one is a release made before signing existed,
// or by a build without the key, and the other is a file that should not be
// trusted. The first may be installed by hand; neither may be installed
// automatically.
var ErrUnsigned = errors.New("release: no signature published")

// ErrBadSignature means a signature was present and did not verify. Nothing
// should act on the download.
var ErrBadSignature = errors.New("release: signature does not verify")

// ErrDigestMismatch means the artifact does not match the signed checksum list.
var ErrDigestMismatch = errors.New("release: artifact does not match its signed checksum")

// Verified is a checksums file whose signature has been checked.
type Verified struct {
	digests map[string]string
}

// VerifyChecksums checks the signature over a release's checksums file.
//
// Both arguments come from the release; only the public key comes from this
// binary, which is the whole point — an attacker who can serve both files still
// cannot produce a signature.
func VerifyChecksums(checksums, signatureHex []byte) (*Verified, error) {
	pub := PublicKey()
	if pub == nil {
		// No key compiled in. Refusing is the only safe answer: a build that
		// cannot verify must not decide that everything verifies.
		return nil, fmt.Errorf("release: no release key in this build")
	}
	if len(signatureHex) == 0 {
		return nil, ErrUnsigned
	}
	sig, err := hex.DecodeString(strings.TrimSpace(string(signatureHex)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrBadSignature
	}
	if !ed25519.Verify(pub, checksums, sig) {
		return nil, ErrBadSignature
	}
	return &Verified{digests: parseChecksums(checksums)}, nil
}

// Digest returns the signed SHA-256 for one artifact, and whether it was listed.
func (v *Verified) Digest(name string) (string, bool) {
	d, ok := v.digests[name]
	return d, ok
}

// CheckArtifact confirms a downloaded artifact matches its signed digest.
//
// An artifact absent from the list is refused rather than allowed: the list is
// the statement of what the release contains, and something not in it is not
// part of the release.
func (v *Verified) CheckArtifact(name string, body []byte) error {
	want, ok := v.Digest(name)
	if !ok {
		return fmt.Errorf("release: %s is not in the signed checksum list", name)
	}
	sum := sha256.Sum256(body)
	if !strings.EqualFold(want, hex.EncodeToString(sum[:])) {
		return ErrDigestMismatch
	}
	return nil
}

// parseChecksums reads the "<hex>  <name>" format goreleaser writes.
//
// Unparseable lines are skipped rather than failing the file: an extra comment
// or a blank line is not a reason to reject a release, and a name that matters
// being absent is caught by CheckArtifact refusing it.
func parseChecksums(body []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		digest, name := fields[0], strings.TrimPrefix(fields[1], "*")
		if len(digest) != sha256.Size*2 {
			continue
		}
		out[name] = digest
	}
	return out
}
