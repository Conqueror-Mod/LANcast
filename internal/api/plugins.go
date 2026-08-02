package api

import (
	"encoding/json"
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

func (s *Server) viewOf(p store.InstalledPlugin) pluginView {
	granted := plugin.Capabilities{HTTP: p.GrantedHTTP, Secrets: p.GrantedSecrets}
	return pluginView{
		Name: p.Name, Version: p.Version, Kind: p.Kind, Signer: p.Signer,
		Enabled: p.Enabled, Digest: p.Digest,
		Requested:   caps(s.requestedCaps(p.Digest, granted)),
		Granted:     caps(granted),
		InstalledAt: p.InstalledAt,
	}
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
		views = append(views, s.viewOf(p))
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
	view := s.viewOf(rec)
	view.Requested = caps(vb.Manifest.Capabilities)
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
	writeJSON(w, http.StatusOK, s.viewOf(p))
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
