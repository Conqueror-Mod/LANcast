package plugin

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// loadFixture compiles the committed fixture module under a manifest granting
// one HTTP host and one secret, with injected getters so nothing hits the
// network.
func loadFixture(t *testing.T, opts ...Option) *Plugin {
	t.Helper()
	wasm, err := os.ReadFile("testdata/fixture.wasm")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	ctx := context.Background()
	rt, err := NewRuntime(ctx, quietLog(), opts...)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(func() { rt.Close(ctx) })

	m := Manifest{
		Name: "fixture", Version: "0.0.1", ABI: ABIVersion, Kind: KindRatingSource,
		Capabilities: Capabilities{HTTP: []string{"example.test"}, Secrets: []string{"omdb_key"}},
	}
	p, err := rt.Load(ctx, m, wasm)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return p
}

func TestParseManifestValidation(t *testing.T) {
	good := `{"name":"x","version":"1","abi":1,"kind":"rating_source"}`
	if _, err := ParseManifest([]byte(good)); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := map[string]string{
		"no name":      `{"abi":1,"kind":"rating_source"}`,
		"unknown kind": `{"name":"x","abi":1,"kind":"weather"}`,
		"bad abi":      `{"name":"x","abi":99,"kind":"rating_source"}`,
		"malformed":    `{not json`,
	}
	for name, src := range bad {
		if _, err := ParseManifest([]byte(src)); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

func TestEchoRoundTrip(t *testing.T) {
	p := loadFixture(t)
	in := []byte(`{"hello":"世界"}`)
	out, err := p.Call(context.Background(), "echo", in)
	if err != nil {
		t.Fatalf("Call echo: %v", err)
	}
	if string(out) != string(in) {
		t.Errorf("echo = %q, want %q", out, in)
	}
}

func TestHTTPGetRestrictedToDeclaredHosts(t *testing.T) {
	var fetched string
	getter := func(ctx context.Context, url string) ([]byte, error) {
		fetched = url
		return []byte("payload"), nil
	}
	p := loadFixture(t, WithHTTPGetter(getter))

	// Declared host: the host makes the call and the bytes come back.
	out, err := p.Call(context.Background(), "fetch", []byte("https://example.test/data"))
	if err != nil {
		t.Fatalf("fetch allowed: %v", err)
	}
	if string(out) != "payload" {
		t.Errorf("allowed fetch = %q, want payload", out)
	}
	if fetched != "https://example.test/data" {
		t.Errorf("host getter saw %q", fetched)
	}

	// Undeclared host: denied, the getter is never reached, empty comes back.
	fetched = ""
	out, err = p.Call(context.Background(), "fetch", []byte("https://evil.test/steal"))
	if err != nil {
		t.Fatalf("fetch denied returned error: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("denied fetch = %q, want empty", out)
	}
	if fetched != "" {
		t.Errorf("denied host still reached the getter: %q", fetched)
	}
}

func TestSecretScopedToManifest(t *testing.T) {
	secrets := map[string]string{"omdb_key": "s3cr3t", "tmdb_key": "nope"}
	p := loadFixture(t, WithSecretResolver(func(name string) string { return secrets[name] }))

	// Granted secret comes through.
	out, err := p.Call(context.Background(), "getsecret", []byte("omdb_key"))
	if err != nil {
		t.Fatalf("getsecret granted: %v", err)
	}
	if string(out) != "s3cr3t" {
		t.Errorf("granted secret = %q, want s3cr3t", out)
	}

	// A real secret the manifest did not grant is invisible to the plugin.
	out, err = p.Call(context.Background(), "getsecret", []byte("tmdb_key"))
	if err != nil {
		t.Fatalf("getsecret ungranted: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("ungranted secret leaked: %q", out)
	}
}

func TestMissingExportIsAnError(t *testing.T) {
	p := loadFixture(t)
	if _, err := p.Call(context.Background(), "does_not_exist", nil); err == nil {
		t.Error("calling a missing export should error")
	}
}
