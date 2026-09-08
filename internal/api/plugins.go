package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"lancast/internal/plugin"
	"lancast/internal/store"
)

// maxBundleBytes caps a plugin upload. Bundles are a few MB; this leaves room
// without inviting a memory-exhaustion upload.
const maxBundleBytes = 32 << 20 // 32 MiB

// pluginsRoot is where verified modules are unpacked, keyed by digest.
func (s *Server) pluginsRoot() string { return filepath.Join(s.dataDir, "plugins") }

type capsView struct {
	HTTP    []string `json:"http"`
	Secrets []string `json:"secrets"`
}

// pluginView is the API shape of an installed (or just-uploaded) plugin. It shows
// requested vs granted so the client can say "wants X, you granted Y".
type pluginView struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Kind        string   `json:"kind"`
	Signer      string   `json:"signer"`
	Enabled     bool     `json:"enabled"`
	Digest      string   `json:"digest"`
	Requested   capsView `json:"requested"`
	Granted     capsView `json:"granted"`
	InstalledAt int64    `json:"installed_at,omitempty"`

	// SecretsConfigured is the granted secret names that actually resolve to a
	// value — either one stored for this plugin, or one of the server's own
	// provider keys.
	//
	// It exists because "granted" was being read as "working". A plugin could
	// be granted a secret, be shown as granted, and read nothing, and the
	// approval dialog told the operator it would read a key it never could.
	// Names only: no value is ever reported back out.
	SecretsConfigured []string `json:"secrets_configured"`
}

func caps(c plugin.Capabilities) capsView {
	return capsView{HTTP: nonNil(c.HTTP), Secrets: nonNil(c.Secrets)}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// requestedCaps reads a staged/installed plugin's manifest from disk to report
// what it asks for. If the file is gone, the request is treated as equal to the
// grant — a best-effort display, never a failure.
func (s *Server) requestedCaps(digest string, granted plugin.Capabilities) plugin.Capabilities {
	b, err := os.ReadFile(filepath.Join(s.pluginsRoot(), digest, "plugin.json"))
	if err != nil {
		return granted
	}
	m, err := plugin.ParseManifest(b)
	if err != nil {
		return granted
	}
	return m.Capabilities
}

func (s *Server) viewOf(ctx context.Context, p store.InstalledPlugin) pluginView {
	granted := plugin.Capabilities{HTTP: p.GrantedHTTP, Secrets: p.GrantedSecrets}
	return pluginView{
		Name: p.Name, Version: p.Version, Kind: p.Kind, Signer: p.Signer,
		Enabled: p.Enabled, Digest: p.Digest,
		Requested:         caps(s.requestedCaps(p.Digest, granted)),
		Granted:           caps(granted),
		InstalledAt:       p.InstalledAt,
		SecretsConfigured: nonNil(s.configuredSecrets(ctx, p)),
	}
}

/*
 * configuredSecrets reports which of a plugin's granted secrets would actually
 * hand it something.
 *
 * It answers the same question the host's resolver answers, in the same order,
 * and both read the built-in names through config.Settings.BuiltinSecret —
 * because a second copy of that mapping that disagreed with the first would
 * show an operator a plugin as configured while the plugin read nothing, which
 * is the exact failure this whole change exists to remove.
 *
 * Best-effort: a database failure reports nothing configured rather than
 * failing the listing. Being told a key is missing when it is present is a
 * recoverable annoyance; not being able to see the page is not.
 */
func (s *Server) configuredSecrets(ctx context.Context, p store.InstalledPlugin) []string {
	stored, err := s.st.ConfiguredPluginSecrets(ctx, p.Name)
	if err != nil {
		s.log.Warn("could not read configured plugin secrets", "plugin", p.Name, "error", err)
	}
	has := make(map[string]bool, len(stored))
	for _, n := range stored {
		has[n] = true
	}
	settings := s.settings.Get()

	var out []string
	for _, n := range p.GrantedSecrets {
		if has[n] || settings.BuiltinSecret(n) != "" {
			out = append(out, n)
		}
	}
	return out
}

/*
 * setPluginSecret stores a credential for one installed plugin.
 *
 * Only a *granted* name is accepted. The grant is the authority everywhere else
 * in this model, and a value stored against a name nobody approved would be a
 * credential sitting in the database that no plugin can read — dead data that
 * looks like configuration.
 *
 * There is deliberately no way to read a value back. The only route out of the
 * database is the host function handing it to the guest that was granted it.
 */
func (s *Server) setPluginSecret(w http.ResponseWriter, r *http.Request) {
	name, secret := r.PathValue("name"), r.PathValue("secret")
	p, err := s.st.GetInstalledPlugin(r.Context(), name)
	if s.notFoundOr(w, err, "get plugin", "no such plugin") {
		return
	}
	if !contains(p.GrantedSecrets, secret) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"that secret is not granted to this plugin")
		return
	}

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if err := s.st.SetPluginSecret(r.Context(), name, secret, req.Value); err != nil {
		s.writeInternal(w, err, "set plugin secret")
		return
	}
	s.reloadPluginsSoon()
	// The name and whether it was set or cleared; never the value. An audit log
	// an administrator can read is not a place to put a credential.
	action := "set"
	if req.Value == "" {
		action = "cleared"
	}
	s.audit(r, "plugin.secret", "plugin", name,
		fmt.Sprintf("%s the %q secret for %q", action, secret, name),
		map[string]any{"secret": secret, "cleared": req.Value == ""})
	w.WriteHeader(http.StatusNoContent)
}

// deletePluginSecret forgets one credential. Deleting one that was never set
// succeeds: the caller asked for it to be gone, and it is.
func (s *Server) deletePluginSecret(w http.ResponseWriter, r *http.Request) {
	name, secret := r.PathValue("name"), r.PathValue("secret")
	if _, err := s.st.GetInstalledPlugin(r.Context(), name); s.notFoundOr(w, err, "get plugin", "no such plugin") {
		return
	}
	if err := s.st.DeletePluginSecret(r.Context(), name, secret); err != nil {
		s.writeInternal(w, err, "delete plugin secret")
		return
	}
	s.reloadPluginsSoon()
	s.audit(r, "plugin.secret", "plugin", name,
		fmt.Sprintf("cleared the %q secret for %q", secret, name),
		map[string]any{"secret": secret, "cleared": true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) trustedKeys() plugin.TrustedKeys {
	return plugin.TrustedKeys{Project: plugin.ProjectPublicKey()}
}

// listPlugins returns every installed plugin with its status and capabilities.
func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	installed, err := s.st.ListInstalledPlugins(r.Context())
	if err != nil {
		s.writeInternal(w, err, "list plugins")
		return
	}
	views := make([]pluginView, 0, len(installed))
	for _, p := range installed {
		views = append(views, s.viewOf(r.Context(), p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": views})
}

// uploadPlugin is step one of install: verify a bundle and stage it, disabled,
// with an empty grant. It returns what the plugin *requests* so the client can
// present the capability-approval dialog. Nothing is granted or activated here.
func (s *Server) uploadPlugin(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBundleBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "could not read bundle (too large?)")
		return
	}
	vb, err := plugin.VerifyBundle(body, s.trustedKeys())
	if err != nil {
		// A tampered or unknown-key bundle lands here — a client error, not ours.
		writeError(w, http.StatusBadRequest, "bad_request", "bundle failed verification: "+err.Error())
		return
	}
	if err := plugin.Unpack(s.pluginsRoot(), vb); err != nil {
		s.writeInternal(w, err, "unpack plugin")
		return
	}
	// Stage the row disabled with no grant: present until granted, inert until then.
	rec := store.InstalledPlugin{
		Name: vb.Manifest.Name, Version: vb.Manifest.Version, Kind: string(vb.Manifest.Kind),
		Digest: vb.Digest, Signer: string(vb.Signer), Enabled: false,
	}
	if err := s.st.InstallPlugin(r.Context(), rec); err != nil {
		s.writeInternal(w, err, "stage plugin")
		return
	}
	view := s.viewOf(r.Context(), rec)
	view.Requested = caps(vb.Manifest.Capabilities)
	// Staged, not yet trusted: the capability grant is a separate act and gets
	// its own event. Recording the signer is the point — provenance is half of
	// the two-layer trust model (ADR 0021).
	s.audit(r, "plugin.install", "plugin", rec.Name,
		fmt.Sprintf("Staged plugin %q (%s), signed by %s", rec.Name, rec.Version, rec.Signer),
		map[string]any{"digest": rec.Digest, "signer": rec.Signer, "version": rec.Version})
	writeJSON(w, http.StatusOK, view)
}

// grantPlugin is step two: approve some or all of the requested capabilities and
// activate. The grant must be a subset of what the manifest requests — the UI
// cannot hand a plugin more than it asked for — and the recorded grant, not the
// manifest, is the effective authority (ADR 0021).
func (s *Server) grantPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.st.GetInstalledPlugin(r.Context(), name)
	if s.notFoundOr(w, err, "get plugin", "no such plugin") {
		return
	}

	var req struct {
		HTTP    []string `json:"http"`
		Secrets []string `json:"secrets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}

	requested := s.requestedCaps(p.Digest, plugin.Capabilities{})
	if !subset(req.HTTP, requested.HTTP) || !subset(req.Secrets, requested.Secrets) {
		writeError(w, http.StatusBadRequest, "bad_request", "grant exceeds what the plugin requests")
		return
	}

	p.GrantedHTTP = nonNil(req.HTTP)
	p.GrantedSecrets = nonNil(req.Secrets)
	p.Enabled = true
	if err := s.st.InstallPlugin(r.Context(), p); err != nil {
		s.writeInternal(w, err, "grant plugin")
		return
	}
	s.reloadPluginsSoon()
	// The authority half of the trust model, and the event most worth being
	// able to review later: this is where third-party code was given reach.
	s.audit(r, "plugin.grant", "plugin", p.Name,
		fmt.Sprintf("Granted %q http=%v secrets=%v and enabled it",
			p.Name, p.GrantedHTTP, p.GrantedSecrets),
		map[string]any{"http": p.GrantedHTTP, "secrets": p.GrantedSecrets, "digest": p.Digest})
	writeJSON(w, http.StatusOK, s.viewOf(r.Context(), p))
}

func (s *Server) enablePlugin(w http.ResponseWriter, r *http.Request) {
	s.setPluginEnabled(w, r, true)
}

func (s *Server) disablePlugin(w http.ResponseWriter, r *http.Request) {
	s.setPluginEnabled(w, r, false)
}

func (s *Server) setPluginEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	name := r.PathValue("name")
	if err := s.st.SetPluginEnabled(r.Context(), name, enabled); s.notFoundOr(w, err, "set plugin enabled", "no such plugin") {
		return
	}
	s.reloadPluginsSoon()
	action, verb := "plugin.disable", "Disabled"
	if enabled {
		action, verb = "plugin.enable", "Enabled"
	}
	s.audit(r, action, "plugin", name, fmt.Sprintf("%s plugin %q", verb, name), nil)
	w.WriteHeader(http.StatusNoContent)
}

// removePlugin forgets a plugin and deletes its unpacked files.
func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.st.GetInstalledPlugin(r.Context(), name)
	if s.notFoundOr(w, err, "get plugin", "no such plugin") {
		return
	}
	if err := s.st.RemovePlugin(r.Context(), name); err != nil {
		s.writeInternal(w, err, "remove plugin")
		return
	}
	// Best-effort file cleanup: the row is already gone, so a lingering dir is
	// harmless (nothing loads it) and must not turn removal into a failure.
	if err := plugin.RemoveUnpacked(s.pluginsRoot(), p.Digest); err != nil {
		s.log.Warn("plugin files not fully removed", "name", name, "error", err)
	}
	s.reloadPluginsSoon()
	s.audit(r, "plugin.remove", "plugin", name,
		fmt.Sprintf("Removed plugin %q (%s) and its files", name, p.Version),
		map[string]any{"digest": p.Digest, "signer": p.Signer})
	w.WriteHeader(http.StatusNoContent)
}

// reloadPluginsSoon applies a plugin change to the running registry.
func (s *Server) reloadPluginsSoon() {
	if s.reloadPlugins == nil {
		return
	}
	if err := s.reloadPlugins(); err != nil {
		s.log.Warn("plugin reload failed", "error", err)
	}
}

// subset reports whether every element of want is in have.
func subset(want, have []string) bool {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
