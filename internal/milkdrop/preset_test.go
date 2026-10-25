package milkdrop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Named tests per rule, as in internal/probe/decide_test.go. The presets here
// are written for the tests: the repository carries no real ones (ADR 0091).

func milk(lines ...string) []byte {
	return []byte("[preset00]\n" + strings.Join(lines, "\n") + "\n")
}

func mustParse(t *testing.T, body []byte) Upload {
	t.Helper()
	u, err := Parse(body, "test.milk")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return u
}

func refused(t *testing.T, body []byte, reason string) {
	t.Helper()
	_, err := Parse(body, "test.milk")
	if err == nil {
		t.Fatalf("accepted, want refused for %q", reason)
	}
	if !IsInvalid(err) {
		t.Errorf("err = %v (%T), want an Invalid", err, err)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Errorf("err = %q, want it to contain %q", err, reason)
	}
}

func TestMilkBaseValuesTakeButterchurnsNames(t *testing.T) {
	u := mustParse(t, milk("fDecay=0.5", "fVideoEchoZoom=2.0", "nWaveMode=3", "zoom=1.01", "bTexWrap=1"))
	want := map[string]float64{"decay": 0.5, "echo_zoom": 2, "wave_mode": 3, "zoom": 1.01, "wrap": 1}
	for k, v := range want {
		if u.Preset.BaseVals[k] != v {
			t.Errorf("%s = %v, want %v", k, u.Preset.BaseVals[k], v)
		}
	}
	if _, ok := u.Preset.BaseVals["fdecay"]; ok {
		t.Error("kept the .milk name fdecay as well")
	}
}

// parseFloat reads a leading number and ignores the rest, and a value that is
// not a number at all is skipped rather than refusing the file.
func TestMilkValuesAreReadAsParseFloatReadsThem(t *testing.T) {
	u := mustParse(t, milk("zoom=0.5000 ; tuned", "rot=.25", "warp=abc"))
	if u.Preset.BaseVals["zoom"] != 0.5 || u.Preset.BaseVals["rot"] != 0.25 {
		t.Errorf("base values = %v", u.Preset.BaseVals)
	}
	if _, ok := u.Preset.BaseVals["warp"]; ok {
		t.Error("kept warp=abc, which is not a number")
	}
}

func TestMilkVersionComesFromTheFile(t *testing.T) {
	if v := mustParse(t, milk("MILKDROP_PRESET_VERSION=201")).Preset.Version; v != 2 {
		t.Errorf("MilkDrop 2 file: version %d, want 2", v)
	}
	if v := mustParse(t, milk("zoom=1")).Preset.Version; v != 1 {
		t.Errorf("MilkDrop 1 file: version %d, want 1", v)
	}
	if _, ok := mustParse(t, milk("MILKDROP_PRESET_VERSION=201")).Preset.BaseVals["milkdrop_preset_version"]; ok {
		t.Error("the file's version was stored as a base value")
	}
}

// Lines are put back in number order, joined with "\n", which eel-wasm reads
// as nothing: `is_` + `beat` is `is_beat`, as in MilkDrop.
func TestMilkLinesJoinInNumberOrder(t *testing.T) {
	u := mustParse(t, milk("per_frame_2=beat*2;", "per_frame_1=k1 = is_", "per_frame_10=q1 = k1;"))
	if u.Preset.FrameEqs != "k1 = is_\nbeat*2;\nq1 = k1;" {
		t.Errorf("frame_eqs_eel = %q", u.Preset.FrameEqs)
	}
}

func TestMilkShadersAreDroppedAndSaidSo(t *testing.T) {
	u := mustParse(t, milk("warp_1=`shader_body {", "warp_2=`}", "comp_1=`ret = 1;"))
	if u.Shaders != ShadersNotConverted || u.Preset.Warp != "" || u.Preset.Comp != "" {
		t.Errorf("shaders = %q, warp %q, comp %q", u.Shaders, u.Preset.Warp, u.Preset.Comp)
	}
	if u := mustParse(t, milk("zoom=1")); u.Shaders != ShadersNone {
		t.Errorf("no shaders: %q, want none", u.Shaders)
	}
	// MilkDrop 1 files carry empty shader lines; that is not a shader.
	if u := mustParse(t, milk("warp_1=`", "comp_1=")); u.Shaders != ShadersNone {
		t.Errorf("empty shader lines: %q, want none", u.Shaders)
	}
}

// MilkDrop's default for a shape or wave is off; butterchurn's, when the key
// is missing, is on. The reader writes MilkDrop's down so butterchurn agrees.
func TestMilkShapesAndWavesAreOffUnlessEnabled(t *testing.T) {
	u := mustParse(t, milk("shapecode_1_enabled=1", "shapecode_1_sides=5", "shape_1_per_frame1=x = 0.5;"))
	if u.Preset.Shapes[0].BaseVals["enabled"] != 0 || u.Preset.Waves[3].BaseVals["enabled"] != 0 {
		t.Error("a shape or wave the file never mentions should be written down as off")
	}
	s := u.Preset.Shapes[1]
	if s.BaseVals["enabled"] != 1 || s.BaseVals["sides"] != 5 || s.FrameEqs != "x = 0.5;" {
		t.Errorf("shape 1 = %+v", s)
	}
}

func TestMilkShapeAndWaveNamesAreMapped(t *testing.T) {
	u := mustParse(t, milk("wavecode_0_bAdditive=1", "wavecode_0_bUseDots=1", "wavecode_0_bDrawThick=1",
		"shapecode_0_thickOutline=1", "shapecode_0_num_inst=40"))
	w, s := u.Preset.Waves[0].BaseVals, u.Preset.Shapes[0].BaseVals
	if w["additive"] != 1 || w["usedots"] != 1 || w["thick"] != 1 {
		t.Errorf("wave = %v", w)
	}
	if s["thickoutline"] != 1 || s["num_inst"] != 40 {
		t.Errorf("shape = %v", s)
	}
}

// A disabled shape is never compiled, so its code is no reason to refuse —
// and since it was not checked, it is not kept.
func TestDisabledCodeIsNeitherCheckedNorKept(t *testing.T) {
	u := mustParse(t, milk("shapecode_0_enabled=0", "shape_0_per_frame1=x = (;"))
	if u.Preset.Shapes[0].FrameEqs != "" {
		t.Errorf("kept unchecked code %q", u.Preset.Shapes[0].FrameEqs)
	}
	refused(t, milk("shapecode_0_enabled=1", "shape_0_per_frame1=x = (;"), "shape_0_per_frame1, column 6")
}

func TestARefusalNamesTheMilkLine(t *testing.T) {
	refused(t, milk("per_frame_1=a = 1;", "per_frame_2=b = fmod(a, 2);"), "per_frame_2, column 5: unknown function fmod()")
	refused(t, milk("wavecode_2_enabled=1", "wave_2_per_point3=y = ?;"), "wave_2_per_point3, column 5")
}

func TestAMilkNeedsItsSection(t *testing.T) {
	refused(t, []byte("zoom=1\nper_frame_1=a=1;\n"), "no [preset00] section")
}

func TestUnreadableUploadsAreRefused(t *testing.T) {
	_, err := Parse(nil, "x.milk")
	if !IsInvalid(err) || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty: %v", err)
	}
	_, err = Parse([]byte("<!doctype html><script>x</script>"), "x.milk")
	if !IsInvalid(err) || !strings.Contains(err.Error(), "markup") {
		t.Errorf("html: %v", err)
	}
	_, err = Parse(make([]byte, MaxUploadBytes+1), "x.milk")
	if !IsInvalid(err) {
		t.Errorf("oversized: %v", err)
	}
}

// A 2005 .milk is Latin-1; what it says must survive into the stored text.
func TestLatin1MilkIsDecoded(t *testing.T) {
	u := mustParse(t, []byte("[preset00]\nper_frame_1=a = 1; // caf\xe9\n"))
	if !strings.HasSuffix(u.Preset.FrameEqs, "// café") {
		t.Errorf("frame_eqs_eel = %q", u.Preset.FrameEqs)
	}
}

// ---- butterchurn JSON ------------------------------------------------------------

func parseJ(t *testing.T, s string) (Upload, error) {
	t.Helper()
	return Parse([]byte(s), "x.json")
}

func TestJSONIsStoredInFullShape(t *testing.T) {
	u, err := parseJ(t, `{"version":1,"baseVals":{"zoom":1.1},"frame_eqs_eel":"q1 = 1;"}`)
	if err != nil {
		t.Fatal(err)
	}
	p := u.Preset
	if p.Version != 1 || p.BaseVals["zoom"] != 1.1 || p.FrameEqs != "q1 = 1;" {
		t.Errorf("preset = %+v", p)
	}
	for i, s := range p.Shapes {
		if s.BaseVals == nil || s.BaseVals["enabled"] != 0 {
			t.Errorf("shape %d = %+v, want present and off", i, s)
		}
	}
	if u.Shaders != ShadersNone {
		t.Errorf("shaders = %q", u.Shaders)
	}
}

func TestJSONVersionDefaultsToTwo(t *testing.T) {
	u, err := parseJ(t, `{"frame_eqs_eel":""}`)
	if err != nil || u.Preset.Version != 2 {
		t.Errorf("version = %d, err %v; butterchurn reads a missing version as 2", u.Preset.Version, err)
	}
	if _, err := parseJ(t, `{"version":3}`); !IsInvalid(err) {
		t.Errorf("version 3: %v", err)
	}
}

func TestJSONJavaScriptIsRefusedByName(t *testing.T) {
	for _, s := range []string{
		`{"init_eqs_str":"a.q1=1"}`,
		`{"shapes":[{"baseVals":{},"frame_eqs_str":"x"}]}`,
		`{"waves":[{"baseVals":{},"point_eqs_str":"x"}]}`,
	} {
		_, err := parseJ(t, s)
		if !IsInvalid(err) || !strings.Contains(err.Error(), "JavaScript") {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestJSONUnknownKeysAreRefused(t *testing.T) {
	for _, s := range []string{
		`{"name":"x"}`,
		`{"shapes":[{"baseVals":{},"onclick":"x"}]}`,
	} {
		if _, err := parseJ(t, s); !IsInvalid(err) || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("%s: %v", s, err)
		}
	}
}

func TestJSONShapesAreBounded(t *testing.T) {
	five := `{"shapes":[{},{},{},{},{}]}`
	if _, err := parseJ(t, five); !IsInvalid(err) {
		t.Errorf("five shapes: %v", err)
	}
}

func TestJSONTypesAreChecked(t *testing.T) {
	_, err := parseJ(t, `{"baseVals":{"zoom":"1.0"}}`)
	if !IsInvalid(err) || !strings.Contains(err.Error(), "should be a number") {
		t.Errorf("string base value: %v", err)
	}
	if _, err := parseJ(t, `{"frame_eqs_eel":""} {"frame_eqs_eel":""}`); !IsInvalid(err) {
		t.Errorf("two documents: %v", err)
	}
}

func TestBaseValueNamesAreChecked(t *testing.T) {
	for _, k := range []string{"__proto__", "constructor", "Zoom", "a-b", "1x"} {
		_, err := parseJ(t, `{"baseVals":{"`+k+`":1}}`)
		if !IsInvalid(err) || !strings.Contains(err.Error(), "not a base value name") {
			t.Errorf("%q: %v", k, err)
		}
	}
	// In a .milk the same names are skipped, as MilkDrop would skip them.
	u := mustParse(t, milk("__proto__=1", "constructor=2", "zoom=1"))
	if len(u.Preset.BaseVals) != 1 {
		t.Errorf("base values = %v, want only zoom", u.Preset.BaseVals)
	}
}

func TestJSONShadersAreKeptAndBounded(t *testing.T) {
	u, err := parseJ(t, `{"warp":"void main(){}"}`)
	if err != nil || u.Shaders != ShadersConverted || u.Preset.Warp != "void main(){}" {
		t.Errorf("upload = %+v, err %v", u, err)
	}
	big := `{"comp":"` + strings.Repeat("x", MaxShaderBytes+1) + `"}`
	if _, err := parseJ(t, big); !IsInvalid(err) {
		t.Errorf("oversized shader: %v", err)
	}
}

func TestJSONRefusalNamesTheKey(t *testing.T) {
	_, err := parseJ(t, `{"waves":[{"baseVals":{"enabled":1},"point_eqs_eel":"x = 1;\ny = sin();"}]}`)
	want := "waves[0].point_eqs_eel: line 2, column 5: sin() takes 1 argument, given 0"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

// ---- names -----------------------------------------------------------------------

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"Rovastar - Destiny Star.milk":   "Rovastar - Destiny Star",
		`C:\Presets\pack\One.MILK`:       "One",
		"../../etc/passwd":               "passwd",
		"two  spaces\tand\na newline":    "two spaces and a newline",
		"bidi\u202eexe.milk":             "bidiexe",
		"":                               "Untitled preset",
		"   .milk":                       "Untitled preset",
		strings.Repeat("é", 200):         strings.Repeat("é", maxNameRunes),
		"butterchurn/converted.json":     "converted",
		"not-an-extension.milkshake.txt": "not-an-extension.milkshake.txt",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- the store ---------------------------------------------------------------------

func TestStoreKeepsPresetsByHash(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "visualizers", "milkdrop")
	s := NewStore(dir)
	u := mustParse(t, milk("zoom=1", "per_frame_1=q1 = 1;"))
	u.Name = "One"
	sm, created, _, err := s.Put(u)
	if err != nil || !created {
		t.Fatalf("Put: %+v %v %v", sm, created, err)
	}
	if _, err := os.Stat(filepath.Join(dir, sm.ID+".json")); err != nil {
		t.Errorf("no file named by the hash: %v", err)
	}

	// A second store over the same folder reads it back: the file is the
	// whole truth, with no table beside it.
	again := NewStore(dir)
	list, err := again.List()
	if err != nil || len(list) != 1 || list[0] != sm {
		t.Fatalf("List from disk = %+v, %v; want %+v", list, err, sm)
	}
	e, err := again.Get(sm.ID)
	if err != nil || e.Preset.FrameEqs != "q1 = 1;" {
		t.Errorf("Get = %+v, %v", e, err)
	}
}

func TestStoreListsByNameIgnoringCase(t *testing.T) {
	s := NewStore(t.TempDir())
	for i, name := range []string{"beta", "Alpha", "gamma"} {
		u := mustParse(t, milk("zoom="+string(rune('1'+i))))
		u.Name = name
		if _, _, _, err := s.Put(u); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := s.List()
	var names []string
	for _, sm := range list {
		names = append(names, sm.Name)
	}
	if strings.Join(names, ",") != "Alpha,beta,gamma" {
		t.Errorf("order = %v", names)
	}
}

// One damaged file must not hide every other preset.
func TestStoreSkipsADamagedFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	u := mustParse(t, milk("zoom=1"))
	sm, _, _, err := s.Put(u)
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Repeat("0", 64)
	if err := os.WriteFile(filepath.Join(dir, bad+".json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file that claims another preset's name is damaged too.
	other := strings.Repeat("1", 64)
	b, _ := os.ReadFile(filepath.Join(dir, sm.ID+".json"))
	if err := os.WriteFile(filepath.Join(dir, other+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := NewStore(dir).List()
	if err != nil || len(list) != 1 || list[0].ID != sm.ID {
		t.Errorf("List = %+v, %v; want only the good preset", list, err)
	}
}

// An id comes from a URL. Anything that is not a hash is not found, before any
// path is built from it.
func TestStoreRefusesWhatIsNotAnID(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, id := range []string{"", "..", `..\..\x`, "../" + strings.Repeat("a", 61), strings.Repeat("A", 64), strings.Repeat("a", 65)} {
		if _, err := s.Get(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", id, err)
		}
		if _, err := s.Delete(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestStoreDelete(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	sm, _, _, err := s.Put(mustParse(t, milk("zoom=1")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete(sm.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Errorf("still listed: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(dir, sm.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
	if _, err := s.Delete(sm.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

// The id is the preset's own hash, so it cannot depend on the name or the
// moment it came.
func TestIDIsThePresetAlone(t *testing.T) {
	a := mustParse(t, milk("zoom=1"))
	b := mustParse(t, milk("zoom=1"))
	b.Name = "something else"
	ia, _ := ID(a.Preset)
	ib, _ := ID(b.Preset)
	if ia != ib || len(ia) != 64 {
		t.Errorf("ids %s / %s", ia, ib)
	}
	c := mustParse(t, milk("zoom=1.5"))
	if ic, _ := ID(c.Preset); ic == ia {
		t.Error("different presets share an id")
	}
}
