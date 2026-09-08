package store

import (
	"context"
	"errors"
	"testing"
)

func TestInstalledPluginLifecycle(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	p := InstalledPlugin{
		Name: "omdb", Version: "0.1.0", Kind: "rating_source",
		Digest: "abc123", Signer: "first_party", Enabled: true,
		GrantedHTTP: []string{"www.omdbapi.com"}, GrantedSecrets: []string{"omdb_key"},
	}
	if err := st.InstallPlugin(ctx, p); err != nil {
		t.Fatalf("InstallPlugin: %v", err)
	}

	list, err := st.ListInstalledPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %d, want 1", len(list))
	}
	got := list[0]
	if got.Name != "omdb" || got.Signer != "first_party" || !got.Enabled {
		t.Errorf("row = %+v", got)
	}
	if len(got.GrantedHTTP) != 1 || got.GrantedHTTP[0] != "www.omdbapi.com" {
		t.Errorf("granted_http = %v", got.GrantedHTTP)
	}
	if len(got.GrantedSecrets) != 1 || got.GrantedSecrets[0] != "omdb_key" {
		t.Errorf("granted_secrets = %v", got.GrantedSecrets)
	}

	// Disable, then re-read.
	if err := st.SetPluginEnabled(ctx, "omdb", false); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListInstalledPlugins(ctx)
	if list[0].Enabled {
		t.Error("plugin still enabled after disable")
	}

	// Re-install with a changed digest and a narrower grant replaces the row —
	// the mechanism that forces a fresh approval on a manifest change.
	p.Digest = "def456"
	p.GrantedSecrets = nil
	p.Enabled = true
	if err := st.InstallPlugin(ctx, p); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListInstalledPlugins(ctx)
	if len(list) != 1 {
		t.Fatalf("re-install created a second row: %d", len(list))
	}
	if list[0].Digest != "def456" || len(list[0].GrantedSecrets) != 0 {
		t.Errorf("re-install did not replace digest/grant: %+v", list[0])
	}

	// Remove.
	if err := st.RemovePlugin(ctx, "omdb"); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListInstalledPlugins(ctx)
	if len(list) != 0 {
		t.Errorf("plugin still present after remove: %d", len(list))
	}
}

func TestPluginLifecycleNotFound(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	if err := st.SetPluginEnabled(ctx, "ghost", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPluginEnabled on missing = %v, want ErrNotFound", err)
	}
	if err := st.RemovePlugin(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemovePlugin on missing = %v, want ErrNotFound", err)
	}
}

// installFor is a plugin row to hang secrets off.
func installFor(t *testing.T, st *Store, name string, secrets ...string) {
	t.Helper()
	err := st.InstallPlugin(context.Background(), InstalledPlugin{
		Name: name, Version: "1", Kind: "rating_source", Digest: name + "-digest",
		Signer: "unsigned", Enabled: true, GrantedSecrets: secrets,
	})
	if err != nil {
		t.Fatalf("InstallPlugin %s: %v", name, err)
	}
}

func TestPluginSecretRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	installFor(t, st, "example", "example_key")

	if _, ok, err := st.PluginSecret(ctx, "example", "example_key"); err != nil || ok {
		t.Fatalf("unset secret = (ok %v, err %v), want not set", ok, err)
	}
	if err := st.SetPluginSecret(ctx, "example", "example_key", "hunter2"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.PluginSecret(ctx, "example", "example_key")
	if err != nil || !ok || v != "hunter2" {
		t.Fatalf("secret = (%q, %v, %v), want hunter2", v, ok, err)
	}

	// Writing again replaces rather than duplicating.
	if err := st.SetPluginSecret(ctx, "example", "example_key", "hunter3"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.PluginSecret(ctx, "example", "example_key"); v != "hunter3" {
		t.Errorf("after rewrite = %q, want hunter3", v)
	}

	names, err := st.ConfiguredPluginSecrets(ctx, "example")
	if err != nil || len(names) != 1 || names[0] != "example_key" {
		t.Errorf("configured = %v (%v), want [example_key]", names, err)
	}
}

/*
 * Two plugins asking for the same name are asking for two different things.
 *
 * A credential is obtained *for* a plugin. Keyed on the name alone, the second
 * plugin to be installed would silently read the first one's key — which is
 * both wrong and a quiet way to hand somebody's credential to code they did
 * not give it to.
 */
func TestPluginSecretsAreScopedToOnePlugin(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	installFor(t, st, "one", "api_key")
	installFor(t, st, "two", "api_key")

	if err := st.SetPluginSecret(ctx, "one", "api_key", "one-value"); err != nil {
		t.Fatal(err)
	}
	if v, ok, _ := st.PluginSecret(ctx, "two", "api_key"); ok || v != "" {
		t.Errorf("plugin two read %q, want nothing — it read another plugin's credential", v)
	}
	if err := st.SetPluginSecret(ctx, "two", "api_key", "two-value"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.PluginSecret(ctx, "one", "api_key"); v != "one-value" {
		t.Errorf("plugin one now reads %q, want one-value", v)
	}
}

// An empty value deletes rather than storing emptiness: "" is what an unset
// secret already reads as, and two spellings of unset is one too many.
func TestPluginSecretEmptyValueClearsIt(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	installFor(t, st, "example", "example_key")

	st.SetPluginSecret(ctx, "example", "example_key", "hunter2")
	if err := st.SetPluginSecret(ctx, "example", "example_key", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.PluginSecret(ctx, "example", "example_key"); ok {
		t.Error("an empty value stored a row instead of clearing it")
	}
	names, _ := st.ConfiguredPluginSecrets(ctx, "example")
	if len(names) != 0 {
		t.Errorf("configured = %v, want none", names)
	}
}

/*
 * Removing a plugin takes its credentials with it.
 *
 * A stored key outliving the plugin it was obtained for is a credential nobody
 * can see, in a table nobody would think to look in, waiting to be handed to
 * whatever installs under that name next.
 */
func TestRemovePluginForgetsItsSecrets(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	installFor(t, st, "example", "example_key")
	st.SetPluginSecret(ctx, "example", "example_key", "hunter2")

	if err := st.RemovePlugin(ctx, "example"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.PluginSecret(ctx, "example", "example_key"); ok {
		t.Error("the credential outlived the plugin it belonged to")
	}
}

// Deleting one that was never set is not a failure: the caller asked for it to
// be gone and it is.
func TestDeletePluginSecretIsIdempotent(t *testing.T) {
	st := newStore(t)
	if err := st.DeletePluginSecret(context.Background(), "nobody", "nothing"); err != nil {
		t.Errorf("deleting an unset secret failed: %v", err)
	}
}
