// Package identity is the server's own cryptographic identity: an Ed25519
// keypair that says which LANcast this is, and a fingerprint of its public half
// that a person can read down a phone.
//
// This is [ADR 0044](../../docs/adr/0044-server-identity-and-peering.md), and it
// exists because until now nothing had ever needed to ask a running server who
// it was. Two households cannot watch anything together until each can prove,
// on every connection, that it is the one that was introduced.
//
// # Why not the TLS certificate
//
// [ADR 0014](../../docs/adr/0014-transport-security.md) already gives the server
// a certificate, and internal/certpin already pins one locally, so a second
// keypair looks like duplication. It is not, and the reason is the difference in
// *lifetime*: tlscert deliberately treats a missing, corrupt, or aging
// certificate as a cache miss and generates a new one, on the sound reasoning
// that a server should not die because of one bad file. A serving certificate
// may be replaced whenever it needs to be — the bring-your-own-cert path exists
// precisely so an operator can rotate it.
//
// An identity cannot work that way. If it regenerated on a bad read, the server
// would quietly have become somebody else and every peer's pin would break with
// nothing to explain it. So the two rules here are the opposite of tlscert's,
// and they are the whole point of this package:
//
//   - A key is generated **only** when none exists.
//   - Anything else — unreadable, malformed, the wrong algorithm — is an
//     **error**, never a reason to make a new one.
//
// # Secrecy
//
// The private key never leaves this package: not through the API, not into a
// log line, and not into a crash report. This project ships crash reporting,
// and a key that reaches one is a key published to whoever reads it. Only the
// public half and the fingerprint are exported.
package identity

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base32"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrCorrupt means key material exists and could not be understood. It is
// deliberately distinct from "no identity yet": the first is a fault to report
// and the second is the ordinary first-run state.
var ErrCorrupt = errors.New("the server identity on disk could not be read")

const (
	dirName = "identity"
	keyFile = "key.pem"
	// PKCS#8, which is what x509.MarshalPKCS8PrivateKey emits for Ed25519 and
	// what the standard "PRIVATE KEY" label means.
	pemType = "PRIVATE KEY"

	// groupSize is how many fingerprint characters sit between separators.
	// Four, because the fingerprint's job is to be *checked* out loud against
	// an invite somebody pasted, and four is the run length people read back
	// without losing their place.
	groupSize = 4
)

// fpEncoding is RFC 4648 base32 without padding: A–Z and 2–7 only.
//
// Base32 rather than base64 or hex. Hex would be 64 characters for the same
// 256 bits, and base64 is case-sensitive and includes '+' and '/', which is
// miserable to read aloud or retype. Base32's alphabet has no case to get wrong
// and no punctuation to mishear.
var fpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Identity is this server's keypair. Copyable; the private key is unexported so
// no caller outside this package can put it anywhere.
type Identity struct {
	priv ed25519.PrivateKey
}

// Dir is where a data directory keeps its identity.
func Dir(dataDir string) string { return filepath.Join(dataDir, dirName) }

// KeyPath is the key file for a data directory.
func KeyPath(dataDir string) string { return filepath.Join(Dir(dataDir), keyFile) }

// Public is the half that may be shared, and the half a peer pins.
func (i Identity) Public() ed25519.PublicKey {
	return i.priv.Public().(ed25519.PublicKey)
}

/*
 * Fingerprint is SHA-256 over the raw 32-byte public key, base32, uppercase and
 * unpadded — 52 characters.
 *
 * Over the raw key rather than a DER wrapping, so the value does not depend on
 * an encoding choice that a future version might make differently. The whole
 * hash and not a prefix: a shortened fingerprint is a smaller target to collide
 * with, and every other property here rests on this one being hard to forge.
 * It is long because it has to be, and Grouped exists to make it readable
 * rather than shorter.
 */
func (i Identity) Fingerprint() string {
	sum := sha256.Sum256(i.Public())
	return fpEncoding.EncodeToString(sum[:])
}

// Grouped is the fingerprint with separators, for showing to a person.
//
// Never parse this. Group takes whatever it is given and Normalize undoes it;
// comparisons happen on the ungrouped form.
func (i Identity) Grouped() string { return Group(i.Fingerprint()) }

// Group inserts a separator every groupSize characters.
func Group(fingerprint string) string {
	var b strings.Builder
	for i, r := range fingerprint {
		if i > 0 && i%groupSize == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

/*
 * Normalize turns something a person typed into something comparable.
 *
 * Separators, spaces and case are all noise: a fingerprint read off a screen
 * and retyped arrives with the groups run together, or with spaces instead of
 * dashes, or in lower case. Base32's alphabet has no lower case, so folding it
 * up cannot collide with a different valid value — which is the reason that
 * alphabet was chosen.
 *
 * This does not validate. A caller comparing against a known fingerprint gets a
 * mismatch for free; a caller parsing an invite (Phase 2) will check length and
 * alphabet where it can report a useful error.
 */
func Normalize(s string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		switch r {
		case '-', ' ', '\t', '\n', '\r', ':':
			return -1
		}
		return r
	}, s))
}

// FingerprintOf is the fingerprint of somebody else's public key, so a caller
// holding a peer's key can render it the same way without an Identity.
func FingerprintOf(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return fpEncoding.EncodeToString(sum[:])
}

/*
 * LoadOrCreate returns the identity for a data directory, generating one only
 * if none exists.
 *
 * Read the package comment before changing the error handling here. A corrupt
 * key is ErrCorrupt and the caller is expected to refuse to start, because the
 * alternative — generating a replacement — silently changes who this server is.
 */
func LoadOrCreate(dataDir string) (Identity, error) {
	path := KeyPath(dataDir)

	switch data, err := os.ReadFile(path); {
	case err == nil:
		id, perr := parse(data)
		if perr != nil {
			// Wrapped rather than replaced, so an operator can see *why* it
			// could not be read, and %w keeps ErrCorrupt matchable.
			return Identity{}, fmt.Errorf("%w (%s): %v", ErrCorrupt, path, perr)
		}
		return id, nil
	case !errors.Is(err, os.ErrNotExist):
		// A permission error is emphatically not "no identity yet". Treating it
		// as absent would mint a second identity for a server that already has
		// one it cannot currently read.
		return Identity{}, fmt.Errorf("%w (%s): %v", ErrCorrupt, path, err)
	}

	return create(dataDir, path)
}

func create(dataDir, path string) (Identity, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate server identity: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return Identity{}, fmt.Errorf("encode server identity: %w", err)
	}
	if err := os.MkdirAll(Dir(dataDir), 0o700); err != nil {
		return Identity{}, fmt.Errorf("create identity dir: %w", err)
	}
	if err := writeFileSync(path, pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: der})); err != nil {
		return Identity{}, err
	}
	return Identity{priv: priv}, nil
}

func parse(data []byte) (Identity, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return Identity{}, errors.New("not PEM")
	}
	if block.Type != pemType {
		return Identity{}, fmt.Errorf("unexpected PEM block %q", block.Type)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return Identity{}, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		// A server whose identity is an RSA key is not a server with a slightly
		// different identity; it is one this build cannot speak for.
		return Identity{}, fmt.Errorf("identity is %T, want ed25519", key)
	}
	return Identity{priv: priv}, nil
}

// Signer exposes the key for the one thing outside this package that needs it —
// mTLS in Phase 2 and ticket signing in Phase 4 — as a crypto.Signer, so the
// bytes themselves still never leave.
func (i Identity) Signer() crypto.Signer { return i.priv }

// writeFileSync writes 0600 via a temp file and rename, so a crash mid-write
// cannot leave a truncated key — which, unlike a truncated certificate, is not
// something this package will replace for you.
func writeFileSync(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
