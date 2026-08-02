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
