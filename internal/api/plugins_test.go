package api

import (
	"bytes"
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
	manifest := []byte(`{"name":"omdb","version":"0.1.0","abi":1,"kind":"rating_source",` +
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
