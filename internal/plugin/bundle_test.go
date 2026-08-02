package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"testing"
)

func testManifestJSON() []byte {
	return []byte(`{"name":"omdb","version":"0.1.0","abi":1,"kind":"rating_source",` +
		`"capabilities":{"http":["www.omdbapi.com"],"secrets":["omdb_key"]}}`)
}

func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := genKey(t)
	wasm := []byte("\x00asm-not-real-but-opaque-to-verify")

	bundle, err := CreateBundle(testManifestJSON(), wasm, priv)
	if err != nil {
		t.Fatalf("CreateBundle: %v", err)
	}

	// Verifies as first_party against the project key.
	vb, err := VerifyBundle(bundle, TrustedKeys{Project: pub})
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}
	if vb.Signer != SignerFirstParty {
		t.Errorf("Signer = %q, want first_party", vb.Signer)
	}
	if vb.Manifest.Name != "omdb" || !bytes.Equal(vb.Wasm, wasm) {
		t.Errorf("bundle contents wrong: %+v", vb.Manifest)
	}
	if vb.Digest == "" {
		t.Error("digest not set")
	}

	// The same signature checked against a pinned (non-project) key is pinned.
	vb2, err := VerifyBundle(bundle, TrustedKeys{Pinned: []ed25519.PublicKey{pub}})
	if err != nil {
		t.Fatalf("pinned verify: %v", err)
	}
	if vb2.Signer != SignerPinned {
		t.Errorf("Signer = %q, want pinned", vb2.Signer)
	}
}

func TestUnsignedBundleIsNamedNotRejected(t *testing.T) {
	bundle, err := CreateBundle(testManifestJSON(), []byte("wasm"), nil)
	if err != nil {
		t.Fatal(err)
	}
	vb, err := VerifyBundle(bundle, TrustedKeys{})
	if err != nil {
		t.Fatalf("unsigned bundle rejected: %v", err)
	}
	if vb.Signer != SignerUnsigned {
		t.Errorf("Signer = %q, want unsigned", vb.Signer)
	}
}

func TestSignatureByUnknownKeyIsRejected(t *testing.T) {
	_, priv := genKey(t)     // signer
	otherPub, _ := genKey(t) // the only key we trust — a different one
	bundle, err := CreateBundle(testManifestJSON(), []byte("wasm"), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyBundle(bundle, TrustedKeys{Project: otherPub}); err == nil {
		t.Error("a signature by an untrusted key should be rejected, not downgraded")
	}
}

func TestTamperedManifestFailsVerification(t *testing.T) {
	pub, priv := genKey(t)
	bundle, err := CreateBundle(testManifestJSON(), []byte("wasm-bytes"), priv)
	if err != nil {
		t.Fatal(err)
	}
	tampered := rewriteBundleMember(t, bundle, bundleManifest,
		[]byte(`{"name":"omdb","version":"9.9.9","abi":1,"kind":"rating_source"}`))
	if _, err := VerifyBundle(tampered, TrustedKeys{Project: pub}); err == nil {
		t.Error("a modified manifest must fail the signature check")
	}
}

func TestTamperedWasmFailsVerification(t *testing.T) {
	pub, priv := genKey(t)
	bundle, err := CreateBundle(testManifestJSON(), []byte("original-wasm"), priv)
	if err != nil {
		t.Fatal(err)
	}
	tampered := rewriteBundleMember(t, bundle, bundleWasm, []byte("swapped-wasm!"))
	if _, err := VerifyBundle(tampered, TrustedKeys{Project: pub}); err == nil {
		t.Error("a modified module must fail the signature check")
	}
}

// rewriteBundleMember rebuilds a bundle zip with one member's bytes replaced,
// leaving the (now stale) signature in place — simulating tampering.
func rewriteBundleMember(t *testing.T, bundle []byte, name string, replacement []byte) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == name {
			w.Write(replacement)
			continue
		}
		rc, _ := f.Open()
		io.Copy(w, rc)
		rc.Close()
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
