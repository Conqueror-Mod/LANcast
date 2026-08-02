package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// installFixture verifies and unpacks the committed fixture under a manifest that
// *requests* both an HTTP host and a secret, returning the root and digest. The
// grant is applied later, at load, by LoadInstalled.
func installFixture(t *testing.T) (root, digest string) {
	t.Helper()
	wasm, err := os.ReadFile("testdata/fixture.wasm")
	if err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"fixture","version":"1","abi":1,"kind":"rating_source",` +
		`"capabilities":{"http":["example.test"],"secrets":["omdb_key"]}}`)
	bundle, err := CreateBundle(manifest, wasm, nil) // unsigned is fine for the loader test
	if err != nil {
		t.Fatal(err)
	}
	vb, err := VerifyBundle(bundle, TrustedKeys{})
	if err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	if err := Unpack(root, vb); err != nil {
		t.Fatal(err)
	}
	return root, vb.Digest
}

// The load-bearing property of ADR 0021: the GRANT is the effective authority,
// not the manifest's request. The fixture's manifest asks for the omdb_key
// secret, but the operator granted only the HTTP host — so at runtime the secret
// is invisible even though the resolver holds it.
func TestGrantOverridesManifestRequest(t *testing.T) {
	root, digest := installFixture(t)
	ctx := context.Background()

	rt, err := NewRuntime(ctx, quietLog(),
		WithSecretResolver(func(name string) string {
			if name == "omdb_key" {
				return "s3cr3t"
			}
			return ""
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close(ctx) })

	// Grant HTTP but NOT the secret.
	rec := InstalledRecord{
		Name: "fixture", Digest: digest, Enabled: true,
		GrantedHTTP: []string{"example.test"}, GrantedSecrets: nil,
	}
	plugins := rt.LoadInstalled(ctx, root, []InstalledRecord{rec}, nil)
	if len(plugins) != 1 {
		t.Fatalf("LoadInstalled = %d plugins, want 1", len(plugins))
	}

	out, err := plugins[0].Call(ctx, "getsecret", []byte("omdb_key"))
	if err != nil {
		t.Fatalf("getsecret: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("ungranted secret leaked despite the manifest requesting it: %q", out)
	}
}

func TestDisabledPluginIsNotLoaded(t *testing.T) {
	root, digest := installFixture(t)
	ctx := context.Background()
	rt, _ := NewRuntime(ctx, quietLog())
	t.Cleanup(func() { rt.Close(ctx) })

	rec := InstalledRecord{Name: "fixture", Digest: digest, Enabled: false}
	if plugins := rt.LoadInstalled(ctx, root, []InstalledRecord{rec}, nil); len(plugins) != 0 {
		t.Errorf("disabled plugin was loaded: %d", len(plugins))
	}
}

func TestBlocklistedDigestIsRefused(t *testing.T) {
	root, digest := installFixture(t)
	ctx := context.Background()
	rt, _ := NewRuntime(ctx, quietLog())
	t.Cleanup(func() { rt.Close(ctx) })

	rec := InstalledRecord{Name: "fixture", Digest: digest, Enabled: true}
	blocked := map[string]bool{digest: true}
	if plugins := rt.LoadInstalled(ctx, root, []InstalledRecord{rec}, blocked); len(plugins) != 0 {
		t.Errorf("blocklisted plugin was loaded: %d", len(plugins))
	}
}

func TestOnDiskTamperIsRefused(t *testing.T) {
	root, digest := installFixture(t)
	ctx := context.Background()
	rt, _ := NewRuntime(ctx, quietLog())
	t.Cleanup(func() { rt.Close(ctx) })

	// Corrupt the unpacked module after install; the digest no longer matches.
	if err := os.WriteFile(filepath.Join(root, digest, bundleWasm), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := InstalledRecord{Name: "fixture", Digest: digest, Enabled: true}
	if plugins := rt.LoadInstalled(ctx, root, []InstalledRecord{rec}, nil); len(plugins) != 0 {
		t.Errorf("on-disk-tampered plugin was loaded: %d", len(plugins))
	}
}
