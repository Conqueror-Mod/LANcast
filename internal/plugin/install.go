package plugin

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
)

// KnownBadDigests is the shipped blocklist — bundles refused at load regardless
// of their installed state (ADR 0021). Empty by default; a build can populate it
// to neutralise a known-bad first-party release across updates.
var KnownBadDigests = map[string]bool{}

// InstalledRecord is what the loader needs about an installed plugin: its
// identity, whether it is enabled, and — the load-bearing part — the granted
// capabilities. The grant, not the manifest's request, is the effective
// authority (ADR 0021). Kept free of the store type so the runtime does not
// depend on the persistence layer.
type InstalledRecord struct {
	Name           string
	Digest         string
	Enabled        bool
	GrantedHTTP    []string
	GrantedSecrets []string
}

// Unpack writes a verified bundle's manifest and module under root/<digest>/, so
// the loader can read them across restarts. The manifest is written as it was
// signed, so the digest recomputes exactly at load.
func Unpack(root string, vb *VerifiedBundle) error {
	dir := filepath.Join(root, vb.Digest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, bundleManifest), vb.ManifestBytes, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, bundleWasm), vb.Wasm, 0o644)
}

// RemoveUnpacked deletes a plugin's unpacked files by digest.
func RemoveUnpacked(root, digest string) error {
	return os.RemoveAll(filepath.Join(root, digest))
}

// LoadInstalled loads the enabled, non-blocklisted, still-intact plugins from
// their records. For each it re-computes the on-disk digest and refuses a
// mismatch (tampering), then loads the module under the **granted** capabilities
// — the manifest may ask for more, but only the grant authorizes. One bad plugin
// is skipped with a log, never fatal.
func (rt *Runtime) LoadInstalled(ctx context.Context, root string, records []InstalledRecord, blocked map[string]bool) []*Plugin {
	var out []*Plugin
	for _, rec := range records {
		if !rec.Enabled {
			continue
		}
		if blocked[rec.Digest] {
			rt.log.Warn("plugin blocklisted; refusing to load", "name", rec.Name, "digest", rec.Digest)
			continue
		}
		dir := filepath.Join(root, rec.Digest)
		manifestBytes, err := os.ReadFile(filepath.Join(dir, bundleManifest))
		if err != nil {
			rt.log.Warn("plugin files missing; skipping", "name", rec.Name, "error", err)
			continue
		}
		wasm, err := os.ReadFile(filepath.Join(dir, bundleWasm))
		if err != nil {
			rt.log.Warn("plugin files missing; skipping", "name", rec.Name, "error", err)
			continue
		}
		if hex.EncodeToString(digest(manifestBytes, wasm)) != rec.Digest {
			rt.log.Warn("plugin digest mismatch (on-disk tampering?); skipping", "name", rec.Name)
			continue
		}
		m, err := ParseManifest(manifestBytes)
		if err != nil {
			rt.log.Warn("plugin manifest invalid; skipping", "name", rec.Name, "error", err)
			continue
		}
		// The grant is the effective authority — the manifest's own request is
		// replaced, so a plugin can never exercise a capability the operator did
		// not approve, even if a later manifest edit widened the request.
		m.Capabilities = Capabilities{HTTP: rec.GrantedHTTP, Secrets: rec.GrantedSecrets}

		p, err := rt.Load(ctx, m, wasm)
		if err != nil {
			rt.log.Warn("plugin failed to load; skipping", "name", rec.Name, "error", err)
			continue
		}
		rt.log.Info("loaded plugin", "name", m.Name, "kind", m.Kind, "version", m.Version)
		out = append(out, p)
	}
	return out
}
