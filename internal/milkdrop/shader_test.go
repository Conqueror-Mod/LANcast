package milkdrop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lancast/internal/milkdrop/msc"
)

/*
 * The shader chain (ADR 0091, steps g2 and g3).
 *
 * The fixtures in testdata/shaders are LANcast's own presets. golden.json was
 * made from them by the spike's JavaScript chain (pipeline.mjs, by way of
 * testdata/shaders/golden.mjs) and the same module, so these hold the Go port
 * to what butterchurn's packs were made with, byte for byte. The corpus test
 * (shadercorpus_test.go) does the same over 9,141 shaders kept outside the
 * repository.
 *
 * The rest are decisions, one named test each, with a fake converter: they
 * are about what ConvertShaders and the store do with an answer, not about the
 * converter, which internal/milkdrop/msc tests on its own.
 */

var (
	convOnce sync.Once
	realMSC  *msc.Converter
)

func moduleConverter() *msc.Converter {
	convOnce.Do(func() { realMSC = msc.New() })
	return realMSC
}

type goldenShader struct {
	Source     string `json:"source"`
	HLSLSHA256 string `json:"hlsl_sha256"`
	RC         int    `json:"rc"`
	GLSL       string `json:"glsl"`
	FirstLine  string `json:"first_line"`
}

func TestTheShaderChainMatchesPipelineMjsOnLANcastsFixtures(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "shaders", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]map[string]goldenShader
	if err := json.Unmarshal(b, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) < 7 {
		t.Fatalf("golden.json has %d fixtures", len(golden))
	}
	for file, want := range golden {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", "shaders", file))
			if err != nil {
				t.Fatal(err)
			}
			u, err := Parse(body, file)
			if err != nil {
				t.Fatal(err)
			}
			if len(want) == 0 {
				if u.Shaders != ShadersNone || u.HLSL != nil {
					t.Fatalf("shaders %q, hlsl %v; want none", u.Shaders, u.HLSL)
				}
				return
			}
			if u.Shaders != ShadersNotConverted || u.HLSL == nil {
				t.Fatalf("Parse: shaders %q, hlsl %v; want not_converted with the text kept", u.Shaders, u.HLSL)
			}
			src := map[string]string{"warp": u.HLSL.Warp, "comp": u.HLSL.Comp}
			for _, kind := range []string{"warp", "comp"} {
				g, ok := want[kind]
				if !ok {
					if src[kind] != "" {
						t.Errorf("%s: split %q where splitPreset found none", kind, src[kind])
					}
					continue
				}
				if src[kind] != g.Source {
					t.Errorf("%s: split\n%q\nwant\n%q", kind, src[kind], g.Source)
				}
				sum := sha256.Sum256([]byte(prepareShader(src[kind])))
				if got := hex.EncodeToString(sum[:]); got != g.HLSLSHA256 {
					t.Errorf("%s: prepared HLSL differs from prepareShader's (sha256 %s, want %s)", kind, got, g.HLSLSHA256)
				}
			}

			if err := ConvertShaders(context.Background(), moduleConverter(), &u); err != nil {
				t.Fatal(err)
			}
			failed := ""
			for _, kind := range []string{"warp", "comp"} {
				if g, ok := want[kind]; ok && g.RC != 0 && failed == "" {
					failed = kind + ": " + g.FirstLine
				}
			}
			if failed != "" {
				if u.Shaders != ShadersNotConverted || u.ShadersReason != failed {
					t.Fatalf("shaders %q, reason %q; want not_converted, %q", u.Shaders, u.ShadersReason, failed)
				}
				if u.Preset.Warp != "" || u.Preset.Comp != "" {
					t.Fatal("a preset whose shaders did not all convert kept some of them")
				}
				return
			}
			if u.Shaders != ShadersConverted || u.ShadersReason != "" {
				t.Fatalf("shaders %q, reason %q; want converted", u.Shaders, u.ShadersReason)
			}
			if u.Preset.Warp != want["warp"].GLSL {
				t.Errorf("warp\n%s\nwant\n%s", u.Preset.Warp, want["warp"].GLSL)
			}
			if u.Preset.Comp != want["comp"].GLSL {
				t.Errorf("comp\n%s\nwant\n%s", u.Preset.Comp, want["comp"].GLSL)
			}
		})
	}
}

/*
 * A converted .milk must pass the same validator a butterchurn JSON upload
 * does. The spike checked this on 1,107 presets; here it is checked on every
 * fixture, so the two readers cannot drift apart.
 */
func TestAConvertedMilkIsAcceptedAsButterchurnJSON(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "shaders", "drift.milk"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := Parse(body, "drift.milk")
	if err != nil {
		t.Fatal(err)
	}
	if err := ConvertShaders(context.Background(), moduleConverter(), &u); err != nil || u.Shaders != ShadersConverted {
		t.Fatalf("convert: %v, %q", err, u.ShadersReason)
	}
	js, err := json.Marshal(u.Preset)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(js, "drift.json")
	if err != nil {
		t.Fatalf("the converted preset, as JSON, is refused: %v", err)
	}
	if again.Preset.Warp != u.Preset.Warp || again.Shaders != ShadersConverted {
		t.Fatal("the JSON reader changed the converted shaders")
	}
}

// ---- decisions, with a fake converter ---------------------------------------------

type fakeConv struct {
	calls []string
	// answer is called with which shader this is (warp first) and its HLSL.
	answer func(n int, hlsl string) (string, error)
}

func (f *fakeConv) Convert(ctx context.Context, hlsl string) (string, error) {
	f.calls = append(f.calls, hlsl)
	return f.answer(len(f.calls), hlsl)
}

// glslFor is converter output shaped as glsl-optimizer prints it.
func glslFor(body string) string {
	return "#version 300 es\nout lowp vec4 _glesFragData[4];\nuniform sampler2D sampler_main;\nin highp vec2 xlv_TEXCOORD0;\nvoid main ()\n{\n  _glesFragData[0] = " + body + ";\n}\n\n"
}

func ok(n int, hlsl string) (string, error) {
	return glslFor("texture (sampler_main, xlv_TEXCOORD0)"), nil
}

const twoShaders = "[preset00]\nMILKDROP_PRESET_VERSION=201\nper_frame_1=zoom = 1.0;\n" +
	"warp_1=`shader_body { ret = tex2D(sampler_main, uv).xyz; }\n" +
	"comp_1=`shader_body { ret = GetPixel(uv); }\n"

func parsed(t *testing.T, milk string) Upload {
	t.Helper()
	u, err := Parse([]byte(milk), "test.milk")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestParseKeepsTheShadersUnconvertedForConvertShaders(t *testing.T) {
	u := parsed(t, twoShaders)
	if u.Shaders != ShadersNotConverted || u.Preset.Warp != "" || u.Preset.Comp != "" {
		t.Fatalf("Parse converted or kept GLSL: %+v", u)
	}
	if u.HLSL == nil || !strings.Contains(u.HLSL.Warp, "tex2D(sampler_main, uv)") || !strings.Contains(u.HLSL.Comp, "GetPixel") {
		t.Fatalf("hlsl = %+v, want the shader text without its backticks", u.HLSL)
	}
}

func TestBothShadersConvertedArePrepared(t *testing.T) {
	u := parsed(t, twoShaders)
	f := &fakeConv{answer: ok}
	if err := ConvertShaders(context.Background(), f, &u); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("%d conversions, want warp and comp", len(f.calls))
	}
	for _, h := range f.calls {
		if !strings.Contains(h, "float4 xlat_main (float2 uv : TEXCOORD0) : COLOR0") || !strings.Contains(h, "#define GetBlur1(uv)") {
			t.Fatalf("the converter was not given a prepared shader:\n%s", h)
		}
	}
	if u.Shaders != ShadersConverted || u.Preset.Warp != " shader_body { \n  ret = texture (sampler_main, uv).xyz;\n }" {
		t.Fatalf("shaders %q, warp %q", u.Shaders, u.Preset.Warp)
	}
}

func TestOneShaderFailingDropsBoth(t *testing.T) {
	u := parsed(t, twoShaders)
	f := &fakeConv{answer: func(n int, hlsl string) (string, error) {
		if n == 2 {
			return "", &msc.Failure{Reason: "HLSL parse error:(140): ERROR: 'x' : undeclared identifier"}
		}
		return ok(n, hlsl)
	}}
	if err := ConvertShaders(context.Background(), f, &u); err != nil {
		t.Fatal(err)
	}
	if u.Shaders != ShadersNotConverted || u.Preset.Warp != "" || u.Preset.Comp != "" {
		t.Fatalf("shaders %q, warp %q: one failed, so none is kept", u.Shaders, u.Preset.Warp)
	}
	if u.ShadersReason != "comp: HLSL parse error:(140): ERROR: 'x' : undeclared identifier" {
		t.Fatalf("reason %q, want the shader named and the converter's line", u.ShadersReason)
	}
	if u.Replaces != "" || u.HLSL == nil {
		t.Fatal("a preset that did not convert replaces nothing and keeps its HLSL")
	}
}

func TestABrokenConverterIsNotARefusal(t *testing.T) {
	u := parsed(t, twoShaders)
	broken := errors.New("shader converter: compile module: boom")
	err := ConvertShaders(context.Background(), &fakeConv{answer: func(int, string) (string, error) { return "", broken }}, &u)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the converter's own error, for the log", err)
	}
	if u.Shaders != ShadersNotConverted || !strings.Contains(u.ShadersReason, "not working on this server") {
		t.Fatalf("shaders %q, reason %q: the upload must still be storable, as step 1 stored it", u.Shaders, u.ShadersReason)
	}
}

func TestAnEndedRequestIsNotTheShadersFault(t *testing.T) {
	u := parsed(t, twoShaders)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ConvertShaders(ctx, &fakeConv{answer: func(int, string) (string, error) { return "", ctx.Err() }}, &u)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the request's", err)
	}
	if u.ShadersReason != "" {
		t.Fatalf("reason %q recorded for a request that ended", u.ShadersReason)
	}
}

func TestAConvertedShaderOverTheLimitIsNotKept(t *testing.T) {
	u := parsed(t, twoShaders)
	big := glslFor("vec4(" + strings.Repeat("1.0+", MaxShaderBytes/4) + "1.0)")
	if err := ConvertShaders(context.Background(), &fakeConv{answer: func(int, string) (string, error) { return big, nil }}, &u); err != nil {
		t.Fatal(err)
	}
	if u.Shaders != ShadersNotConverted || !strings.HasPrefix(u.ShadersReason, "warp: converted, it is over 64 KB") {
		t.Fatalf("shaders %q, reason %q", u.Shaders, u.ShadersReason)
	}
}

func TestAConvertedPresetNamesThePresetItWasWithoutShaders(t *testing.T) {
	u := parsed(t, twoShaders)
	before, err := ID(u.Preset)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConvertShaders(context.Background(), &fakeConv{answer: ok}, &u); err != nil {
		t.Fatal(err)
	}
	after, _ := ID(u.Preset)
	if u.Replaces != before || after == before {
		t.Fatalf("replaces %q (want %q), id %q", u.Replaces, before, after)
	}
}

func TestJSONAndShaderlessUploadsAreNotConverted(t *testing.T) {
	f := &fakeConv{answer: ok}
	for _, u := range []Upload{
		parsed(t, "[preset00]\nper_frame_1=zoom = 1.0;\n"),
		{Source: SourceJSON, Shaders: ShadersConverted},
	} {
		if err := ConvertShaders(context.Background(), f, &u); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("%d conversions, want none", len(f.calls))
	}
}

// ---- the string steps' oddities, kept on purpose ----------------------------------

func TestJSSubstringClampsAndSwaps(t *testing.T) {
	for _, c := range []struct {
		a, b int
		want string
	}{{0, 3, "abc"}, {3, 0, "abc"}, {-5, 2, "ab"}, {2, 99, "cdef"}, {4, -1, "abcd"}} {
		if got := jsSubstring("abcdef", c.a, c.b); got != c.want {
			t.Errorf("substring(%d, %d) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}

func TestAShaderWithoutBracesKeepsWhatJavaScriptKept(t *testing.T) {
	// No "{": the body runs from the start of shader_body to the last "}",
	// which is none, so substring(0, -1) is "".
	if before, body := shaderParts("float x; shader_body ret = 1;"); before != "float x; " || body != "" {
		t.Fatalf("before %q, body %q", before, body)
	}
	// No shader_body at all: the whole text is the body.
	if before, body := shaderParts("ret = 1;"); before != "" || body != "ret = 1;" {
		t.Fatalf("before %q, body %q", before, body)
	}
}

func TestSplitShadersTakesTheTextAfterTheLastMarker(t *testing.T) {
	s := splitShaders("[preset00]\r\nper_frame_1=x=1;\r\nwarp_1=``ret = 0;\r\nwarp_2=`// warp_9=`kept\r\ncomp_1=`c\r\n")
	if s.Warp != "ret = 0;\nkept" || s.Comp != "c" {
		t.Fatalf("split = %+v", s)
	}
}

func TestProcessingDropsHeaderLinesStartingIn(t *testing.T) {
	// milkdrop-preset-utils drops every declaration line that starts "in" —
	// the inputs it meant, and an `int` it did not. Kept, as the packs have it.
	out := processOptimizedShader("#version 300 es\nint k;\nfloat f;\nuniform sampler2D sampler_main;\nuniform sampler2D sampler_mine;\nvoid main ()\n{\n  xlat_mutableuv = uv;\n  _glesFragData[0] = vec4(1.0);\n}\n")
	if out != "float f;\nuniform sampler2D sampler_mine;\n shader_body { \n  ret = vec4(1.0).xyz;\n }" {
		t.Fatalf("got %q", out)
	}
}

// ---- the store ---------------------------------------------------------------------

func convertedUpload(t *testing.T) Upload {
	t.Helper()
	u := parsed(t, twoShaders)
	if err := ConvertShaders(context.Background(), &fakeConv{answer: ok}, &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTheHLSLIsKeptButNeverServed(t *testing.T) {
	s := NewStore(t.TempDir())
	u := convertedUpload(t)
	sm, _, _, err := s.Put(u)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Get(sm.ID)
	if err != nil {
		t.Fatal(err)
	}
	served, _ := json.Marshal(e)
	if strings.Contains(string(served), "hlsl") || strings.Contains(string(served), "tex2D") {
		t.Fatalf("Get serves the HLSL: %s", served)
	}
	h, err := s.HLSL(sm.ID)
	if err != nil || h == nil || h.Warp != u.HLSL.Warp || h.Comp != u.HLSL.Comp {
		t.Fatalf("HLSL = %+v, %v; want what was uploaded", h, err)
	}
	// And it survives a restart: the index is rebuilt from the files.
	again := NewStore(s.Root())
	if h, _ := again.HLSL(sm.ID); h == nil {
		t.Fatal("the HLSL did not survive a fresh store")
	}
}

// before is the upload a v0.9.92 server stored for twoShaders: shaders
// dropped, nothing kept.
func before(t *testing.T) Upload {
	u := parsed(t, twoShaders)
	u.HLSL = nil
	return u
}

func TestAConvertedReuploadReplacesTheFileStoredWithoutShaders(t *testing.T) {
	s := NewStore(t.TempDir())
	old := before(t)
	old.Name = "Kept Name"
	first, _, _, err := s.Put(old)
	if err != nil {
		t.Fatal(err)
	}
	u := convertedUpload(t)
	u.Name = "renamed on disk.milk"
	sm, created, replaced, err := s.Put(u)
	if err != nil {
		t.Fatal(err)
	}
	if !created || replaced != first.ID || sm.ID == first.ID {
		t.Fatalf("created %v, replaced %q (want %q), id %q", created, replaced, first.ID, sm.ID)
	}
	if sm.Name != "Kept Name" || sm.Shaders != ShadersConverted {
		t.Fatalf("summary %+v, want the old name and converted shaders", sm)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].ID != sm.ID {
		t.Fatalf("list %+v, want only the converted preset", list)
	}
	if _, err := s.Get(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the old file is still there: %v", err)
	}
}

func TestAReuploadThatStillFailsGainsItsReasonAndHLSL(t *testing.T) {
	s := NewStore(t.TempDir())
	first, _, _, err := s.Put(before(t))
	if err != nil {
		t.Fatal(err)
	}
	u := parsed(t, twoShaders)
	if err := ConvertShaders(context.Background(), &fakeConv{answer: func(int, string) (string, error) {
		return "", &msc.Failure{Reason: "glsl-optimizer: no"}
	}}, &u); err != nil {
		t.Fatal(err)
	}
	sm, created, replaced, err := s.Put(u)
	if err != nil {
		t.Fatal(err)
	}
	if created || replaced != "" || sm.ID != first.ID || sm.ShadersReason != "warp: glsl-optimizer: no" || sm.UploadedAt != first.UploadedAt {
		t.Fatalf("created %v replaced %q summary %+v, want the same preset with its reason", created, replaced, sm)
	}
	if h, _ := s.HLSL(sm.ID); h == nil {
		t.Fatal("the HLSL was not kept")
	}
	if l, _ := NewStore(s.Root()).List(); len(l) != 1 || l[0].ShadersReason == "" {
		t.Fatalf("after a restart: %+v", l)
	}
}

func TestAConvertedUploadDoesNotReplaceAJSONPreset(t *testing.T) {
	s := NewStore(t.TempDir())
	u := convertedUpload(t)
	// A JSON upload with no shaders and the same equations has the id the
	// .milk had without its shaders. It is somebody's own preset, not an old
	// copy of this one.
	js := Upload{Name: "mine", Source: SourceJSON, Shaders: ShadersNone, Preset: before(t).Preset}
	jsm, _, _, err := s.Put(js)
	if err != nil {
		t.Fatal(err)
	}
	if jsm.ID != u.Replaces {
		t.Fatalf("test is wrong: ids %q and %q", jsm.ID, u.Replaces)
	}
	if _, _, replaced, err := s.Put(u); err != nil || replaced != "" {
		t.Fatalf("replaced %q, %v; a JSON preset is never replaced", replaced, err)
	}
	if l, _ := s.List(); len(l) != 2 {
		t.Fatalf("%d presets, want both", len(l))
	}
}
