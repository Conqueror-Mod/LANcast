package plugin

import (
	"context"
	"os"
	"strings"
	"testing"
)

/*
 * What the ABI promises (ADR 0064).
 *
 * The version check used to be an exact match, which is not a policy but the
 * absence of one: under it every change to the contract was breaking, including
 * changes that could not possibly break a plugin, so the contract could only
 * evolve by flag day. These are the rules that replaced it, asserted here
 * because the check is the only place the policy is enforced.
 */

func TestManifestAcceptsTheSupportedABIRange(t *testing.T) {
	manifest := func(abi string) []byte {
		return []byte(`{"name":"x","version":"1","abi":` + abi + `,"kind":"rating_source"}`)
	}

	if _, err := ParseManifest(manifest("2")); err != nil {
		t.Errorf("abi 2 refused: %v", err)
	}

	/*
	 * ABI 1 is refused on purpose and this test is the reason it stays that
	 * way. It was never public, its only implementations were ours, and ADR
	 * 0063 broke it because that was true — so supporting it would be carrying
	 * a reader for a version nothing in the world runs.
	 */
	if _, err := ParseManifest(manifest("1")); err == nil {
		t.Error("abi 1 accepted; it is deliberately outside the window")
	}

	// A module built for a contract this host does not implement yet is
	// refused rather than run against a boundary it was not built for.
	_, err := ParseManifest(manifest("3"))
	if err == nil {
		t.Fatal("abi 3 accepted by a host that implements 2")
	}
	// The message names the range, because "unsupported" alone leaves an author
	// guessing which direction they are wrong in.
	if !strings.Contains(err.Error(), "2-2") {
		t.Errorf("error = %q, want it to name the supported range", err)
	}
}

// The window is a constant so the next bump cannot forget it. If ABIVersion
// moves without MinABIVersion being reconsidered, this says so.
func TestABIWindowIsAtMostTwoVersions(t *testing.T) {
	if MinABIVersion > ABIVersion {
		t.Fatalf("MinABIVersion %d is above ABIVersion %d", MinABIVersion, ABIVersion)
	}
	if ABIVersion-MinABIVersion > 1 {
		t.Errorf("the host accepts ABI %d-%d, which is more than the one version of overlap "+
			"ADR 0064 allows — carrying several readers is what that decision refused",
			MinABIVersion, ABIVersion)
	}
}

/*
 * A module that cannot answer for the kind it claims is refused at load.
 *
 * Before this it installed cleanly, was listed as enabled, and failed at the
 * first call with "plugin has no export" — a message that reaches a log rather
 * than the person who just installed it. The exports are known at compile time,
 * so the honest moment to say so is load.
 */
func TestLoadRefusesAModuleMissingItsKindsExports(t *testing.T) {
	// The OMDb plugin is a real, shipped module that exports alloc and ratings
	// and nothing else — so it is a genuine provider-shaped failure rather than
	// a hand-made one.
	wasm, err := os.ReadFile("../../plugins/omdb/plugin.wasm")
	if err != nil {
		t.Skipf("no omdb build: %v", err)
	}
	ctx := context.Background()
	rt, err := NewRuntime(ctx, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.Close(ctx) })

	m := Manifest{
		Name: "pretend", Version: "1", ABI: ABIVersion, Kind: KindProvider,
		Caps: Caps{Movie: true},
	}
	if _, err := rt.Load(ctx, m, wasm); err == nil {
		t.Fatal("a provider with no search export loaded")
	} else if !strings.Contains(err.Error(), "search") {
		t.Errorf("error = %q, want it to name the missing export", err)
	}

	// The same module as what it actually is loads fine.
	m.Kind = KindRatingSource
	m.Caps = Caps{}
	if _, err := rt.Load(ctx, m, wasm); err != nil {
		t.Errorf("the omdb module was refused as a rating_source: %v", err)
	}
}

/*
 * HasExport is what makes an optional export a non-breaking addition.
 *
 * Plugin.Call errors on a missing export, so a host that simply called a newly
 * added optional function would fail every plugin built before it existed. The
 * rule in ADR 0064 says adding one is additive; this is what makes that true
 * rather than aspirational.
 */
func TestHasExport(t *testing.T) {
	p := loadFixture(t)
	if !p.HasExport("ratings") {
		t.Error("the fixture exports ratings and HasExport says otherwise")
	}
	if !p.HasExport("alloc") {
		t.Error("alloc missing, which would make the module uncallable")
	}
	if p.HasExport("a_function_added_in_some_later_version") {
		t.Error("HasExport invented an export")
	}
}
