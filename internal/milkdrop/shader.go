package milkdrop

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"lancast/internal/milkdrop/msc"
)

/*
 * A .milk's shaders, from HLSL to the GLSL butterchurn loads (ADR 0091, the
 * amendment of 2026-10-19, steps g2 and g3).
 *
 * butterchurn's own preset packs were made by milkdrop-preset-converter-node,
 * and this is its chain for one shader:
 *
 *   splitShaders            take the warp_N= / comp_N= lines out of the .milk
 *   prepareShader           wrap the body in MilkDrop's header of samplers,
 *                           q1–q32 and the GetBlur macros; shader_body becomes
 *                           xlat_main
 *   the converter           hlsl2glslfork + glsl-optimizer: internal/milkdrop/msc
 *   processOptimizedShader  trim the GLSL back to the `shader_body { }` that
 *                           butterchurn wraps again when it loads a preset
 *
 * Only the converter is native code, and it runs in wazero's sandbox. The
 * three string steps are ported here from milkdrop-preset-utils @ ba5a4e4
 * (MIT, Jordan Berg; LICENSE.milkdrop-preset-utils in msc/) by way of the
 * spike's transcription, spikes/milkdrop/shaders/pipeline.mjs, which was
 * checked byte for byte against the shipped packs. They are ported *exactly*,
 * oddities included — a filter that drops every header line starting "in",
 * `int` declarations with it; replacements that take only the first match —
 * because the point is to make what butterchurn's packs were made with, and a
 * port that improved on them would be a third converter nobody has measured.
 * The tests hold the port to pipeline.mjs's own output on LANcast's fixtures,
 * and the corpus test (outside the repository) holds it to the spike's run
 * over 9,141 shaders.
 *
 * WHY THIS IS NOT IN Parse
 *
 * "Process execution stays split from parsing." Parse is pure and fast: it
 * decides what a preset says. Converting is a separate step the upload
 * handler calls, through ShaderConverter, after Parse has accepted the
 * preset — so a handler test uses a fake, and Parse's tests never wait for a
 * module to compile. A wazero call is not the external process that rule was
 * written about (it is a pure function of its input bytes, the same in a test,
 * a shell and session 0, and touches no GPU, PATH or file), but the split
 * costs nothing and keeps the fast tests fast.
 */

// ShaderConverter turns one prepared HLSL shader into the GLSL the converter
// printed. A shader it will not convert is a *msc.Failure; any other error is
// the server's, not the shader's.
type ShaderConverter interface {
	Convert(ctx context.Context, hlsl string) (string, error)
}

// ShaderSource is a .milk's shaders as the file had them, joined across their
// numbered lines. It is kept beside the stored preset so that a better
// converter can convert it again without a re-upload, and it is never served:
// butterchurn cannot use HLSL, and nothing else should come to read it.
type ShaderSource struct {
	Warp string `json:"warp,omitempty"`
	Comp string `json:"comp,omitempty"`
}

/*
 * ConvertShaders converts an accepted upload's shaders in place.
 *
 * When every shader converts, the preset carries them, Shaders is converted,
 * and Replaces names the preset this one would have been without them — the
 * one an upload before this step stored — so the store can put this in its
 * place rather than beside it. Otherwise the preset is left exactly as step 1
 * stores it, butterchurn's default shaders, with Shaders not_converted and the
 * converter's reason. A preset is never refused for its shaders: nothing is
 * refused that step 1 accepts.
 *
 * The error is not about the shader. It is non-nil when ctx ended, and then
 * nothing should be stored; or when the converter itself is broken, and then
 * the upload is still complete — marked not_converted, saying so — and the
 * caller should store it and log the error.
 */
func ConvertShaders(ctx context.Context, conv ShaderConverter, u *Upload) error {
	if u.Source != SourceMilk || u.HLSL == nil {
		return nil
	}
	notConverted := func(reason string) {
		u.Shaders, u.ShadersReason = ShadersNotConverted, reason
	}
	type job struct {
		name, src string
		dst       *string
	}
	var warp, comp string
	var jobs []job
	if u.HLSL.Warp != "" {
		jobs = append(jobs, job{"warp", u.HLSL.Warp, &warp})
	}
	if u.HLSL.Comp != "" {
		jobs = append(jobs, job{"comp", u.HLSL.Comp, &comp})
	}
	if len(jobs) == 0 {
		// readMilk saw a shader line splitPreset does not: one before the
		// first equation, say. Rare, and not worth a second reading of the
		// file that disagrees with the one butterchurn's packs were made by.
		notConverted("the shader text could not be found in the file")
		return nil
	}
	for _, j := range jobs {
		glsl, err := conv.Convert(ctx, prepareShader(j.src))
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var f *msc.Failure
			if errors.As(err, &f) {
				notConverted(j.name + ": " + f.Reason)
				return nil
			}
			notConverted("the shader converter is not working on this server")
			return err
		}
		out := processOptimizedShader(glsl)
		if len(out) > MaxShaderBytes {
			notConverted(fmt.Sprintf("%s: converted, it is over %d KB", j.name, MaxShaderBytes>>10))
			return nil
		}
		*j.dst = out
	}
	without, err := ID(u.Preset)
	if err != nil {
		return err
	}
	u.Preset.Warp, u.Preset.Comp = warp, comp
	u.Shaders, u.ShadersReason, u.Replaces = ShadersConverted, "", without
	return nil
}

// ---- splitPreset, the shader half of it -------------------------------------------

// isNonBaseVal is where splitPreset decides the base values end: the first
// line that is an equation, a shape or wave, or the first shader line.
func isNonBaseVal(line string) bool {
	for _, p := range []string{"per_frame_init", "per_frame", "per_frame_pixel", "wavecode", "shapecode", "warp_1", "comp_1"} {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

var (
	warpLine = regexp.MustCompile("warp_\\d+=`*")
	compLine = regexp.MustCompile("comp_\\d+=`*")
)

// linesWithPrefix keeps the lines the pattern matches anywhere in, and of each
// the text after its last match. (JavaScript's `l.split(regex).at(-1)`.)
func linesWithPrefix(lines []string, re *regexp.Regexp) string {
	var out []string
	for _, l := range lines {
		locs := re.FindAllStringIndex(l, -1)
		if locs == nil {
			continue
		}
		out = append(out, l[locs[len(locs)-1][1]:])
	}
	return strings.Join(out, "\n")
}

// splitShaders is splitPreset's shader half: the warp and comp text of a
// .milk, each line's backtick prefix removed and the lines joined by "\n".
// The section marker is matched as written, case and all, as splitPreset
// matches it.
func splitShaders(text string) ShaderSource {
	parts := strings.Split(text, "[preset00]")
	if len(parts) < 2 {
		return ShaderSource{}
	}
	main := strings.ReplaceAll(parts[1], "\r\n", "\n")
	lines := strings.Split(main, "\n")
	i := -1
	for k, l := range lines {
		if isNonBaseVal(l) {
			i = k
			break
		}
	}
	if i < 0 {
		return ShaderSource{}
	}
	rest := lines[i:]
	return ShaderSource{Warp: linesWithPrefix(rest, warpLine), Comp: linesWithPrefix(rest, compLine)}
}

// ---- prepareShader ---------------------------------------------------------------

// jsSubstring is JavaScript's String.prototype.substring: arguments clamped to
// the string, and swapped when the first is larger. getShaderParts relies on
// both when a shader has no braces.
func jsSubstring(s string, a, b int) string {
	clamp := func(v int) int { return max(0, min(v, len(s))) }
	a, b = clamp(a), clamp(b)
	if a > b {
		a, b = b, a
	}
	return s[a:b]
}

// shaderParts splits a shader at its shader_body into the declarations before
// it and the body inside its outermost braces.
func shaderParts(t string) (before, body string) {
	sb := strings.Index(t, "shader_body")
	if t == "" || sb < 0 {
		return "", t
	}
	after := t[sb:]
	return t[:sb], jsSubstring(after, strings.Index(after, "{")+1, strings.LastIndex(after, "}"))
}

// shaderHeader is milkdrop-preset-utils' header, byte for byte: its
// indentation is the template literal's. The line numbers in the converter's
// errors count from here.
var shaderHeader = func() string {
	var q strings.Builder
	for i := 1; i <= 32; i++ {
		if i > 1 {
			q.WriteByte('\n')
		}
		fmt.Fprintf(&q, "   float q%d;", i)
	}
	return `#define  M_PI   3.14159265359
   #define  M_PI_2 6.28318530718
   #define  M_INV_PI_2  0.159154943091895

   uniform sampler2D sampler_main;
   uniform sampler2D sampler_fw_main;
   uniform sampler2D sampler_pw_main;
   uniform sampler2D sampler_fc_main;
   uniform sampler2D sampler_pc_main;

   uniform sampler2D sampler_noise_lq;
   uniform sampler2D sampler_noise_lq_lite;
   uniform sampler2D sampler_noise_mq;
   uniform sampler2D sampler_noise_hq;
   uniform sampler3D sampler_noisevol_lq;
   uniform sampler3D sampler_noisevol_hq;

   uniform sampler2D sampler_pw_noise_lq;

   uniform sampler2D sampler_blur1;
   uniform sampler2D sampler_blur2;
   uniform sampler2D sampler_blur3;

   float4 texsize_noise_lq;
   float4 texsize_noise_mq;
   float4 texsize_noise_hq;
   float4 texsize_noise_lq_lite;
   float4 texsize_noisevol_lq;
   float4 texsize_noisevol_hq;

   float4 _qa;
   float4 _qb;
   float4 _qc;
   float4 _qd;
   float4 _qe;
   float4 _qf;
   float4 _qg;
   float4 _qh;

` + q.String() + `

   float blur1_min;
   float blur1_max;
   float blur2_min;
   float blur2_max;
   float blur3_min;
   float blur3_max;

   float scale1;
   float scale2;
   float scale3;
   float bias1;
   float bias2;
   float bias3;

   float4 slow_roam_cos;
   float4 roam_cos;
   float4 slow_roam_sin;
   float4 roam_sin;

   float3 hue_shader;

   float time;
   float4 rand_preset;
   float4 rand_frame;
   float  progress;
   float  frame;
   float  fps;
   float  decay;
   float  bass;
   float  mid;
   float  treb;
   float  vol;
   float  bass_att;
   float  mid_att;
   float  treb_att;
   float  vol_att;
   float4 texsize;
   float4 aspect;

   float rad;
   float ang;
   float2 uv_orig;

   #define GetMain(uv) (tex2D(sampler_main,uv).xyz)
   #define GetPixel(uv) (tex2D(sampler_main,uv).xyz)
   #define GetBlur1(uv) (tex2D(sampler_blur1,uv).xyz*scale1 + bias1)
   #define GetBlur2(uv) (tex2D(sampler_blur2,uv).xyz*scale2 + bias2)
   #define GetBlur3(uv) (tex2D(sampler_blur3,uv).xyz*scale3 + bias3)

   #define lum(x) (dot(x,float3(0.32,0.49,0.29)))
   #define tex2d tex2D
   #define tex3d tex3D
`
}()

// prepareShader is prepareShader + processShader: the HLSL the converter is
// given for one shader's text.
func prepareShader(shader string) string {
	if shader == "" {
		return ""
	}
	s := shader
	for _, drop := range []string{"sampler sampler_pw_noise_lq;\n", "sampler2D sampler_pw_noise_lq;\n",
		"sampler sampler_pw_noise_hq;\n", "sampler2D sampler_pw_noise_hq;\n"} {
		s = strings.Replace(s, drop, "", 1)
	}
	before, body := shaderParts(s)
	full := shaderHeader + "\n   " + strings.Replace(before, "sampler sampler_", "sampler2D sampler_", 1) + `

   float4 shader_body (float2 uv : TEXCOORD0) : COLOR0
   {
       float3 ret;

       ` + body + `

       return float4(ret, 1.0);
   }`
	return strings.Replace(full, "shader_body", "xlat_main", 1)
}

// ---- processOptimizedShader -------------------------------------------------------

var builtinSamplers = map[string]bool{
	"sampler_main": true, "sampler_fw_main": true, "sampler_pw_main": true, "sampler_fc_main": true,
	"sampler_pc_main": true, "sampler_noise_lq": true, "sampler_noise_lq_lite": true,
	"sampler_noise_mq": true, "sampler_noise_hq": true, "sampler_pw_noise_lq": true,
	"sampler_noisevol_lq": true, "sampler_noisevol_hq": true,
	"sampler_blur1": true, "sampler_blur2": true, "sampler_blur3": true,
}

var (
	userSampler  = regexp.MustCompile(`uniform sampler2D sampler_(.+);$`)
	precisionRes = []*regexp.Regexp{regexp.MustCompile(`highp\s*`), regexp.MustCompile(`medp\s*`), regexp.MustCompile(`lowp\s*`)}
	fragData     = regexp.MustCompile(`_glesFragData\[0\] = (.+);\n\}`)
	identityMut  = regexp.MustCompile(`xlat_mutable(.+)\s*=\s*(.+)`)
)

// isUserSampler: a 2D sampler the preset declared for an image of its own,
// which is the one kind of uniform declaration butterchurn needs kept.
func isUserSampler(line string) bool {
	if !strings.HasPrefix(line, "uniform sampler") {
		return false
	}
	m := userSampler.FindStringSubmatch(line)
	return m != nil && !builtinSamplers["sampler_"+m[1]]
}

// processOptimizedShader trims the converter's GLSL back to declarations and a
// `shader_body { … }`, which butterchurn wraps in its own header and main
// when it loads the preset.
func processOptimizedShader(shader string) string {
	if shader == "" {
		return ""
	}
	s := strings.Replace(shader, "#version 300 es\n", "", 1)
	s = strings.Replace(s, "void main ()", "shader_body", 1)
	for _, re := range precisionRes {
		s = re.ReplaceAllString(s, "")
	}
	s = strings.ReplaceAll(s, "xlv_TEXCOORD0", "uv")
	if loc := fragData.FindStringSubmatchIndex(s); loc != nil {
		s = s[:loc[0]] + "ret = " + s[loc[2]:loc[3]] + ".xyz;\n}" + s[loc[1]:]
	}
	s = strings.Replace(s, "out vec4 _glesFragData[4];\n", "", 1)
	before, body := shaderParts(s)
	var keep []string
	for _, l := range strings.Split(before, "\n") {
		if strings.HasPrefix(l, "in") || (strings.HasPrefix(l, "uniform") && !isUserSampler(l)) {
			continue
		}
		keep = append(keep, l)
	}
	stmts := strings.Split(body, ";")
	k := 0
	for k < len(stmts) {
		t := strings.TrimSpace(stmts[k])
		if !strings.HasPrefix(t, "xlat_mutable") {
			break
		}
		m := identityMut.FindStringSubmatch(t)
		if m == nil || strings.TrimSpace(m[1]) != strings.TrimSpace(m[2]) {
			break
		}
		k++
	}
	return strings.Join(keep, "\n") + " shader_body { " + strings.Join(stmts[k:], ";") + " }"
}
