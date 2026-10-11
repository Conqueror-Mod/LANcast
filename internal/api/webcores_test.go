package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"lancast/internal/retro/webcores"
)

/*
 * The browser's cores over HTTP (ADR 0077 part a).
 *
 * What a request can and cannot reach: anyone signed in can ask which
 * consoles play in the browser and load an installed core, only an
 * administrator can start a download, and no request names a file outside
 * the pinned list.
 */

// placeCore writes a core's two files at their pinned sizes, which is what
// Present and Path check. The bytes are not the real core; nothing here runs
// one.
func placeCore(t *testing.T, h *harness, name string) webcores.Core {
	t.Helper()
	c, ok := webcores.ByName(name)
	if !ok {
		t.Fatalf("no core %s", name)
	}
	for _, f := range c.Files() {
		p := filepath.Join(h.srvAPI.webCoreDir(), f.Dest)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, f.SizeBytes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestWebCoresStatusIsForEverySignedInAccount(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	placeCore(t, h, "fceumm")
	member := h.addMember(t, "kiddo", "correct horse battery staple")

	resp := h.doAs(t, member, "GET", "/api/retro/web-cores", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member asking: %d", resp.StatusCode)
	}
	var body struct {
		Cores []struct {
			Name      string   `json:"name"`
			Installed bool     `json:"installed"`
			Consoles  []string `json:"consoles"`
		} `json:"cores"`
		Archive struct {
			SizeBytes int64 `json:"size_bytes"`
		} `json:"archive"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range body.Cores {
		got[c.Name] = c.Installed
	}
	if len(got) != 4 || !got["fceumm"] || got["mgba"] {
		t.Errorf("cores = %v, want four with only fceumm installed", got)
	}
	if body.Archive.SizeBytes != webcores.Archive.SizeBytes {
		t.Errorf("archive size %d", body.Archive.SizeBytes)
	}
}

// A download is a server-side fetch of a binary, and only an administrator
// starts one. Refused before anything is fetched.
func TestOnlyAnAdministratorInstallsWebCores(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	member := h.addMember(t, "kiddo", "correct horse battery staple")
	for _, path := range []string{"/api/retro/web-cores/install", "/api/retro/web-cores/install/cancel"} {
		resp := h.doAs(t, member, "POST", path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("member POST %s: %d, want 403", path, resp.StatusCode)
		}
	}
	h.srvAPI.webCoreInstall.mu.Lock()
	running := h.srvAPI.webCoreInstall.running
	h.srvAPI.webCoreInstall.mu.Unlock()
	if running {
		t.Error("a member started a download")
	}
}

func TestAnInstalledCoreIsServedWithTheRightType(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	c := placeCore(t, h, "mgba")

	resp := h.authed(t, "GET", "/api/retro/web-cores/mgba/wasm", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || int64(len(body)) != c.Wasm.SizeBytes {
		t.Fatalf("wasm: %d, %d bytes", resp.StatusCode, len(body))
	}
	// instantiateStreaming refuses any other type.
	if ct := resp.Header.Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("Content-Type %q", ct)
	}
	if et := resp.Header.Get("ETag"); et != `"`+c.Wasm.SHA256+`"` {
		t.Errorf("ETag %q, want the pin", et)
	}

	resp = h.authed(t, "GET", "/api/retro/web-cores/mgba/js", nil)
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != http.StatusOK || ct != "text/javascript; charset=utf-8" {
		t.Errorf("js: %d %q", resp.StatusCode, ct)
	}
}

// Not signed in, not installed, not listed, not a kind: never a file.
func TestWebCoreFilesAreRefusedOtherwise(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "a good long password")
	placeCore(t, h, "mgba")
	// A file planted beside the cores, where a crafted name might reach it.
	os.WriteFile(filepath.Join(h.srvAPI.webCoreDir(), "secret.txt"), []byte("no"), 0o644)

	stranger, err := h.srv.Client().Get(h.srv.URL + "/api/retro/web-cores/mgba/wasm")
	if err != nil {
		t.Fatal(err)
	}
	stranger.Body.Close()
	if stranger.StatusCode != http.StatusUnauthorized {
		t.Errorf("no session: %d, want 401", stranger.StatusCode)
	}

	for _, path := range []string{
		"/api/retro/web-cores/fceumm/wasm",     // listed, not installed
		"/api/retro/web-cores/snes9x/wasm",     // not listed
		"/api/retro/web-cores/mgba/dll",        // not a kind
		"/api/retro/web-cores/mgba/secret.txt", // not a kind
		"/api/retro/web-cores/..%2Fsecret.txt/js",
	} {
		resp := h.authed(t, "GET", path, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, resp.StatusCode)
		}
	}
}
