package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lancast/internal/milkdrop"
)

// A preset written for these tests, not taken from any pack: preset licences
// are uncertain (ADR 0091) and the repository carries none.
const testMilk = "[preset00]\n" +
	"MILKDROP_PRESET_VERSION=201\n" +
	"fDecay=0.980000\n" +
	"zoom=1.010000\n" +
	"nWaveMode=2\n" +
	"wavecode_0_enabled=1\n" +
	"wavecode_0_samples=512\n" +
	"wave_0_per_point1=x = sample;\n" +
	"wave_0_per_point2=y = 0.5 + value1;\n" +
	"shapecode_0_enabled=0\n" +
	"shape_0_per_frame1=this would not parse ( but the shape is off\n" +
	"per_frame_1=zoom = zoom + 0.01*sin(time);\n" +
	"per_frame_2=rot = rot + 0.02*bass_att;\n" +
	"warp_1=`shader_body { ret = tex2D(sampler_main, uv).xyz; }\n"

func (h *harness) raw(t *testing.T, method, path string, cookie *http.Cookie, origin string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (h *harness) uploadPreset(t *testing.T, filename, body string) *http.Response {
	t.Helper()
	return h.raw(t, "POST", "/api/visualizers/milkdrop?filename="+url.QueryEscape(filename), h.cookie, "", []byte(body))
}

func TestUploadingAMilkStoresItForEveryone(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")

	resp := h.uploadPreset(t, "Test - Gentle Drift.milk", testMilk)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201", resp.StatusCode)
	}
	var sm milkdrop.Summary
	decode(t, resp, &sm)
	if sm.Name != "Test - Gentle Drift" || sm.Source != "milk" || sm.Shaders != "converted" || sm.ShadersReason != "" {
		t.Errorf("summary = %+v, want the name without .milk, source milk, shaders converted", sm)
	}

	m := h.member(t, "georgia", "another long password")
	var list milkdropList
	decode(t, h.asUser(t, m, "GET", "/api/visualizers/milkdrop", nil), &list)
	if len(list.Presets) != 1 || list.Presets[0].ID != sm.ID {
		t.Fatalf("member's list = %+v, want the one preset", list.Presets)
	}

	resp = h.asUser(t, m, "GET", "/api/visualizers/milkdrop/"+sm.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member's fetch = %d, want 200", resp.StatusCode)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q, want immutable: an id names one preset", cc)
	}
	var e milkdrop.Entry
	decode(t, resp, &e)
	p := e.Preset
	if p.Version != 2 || p.BaseVals["decay"] != 0.98 || p.BaseVals["wave_mode"] != 2 {
		t.Errorf("base values = %v (version %d), want butterchurn's names: decay, wave_mode", p.BaseVals, p.Version)
	}
	if p.FrameEqs != "zoom = zoom + 0.01*sin(time);\nrot = rot + 0.02*bass_att;" {
		t.Errorf("frame_eqs_eel = %q", p.FrameEqs)
	}
	if p.Waves[0].PointEqs == "" || p.Waves[0].BaseVals["enabled"] != 1 {
		t.Errorf("wave 0 = %+v, want enabled with its point equations", p.Waves[0])
	}
	// The disabled shape's broken code was never checked, so it is not kept.
	if p.Shapes[0].FrameEqs != "" {
		t.Errorf("disabled shape kept %q; only what was checked is stored", p.Shapes[0].FrameEqs)
	}
	// The warp was converted on the server (ADR 0091, step g); the preset has
	// no comp, so butterchurn keeps its own.
	if !strings.HasPrefix(p.Warp, " shader_body { ") || !strings.Contains(p.Warp, "texture (sampler_main, uv)") || p.Comp != "" {
		t.Errorf("warp/comp = %q/%q, want the warp as GLSL and no comp", p.Warp, p.Comp)
	}
}

// What a fetch serves is butterchurn's preset and the summary: the HLSL it was
// converted from stays on the server.
func TestTheHLSLIsNeverServed(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	var sm milkdrop.Summary
	decode(t, h.uploadPreset(t, "drift.milk", testMilk), &sm)
	for _, path := range []string{"/api/visualizers/milkdrop", "/api/visualizers/milkdrop/" + sm.ID} {
		resp := h.authed(t, "GET", path, nil)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(b), "hlsl") || strings.Contains(string(b), "tex2D") {
			t.Errorf("%s serves the HLSL: %s", path, b)
		}
	}
	if src, err := h.srvAPI.presets.HLSL(sm.ID); err != nil || src == nil || !strings.Contains(src.Warp, "tex2D(sampler_main, uv)") {
		t.Errorf("stored HLSL = %+v, %v; want it kept on the server", src, err)
	}
}

func TestAShaderThatWillNotConvertIsStoredWithTheReason(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	bad := strings.Replace(testMilk, "tex2D(sampler_main, uv).xyz;", "tex2D(sampler_main, uv).xyz * brightnes;", 1)
	resp := h.uploadPreset(t, "bad shader.milk", bad)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201: a shader that will not convert is not a refusal", resp.StatusCode)
	}
	var sm milkdrop.Summary
	decode(t, resp, &sm)
	if sm.Shaders != "not_converted" || !strings.HasPrefix(sm.ShadersReason, "warp: HLSL parse error:") ||
		!strings.Contains(sm.ShadersReason, "'brightnes' : undeclared identifier") {
		t.Errorf("summary = %+v, want not_converted with the converter's first line", sm)
	}
	var e milkdrop.Entry
	decode(t, h.authed(t, "GET", "/api/visualizers/milkdrop/"+sm.ID, nil), &e)
	if e.Preset.Warp != "" || e.ShadersReason != sm.ShadersReason {
		t.Errorf("stored warp %q, reason %q: want the defaults, and the reason served", e.Preset.Warp, e.ShadersReason)
	}
}

// failingConverter is a converter that is itself broken: the module did not
// compile, say. Nothing about the shader.
type failingConverter struct{}

func (failingConverter) Convert(context.Context, string) (string, error) {
	return "", errors.New("shader converter: compile module: test")
}

func TestABrokenConverterStillStoresThePreset(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	h.srvAPI.shaders = failingConverter{}
	resp := h.uploadPreset(t, "a.milk", testMilk)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201: nothing is refused that step 1 accepts", resp.StatusCode)
	}
	var sm milkdrop.Summary
	decode(t, resp, &sm)
	if sm.Shaders != "not_converted" || !strings.Contains(sm.ShadersReason, "not working on this server") {
		t.Errorf("summary = %+v", sm)
	}
}

/*
 * A preset uploaded to v0.9.92 had its shaders dropped and no HLSL kept, so the
 * only way to its shaders is to upload it again. That must give one preset,
 * converted, under the name it already had — not a second copy beside it.
 */
func TestReuploadingAPresetStoredWithoutShadersReplacesIt(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	old, err := milkdrop.Parse([]byte(testMilk), "Old Name.milk")
	if err != nil {
		t.Fatal(err)
	}
	old.HLSL = nil // as v0.9.92 stored it
	first, _, _, err := h.srvAPI.presets.Put(old)
	if err != nil {
		t.Fatal(err)
	}

	resp := h.uploadPreset(t, "Test - Gentle Drift.milk", testMilk)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var sm milkdrop.Summary
	decode(t, resp, &sm)
	if sm.Shaders != "converted" || sm.Name != "Old Name" || sm.ID == first.ID {
		t.Errorf("summary = %+v, want converted, under the old name, with a new id", sm)
	}
	var list milkdropList
	decode(t, h.authed(t, "GET", "/api/visualizers/milkdrop", nil), &list)
	if len(list.Presets) != 1 || list.Presets[0].ID != sm.ID {
		t.Fatalf("list = %+v, want only the converted preset", list.Presets)
	}
	wantError(t, h.authed(t, "GET", "/api/visualizers/milkdrop/"+first.ID, nil), http.StatusNotFound, "not_found")
}

func TestTheSamePresetTwiceIsOnePreset(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	var first, again milkdrop.Summary
	decode(t, h.uploadPreset(t, "first.milk", testMilk), &first)
	resp := h.uploadPreset(t, "second name.milk", testMilk)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("second upload = %d, want 200", resp.StatusCode)
	}
	decode(t, resp, &again)
	if again.ID != first.ID || again.Name != "first" {
		t.Errorf("second upload = %+v, want the first, unrenamed", again)
	}
	var list milkdropList
	decode(t, h.authed(t, "GET", "/api/visualizers/milkdrop", nil), &list)
	if len(list.Presets) != 1 {
		t.Errorf("%d presets, want 1", len(list.Presets))
	}
}

func TestAPresetThatDoesNotParseIsRefusedWithWhere(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	bad := strings.Replace(testMilk, "per_frame_2=rot = rot + 0.02*bass_att;", "per_frame_2=rot = invsqrt(bass);", 1)
	resp := h.uploadPreset(t, "bad.milk", bad)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body struct {
		Error apiError `json:"error"`
	}
	decode(t, resp, &body)
	if body.Error.Code != "invalid_preset" ||
		body.Error.Message != "per_frame_2, column 7: unknown function invsqrt()" {
		t.Errorf("error = %+v, want invalid_preset naming per_frame_2 and the function", body.Error)
	}
	var list milkdropList
	decode(t, h.authed(t, "GET", "/api/visualizers/milkdrop", nil), &list)
	if len(list.Presets) != 0 {
		t.Errorf("a refused preset was stored: %+v", list.Presets)
	}
}

func TestButterchurnJSONIsAcceptedAfterKeyValidation(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	good := `{"version":2,"baseVals":{"decay":0.9},"init_eqs_eel":"","frame_eqs_eel":"q1 = bass;",` +
		`"pixel_eqs_eel":"","warp":"void main() {}","comp":"",` +
		`"shapes":[{"baseVals":{"enabled":0},"init_eqs_eel":"","frame_eqs_eel":""}],"waves":[]}`
	resp := h.uploadPreset(t, "pack/converted.json", good)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var sm milkdrop.Summary
	decode(t, resp, &sm)
	if sm.Source != "json" || sm.Shaders != "converted" || sm.Name != "converted" {
		t.Errorf("summary = %+v", sm)
	}

	for name, body := range map[string]string{
		"javascript":  `{"init_eqs_str":"a['q1']=1;","frame_eqs_eel":""}`,
		"unknown key": `{"frame_eqs_eel":"","onload":"x"}`,
		"bad eel":     `{"frame_eqs_eel":"q1 = ;"}`,
		"html":        `<html><script>alert(1)</script></html>`,
	} {
		resp := h.uploadPreset(t, name+".json", body)
		wantError(t, resp, http.StatusBadRequest, "invalid_preset")
	}
}

func TestAMemberCannotUploadOrRemoveAPreset(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	var sm milkdrop.Summary
	decode(t, h.uploadPreset(t, "a.milk", testMilk), &sm)

	m := h.member(t, "georgia", "another long password")
	resp := h.raw(t, "POST", "/api/visualizers/milkdrop?filename=x.milk", m.cookie, "", []byte(testMilk))
	wantError(t, resp, http.StatusForbidden, "forbidden")
	resp = h.raw(t, "DELETE", "/api/visualizers/milkdrop/"+sm.ID, m.cookie, "", nil)
	wantError(t, resp, http.StatusForbidden, "forbidden")
}

// Upload is a state-changing method, so the Origin check applies to it as to
// every other: a page elsewhere cannot make an admin's browser add a preset.
func TestACrossOriginUploadIsRefused(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	resp := h.raw(t, "POST", "/api/visualizers/milkdrop?filename=x.milk", h.cookie, "https://evil.example", []byte(testMilk))
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAnOversizedUploadIs413(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	big := testMilk + strings.Repeat("// padding\n", milkdrop.MaxUploadBytes/11+1)
	wantError(t, h.uploadPreset(t, "big.milk", big), http.StatusRequestEntityTooLarge, "too_large")
}

// The filename is a display name. However it is written, the file on disk is
// named by the preset's hash and sits in the preset folder.
func TestAFilenameNeverBecomesAPath(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	var sm milkdrop.Summary
	decode(t, h.uploadPreset(t, `..\..\..\Windows\evil.milk`, testMilk), &sm)
	if sm.Name != "evil" {
		t.Errorf("name = %q, want evil", sm.Name)
	}
	dir := filepath.Join(h.dataDir, "visualizers", "milkdrop")
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(des) != 1 || des[0].Name() != sm.ID+".json" {
		t.Errorf("preset folder holds %v, want only %s.json", des, sm.ID)
	}
}

func TestRemovingAPreset(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	var sm milkdrop.Summary
	decode(t, h.uploadPreset(t, "a.milk", testMilk), &sm)

	resp := h.authed(t, "DELETE", "/api/visualizers/milkdrop/"+sm.ID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()
	wantError(t, h.authed(t, "GET", "/api/visualizers/milkdrop/"+sm.ID, nil), http.StatusNotFound, "not_found")
	wantError(t, h.authed(t, "DELETE", "/api/visualizers/milkdrop/"+sm.ID, nil), http.StatusNotFound, "not_found")
}

func TestAnIdThatIsNotAHashIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.secure(t, "correct horse battery")
	for _, id := range []string{"nothex", "..", strings.Repeat("A", 64), "%2e%2e%5cwin.ini"} {
		wantError(t, h.authed(t, "GET", "/api/visualizers/milkdrop/"+id, nil), http.StatusNotFound, "not_found")
	}
}
