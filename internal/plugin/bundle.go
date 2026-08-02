package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// A .lcplugin bundle is a zip of three members: the manifest, the module, and an
// optional detached signature (ADR 0021). Distribution and trust attach here; the
// runtime and capability model underneath (ADR 0020) are unchanged.
const (
	bundleManifest  = "plugin.json"
	bundleWasm      = "plugin.wasm"
	bundleSignature = "signature"
)

// Signer records how a bundle's provenance verified. It is orthogonal to
// authority: a first_party bundle still gets only the capabilities an operator
// granted, and an unsigned one is still capability-bound.
type Signer string

const (
	SignerFirstParty Signer = "first_party" // signed by the embedded project key
	SignerPinned     Signer = "pinned"      // signed by an operator-pinned key
	SignerUnsigned   Signer = "unsigned"    // no signature present
)

// TrustedKeys are the public keys a signature is checked against. Project is the
// embedded first-party key (may be nil until one is embedded); Pinned are keys an
// operator added for third-party publishers.
type TrustedKeys struct {
	Project ed25519.PublicKey
	Pinned  []ed25519.PublicKey
}

// VerifiedBundle is a bundle whose signature (if any) checked out. The wasm is
// returned but not yet compiled — verification happens first, always.
type VerifiedBundle struct {
	Manifest Manifest
	// ManifestBytes is the exact manifest as it was signed, kept so the digest can
	// be recomputed byte-for-byte at load time (a re-marshaled manifest would not
	// reproduce the same hash).
	ManifestBytes []byte
	Wasm          []byte
	Signer        Signer
	Digest        string // hex SHA-256 of the canonical content; the bundle's identity
}

// digest is SHA-256 over a length-prefixed manifest||wasm, so neither field can
// be swapped for the other's bytes without changing the hash.
func digest(manifest, wasm []byte) []byte {
	h := sha256.New()
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(manifest)))
	h.Write(n[:])
	h.Write(manifest)
	binary.LittleEndian.PutUint64(n[:], uint64(len(wasm)))
	h.Write(n[:])
	h.Write(wasm)
	return h.Sum(nil)
}

// CreateBundle packs a manifest and module into a .lcplugin. A non-nil key signs
// the canonical digest; a nil key produces an unsigned bundle (no signature
// member) — permitted, but the installer names it as such (ADR 0021).
func CreateBundle(manifest, wasm []byte, key ed25519.PrivateKey) ([]byte, error) {
	if _, err := ParseManifest(manifest); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, data []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := write(bundleManifest, manifest); err != nil {
		return nil, err
	}
	if err := write(bundleWasm, wasm); err != nil {
		return nil, err
	}
	if key != nil {
		sig := ed25519.Sign(key, digest(manifest, wasm))
		if err := write(bundleSignature, sig); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// VerifyBundle opens a .lcplugin, validates the manifest, and checks provenance
// before the wasm is ever compiled. A present signature MUST verify against a
// trusted key — a signature that matches none is tampering or an unknown
// publisher, and is rejected rather than downgraded to "unsigned". A bundle with
// no signature verifies as SignerUnsigned.
func VerifyBundle(data []byte, keys TrustedKeys) (*VerifiedBundle, error) {
	manifestBytes, wasm, sig, err := openBundle(data)
	if err != nil {
		return nil, err
	}
	m, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}

	sum := digest(manifestBytes, wasm)
	signer := SignerUnsigned
	if len(sig) > 0 {
		switch {
		case len(keys.Project) > 0 && ed25519.Verify(keys.Project, sum, sig):
			signer = SignerFirstParty
		case verifyAny(keys.Pinned, sum, sig):
			signer = SignerPinned
		default:
			return nil, errors.New("bundle is signed by an unknown or untrusted key")
		}
	}

	return &VerifiedBundle{
		Manifest:      m,
		ManifestBytes: manifestBytes,
		Wasm:          wasm,
		Signer:        signer,
		Digest:        hex.EncodeToString(sum),
	}, nil
}

func verifyAny(keys []ed25519.PublicKey, msg, sig []byte) bool {
	for _, k := range keys {
		if len(k) > 0 && ed25519.Verify(k, msg, sig) {
			return true
		}
	}
	return false
}

// openBundle unzips a bundle into its members. The manifest and module are
// required; the signature is optional (nil when absent).
func openBundle(data []byte) (manifest, wasm, sig []byte, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open bundle: %w", err)
	}
	for _, f := range zr.File {
		switch f.Name {
		case bundleManifest:
			manifest, err = readZip(f)
		case bundleWasm:
			wasm, err = readZip(f)
		case bundleSignature:
			sig, err = readZip(f)
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", f.Name, err)
		}
	}
	if len(manifest) == 0 {
		return nil, nil, nil, errors.New("bundle has no " + bundleManifest)
	}
	if len(wasm) == 0 {
		return nil, nil, nil, errors.New("bundle has no " + bundleWasm)
	}
	return manifest, wasm, sig, nil
}

func readZip(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
