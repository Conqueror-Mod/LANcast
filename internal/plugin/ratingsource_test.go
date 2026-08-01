package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/meta"
)

// A loaded rating_source plugin, wrapped as a meta.RatingSource and registered,
// is indistinguishable from a native source: Registry.Ratings collects its
// scores. This is the ADR 0007 promise at the adapter seam.
func TestRatingSourceThroughRegistry(t *testing.T) {
	p := loadFixture(t)
	rs, err := NewRatingSource(p)
	if err != nil {
		t.Fatalf("NewRatingSource: %v", err)
	}
	if rs.ID() != "fixture" {
		t.Errorf("ID = %q, want fixture", rs.ID())
	}

	reg := meta.NewRegistry()
	reg.AddRatingSource(rs)
	if !reg.HasRatingSources() {
		t.Fatal("registry reports no rating sources after adding one")
	}

	got, err := reg.Ratings(context.Background(), "tt2543164")
	if err != nil {
		t.Fatalf("Registry.Ratings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ratings = %+v, want one", got)
	}
	// The fixture echoes the imdb id into display, proving the request crossed
	// the boundary and the response marshalled back.
	if got[0].Source != "imdb" || got[0].Display != "tt2543164" || got[0].Votes != 42 {
		t.Errorf("rating = %+v, want source imdb display tt2543164 votes 42", got[0])
	}
}

func TestRatingSourceRejectsEmptyIMDb(t *testing.T) {
	rs, _ := NewRatingSource(loadFixture(t))
	got, err := rs.Ratings(context.Background(), "")
	if err != nil || got != nil {
		t.Errorf("empty imdb Ratings = (%v, %v), want (nil, nil)", got, err)
	}
}

// LoadAll skips a malformed plugin dir and loads a good one; RegisterInto wires
// the good one into the registry.
func TestLoadAllSkipsBadPluginsAndRegistersGood(t *testing.T) {
	wasm, err := os.ReadFile("testdata/fixture.wasm")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	// A good plugin: manifest + module.
	good := filepath.Join(root, "good")
	os.MkdirAll(good, 0o755)
	manifest := Manifest{
		Name: "good", Version: "1", ABI: ABIVersion, Kind: KindRatingSource,
		Capabilities: Capabilities{},
	}
	mb, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(good, "plugin.json"), mb, 0o644)
	os.WriteFile(filepath.Join(good, "plugin.wasm"), wasm, 0o644)

	// A broken plugin: manifest with an unsupported ABI.
	bad := filepath.Join(root, "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, "plugin.json"), []byte(`{"name":"bad","abi":99,"kind":"rating_source"}`), 0o644)
	os.WriteFile(filepath.Join(bad, "plugin.wasm"), wasm, 0o644)

	ctx := context.Background()
	rt, err := NewRuntime(ctx, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close(ctx) })

	plugins := rt.LoadAll(ctx, root)
	if len(plugins) != 1 || plugins[0].Manifest.Name != "good" {
		t.Fatalf("LoadAll = %d plugins, want just 'good'", len(plugins))
	}

	reg := meta.NewRegistry()
	RegisterInto(reg, plugins, quietLog())
	if !reg.HasRatingSources() {
		t.Error("good plugin was not registered as a rating source")
	}
}

// A missing plugin dir is not an error — plugins are optional.
func TestLoadAllMissingDirIsEmpty(t *testing.T) {
	ctx := context.Background()
	rt, _ := NewRuntime(ctx, quietLog())
	t.Cleanup(func() { rt.Close(ctx) })
	if got := rt.LoadAll(ctx, filepath.Join(t.TempDir(), "nonexistent")); got != nil {
		t.Errorf("LoadAll of a missing dir = %v, want nil", got)
	}
}
