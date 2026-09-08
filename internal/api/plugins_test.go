package api

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"lancast/internal/plugin"
)

// postRaw sends a non-JSON body (a plugin bundle) to an endpoint.
func (h *harness) postRaw(t *testing.T, path string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// fixtureBundle builds an unsigned bundle. The wasm is opaque bytes — the API
// flow verifies and stores but never compiles (that happens on reload, which the
// harness does not wire), so a real module is unnecessary here.
func fixtureBundle(t *testing.T) []byte {
	t.Helper()
	manifest := []byte(`{"name":"omdb","version":"0.1.0","abi":2,"kind":"rating_source",` +
		`"capabilities":{"http":["www.omdbapi.com"],"secrets":["omdb_key"]}}`)
	b, err := plugin.CreateBundle(manifest, []byte("opaque-wasm"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The two-step install: upload stages the plugin disabled with an empty grant and
// reports what it requests; grant activates it with the approved subset.
func TestPluginInstallTwoStep(t *testing.T) {
	h := newHarness(t)

	// Step one: upload.
	var staged pluginView
	decode(t, h.postRaw(t, "/api/plugins", fixtureBundle(t)), &staged)
	if staged.Name != "omdb" || staged.Enabled {
		t.Fatalf("staged = %+v, want omdb disabled", staged)
	}
	if len(staged.Requested.HTTP) != 1 || len(staged.Requested.Secrets) != 1 {
		t.Errorf("requested = %+v, want the manifest's caps", staged.Requested)
	}
	if len(staged.Granted.HTTP) != 0 || len(staged.Granted.Secrets) != 0 {
		t.Errorf("granted = %+v, want empty before approval", staged.Granted)
	}

	// It appears in the list, still disabled.
	var list struct {
		Plugins []pluginView `json:"plugins"`
	}
	decode(t, h.do(t, "GET", "/api/plugins", nil), &list)
	if len(list.Plugins) != 1 || list.Plugins[0].Enabled {
		t.Fatalf("list = %+v, want one disabled plugin", list.Plugins)
	}

	// Step two: grant a subset (HTTP only), which activates it.
	var granted pluginView
	decode(t, h.do(t, "POST", "/api/plugins/omdb/grant",
		map[string]any{"http": []string{"www.omdbapi.com"}, "secrets": []string{}}), &granted)
	if !granted.Enabled {
		t.Error("plugin not enabled after grant")
	}
	if len(granted.Granted.HTTP) != 1 || len(granted.Granted.Secrets) != 0 {
		t.Errorf("granted = %+v, want http only", granted.Granted)
	}
}

// A grant may not exceed what the manifest requests.
func TestPluginGrantCannotExceedRequest(t *testing.T) {
	h := newHarness(t)
	h.postRaw(t, "/api/plugins", fixtureBundle(t)).Body.Close()

	resp := h.do(t, "POST", "/api/plugins/omdb/grant",
		map[string]any{"http": []string{"evil.test"}, "secrets": []string{}})
	wantError(t, resp, 400, "bad_request")
}

// A tampered bundle is rejected at upload, before anything is staged.
func TestPluginUploadRejectsTamperedBundle(t *testing.T) {
	h := newHarness(t)
	bundle := fixtureBundle(t)
	bundle[len(bundle)-20] ^= 0xff // corrupt a byte

	resp := h.postRaw(t, "/api/plugins", bundle)
	if resp.StatusCode != 400 {
		t.Errorf("tampered upload status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	var list struct {
		Plugins []pluginView `json:"plugins"`
	}
	decode(t, h.do(t, "GET", "/api/plugins", nil), &list)
	if len(list.Plugins) != 0 {
		t.Errorf("a rejected bundle was staged: %+v", list.Plugins)
	}
}

func TestPluginEnableDisableRemove(t *testing.T) {
	h := newHarness(t)
	h.postRaw(t, "/api/plugins", fixtureBundle(t)).Body.Close()
	h.do(t, "POST", "/api/plugins/omdb/grant",
		map[string]any{"http": []string{"www.omdbapi.com"}, "secrets": []string{"omdb_key"}}).Body.Close()

	if resp := h.do(t, "POST", "/api/plugins/omdb/disable", nil); resp.StatusCode != 204 {
		t.Errorf("disable status = %d, want 204", resp.StatusCode)
	}
	if resp := h.do(t, "DELETE", "/api/plugins/omdb", nil); resp.StatusCode != 204 {
		t.Errorf("delete status = %d, want 204", resp.StatusCode)
	}
	if resp := h.do(t, "POST", "/api/plugins/omdb/enable", nil); resp.StatusCode != 404 {
		t.Errorf("enable-after-remove status = %d, want 404", resp.StatusCode)
	}
}

// thirdPartyBundle asks for a secret the server knows nothing about — the case
// that could be granted and never read before plugin_secret existed.
func thirdPartyBundle(t *testing.T) []byte {
	t.Helper()
	manifest := []byte(`{"name":"example","version":"0.1.0","abi":2,"kind":"rating_source",` +
		`"capabilities":{"http":["api.example.com"],"secrets":["example_key"]}}`)
	b, err := plugin.CreateBundle(manifest, []byte("opaque-wasm"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// installThirdParty uploads and grants everything the third-party fixture asks.
func installThirdParty(t *testing.T, h *harness) {
	t.Helper()
	h.postRaw(t, "/api/plugins", thirdPartyBundle(t))
	h.do(t, "POST", "/api/plugins/example/grant",
		map[string]any{"http": []string{"api.example.com"}, "secrets": []string{"example_key"}})
}

func pluginByName(t *testing.T, h *harness, name string) pluginView {
	t.Helper()
	var list struct {
		Plugins []pluginView `json:"plugins"`
	}
	decode(t, h.do(t, "GET", "/api/plugins", nil), &list)
	for _, p := range list.Plugins {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no plugin named %q in %+v", name, list.Plugins)
	return pluginView{}
}

/*
 * The state that used to mislead, and the state that fixes it.
 *
 * A granted secret the server has no value for must report as *not* configured.
 * Reading "granted" as "working" is what let an operator approve a plugin to
 * use a key it could never read.
 */
func TestPluginSecretGrantedIsNotConfigured(t *testing.T) {
	h := newHarness(t)
	installThirdParty(t, h)

	p := pluginByName(t, h, "example")
	if len(p.Granted.Secrets) != 1 || p.Granted.Secrets[0] != "example_key" {
		t.Fatalf("granted = %+v, want example_key", p.Granted)
	}
	if len(p.SecretsConfigured) != 0 {
		t.Errorf("secrets_configured = %v, want none — granted is not configured", p.SecretsConfigured)
	}

	// Give it a value; now it is configured.
	if resp := h.do(t, "PUT", "/api/plugins/example/secrets/example_key",
		map[string]any{"value": "hunter2"}); resp.StatusCode != 204 {
		t.Fatalf("PUT secret = %d, want 204", resp.StatusCode)
	}
	p = pluginByName(t, h, "example")
	if len(p.SecretsConfigured) != 1 || p.SecretsConfigured[0] != "example_key" {
		t.Errorf("secrets_configured = %v, want [example_key]", p.SecretsConfigured)
	}
}

// The grant is the authority here as everywhere else: a value cannot be stored
// against a name nobody approved.
func TestPluginSecretMustBeGranted(t *testing.T) {
	h := newHarness(t)
	h.postRaw(t, "/api/plugins", thirdPartyBundle(t))
	// Uploaded but not granted: the grant is empty.
	resp := h.do(t, "PUT", "/api/plugins/example/secrets/example_key",
		map[string]any{"value": "hunter2"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT before grant = %d, want 400", resp.StatusCode)
	}

	installThirdParty(t, h)
	resp = h.do(t, "PUT", "/api/plugins/example/secrets/something_else",
		map[string]any{"value": "hunter2"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT an ungranted name = %d, want 400", resp.StatusCode)
	}
}

/*
 * No endpoint returns a value, and this asserts it against the bytes rather
 * than against the struct.
 *
 * A pluginView has no field for one, so a Go-level check would pass whatever
 * the wire did. The whole listing is searched for the value instead.
 */
func TestPluginSecretIsNeverReadBack(t *testing.T) {
	h := newHarness(t)
	installThirdParty(t, h)
	h.do(t, "PUT", "/api/plugins/example/secrets/example_key",
		map[string]any{"value": "correct-horse-battery-staple"})

	resp := h.do(t, "GET", "/api/plugins", nil)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("correct-horse-battery-staple")) {
		t.Errorf("the listing returned a secret's value:\n%s", body)
	}
}

func TestPluginSecretDelete(t *testing.T) {
	h := newHarness(t)
	installThirdParty(t, h)
	h.do(t, "PUT", "/api/plugins/example/secrets/example_key", map[string]any{"value": "hunter2"})

	if resp := h.do(t, "DELETE", "/api/plugins/example/secrets/example_key", nil); resp.StatusCode != 204 {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}
	if p := pluginByName(t, h, "example"); len(p.SecretsConfigured) != 0 {
		t.Errorf("secrets_configured = %v after delete, want none", p.SecretsConfigured)
	}
	// Again, on a secret that is now unset: still 204.
	if resp := h.do(t, "DELETE", "/api/plugins/example/secrets/example_key", nil); resp.StatusCode != 204 {
		t.Errorf("second DELETE = %d, want 204", resp.StatusCode)
	}
	// An unknown plugin is a 404 regardless.
	if resp := h.do(t, "DELETE", "/api/plugins/nobody/secrets/x", nil); resp.StatusCode != 404 {
		t.Errorf("DELETE on an unknown plugin = %d, want 404", resp.StatusCode)
	}
}

// An empty value clears rather than storing emptiness.
func TestPluginSecretEmptyValueClears(t *testing.T) {
	h := newHarness(t)
	installThirdParty(t, h)
	h.do(t, "PUT", "/api/plugins/example/secrets/example_key", map[string]any{"value": "hunter2"})
	h.do(t, "PUT", "/api/plugins/example/secrets/example_key", map[string]any{"value": ""})

	if p := pluginByName(t, h, "example"); len(p.SecretsConfigured) != 0 {
		t.Errorf("secrets_configured = %v, want none after an empty value", p.SecretsConfigured)
	}
}
