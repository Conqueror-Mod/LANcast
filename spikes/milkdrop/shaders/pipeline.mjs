// SPIKE (ADR 0091 step g). The string steps around the native converter.
//
// butterchurn's preset packs were made by milkdrop-preset-converter-node:
//   splitPreset (pull warp_N= / comp_N= lines out of the .milk)
//   prepareShader (wrap the body in MilkDrop's header of samplers, uniforms
//                  and GetBlur macros)
//   processShader (shader_body -> xlat_main)
//   convertHLSLString (native: hlsl2glslfork + glsl-optimizer)  <- the WASI part
//   processOptimizedShader (strip the GLSL back to a `shader_body { }` that
//                           butterchurn wraps again at runtime)
//
// Everything except the native step is plain string work. These are
// transcriptions of milkdrop-preset-utils @ ba5a4e4 (MIT, Jordan Berg) and
// milkdrop-preset-converter-node @ b9de8b9 (MIT), with lodash calls replaced
// by their native equivalents and nothing else changed, so the output can be
// compared byte for byte with the shipped packs.

const isNonBaseVal = (line) =>
  line.startsWith("per_frame_init") || line.startsWith("per_frame") ||
  line.startsWith("per_frame_pixel") || line.startsWith("wavecode") ||
  line.startsWith("shapecode") || line.startsWith("warp_1") || line.startsWith("comp_1");

function getLinesWithPrefix(lines, prefix) {
  const regex = new RegExp(`${prefix}_\\d+=\`*`);
  return lines.filter((l) => regex.test(l)).map((l) => l.split(regex).at(-1)).join("\n");
}

// splitPreset, the shader half of it.
export function splitShaders(text) {
  const version = text.includes("MILKDROP_PRESET_VERSION=201") ? 2 : 1;
  let main = text.split("[preset00]")[1];
  if (main === undefined) return { version, warp: "", comp: "" };
  main = main.replace(/\r\n/g, "\n");
  const lines = main.split("\n");
  const i = lines.findIndex(isNonBaseVal);
  const rest = i < 0 ? [] : lines.slice(i);
  return { version, warp: getLinesWithPrefix(rest, "warp"), comp: getLinesWithPrefix(rest, "comp") };
}

export function getShaderParts(t) {
  const sbIndex = t.indexOf("shader_body");
  if (t && sbIndex > -1) {
    const before = t.substring(0, sbIndex);
    const after = t.substring(sbIndex);
    const firstCurly = after.indexOf("{");
    const lastCurly = after.lastIndexOf("}");
    return [before, after.substring(firstCurly + 1, lastCurly)];
  }
  return ["", t];
}

const HEADER = `#define  M_PI   3.14159265359
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

${Array.from({ length: 32 }, (_, i) => `   float q${i + 1};`).join("\n")}

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
`;

// prepareShader + processShader: the HLSL handed to the native converter.
export function prepareShader(shader) {
  if (shader.length === 0) return "";
  let s = shader.replace("sampler sampler_pw_noise_lq;\n", "");
  s = s.replace("sampler2D sampler_pw_noise_lq;\n", "");
  s = s.replace("sampler sampler_pw_noise_hq;\n", "");
  s = s.replace("sampler2D sampler_pw_noise_hq;\n", "");
  const parts = getShaderParts(s);
  const full = `${HEADER}
   ${parts[0].replace("sampler sampler_", "sampler2D sampler_")}

   float4 shader_body (float2 uv : TEXCOORD0) : COLOR0
   {
       float3 ret;

       ${parts[1]}

       return float4(ret, 1.0);
   }`;
  return full.replace("shader_body", "xlat_main");
}

const BUILTIN_SAMPLERS = new Set([
  "sampler_main", "sampler_fw_main", "sampler_pw_main", "sampler_fc_main", "sampler_pc_main",
  "sampler_noise_lq", "sampler_noise_lq_lite", "sampler_noise_mq", "sampler_noise_hq",
  "sampler_pw_noise_lq", "sampler_noisevol_lq", "sampler_noisevol_hq",
  "sampler_blur1", "sampler_blur2", "sampler_blur3",
]);

function isUserSampler(line) {
  if (!line.startsWith("uniform sampler")) return false;
  const m = line.match(/uniform sampler2D sampler_(.+);$/);
  if (m && m.length > 1) return !BUILTIN_SAMPLERS.has(`sampler_${m[1]}`);
  return false;
}

export function processOptimizedShader(shader) {
  if (!shader) return "";
  let s = shader.replace("#version 300 es\n", "");
  s = s.replace("void main ()", "shader_body");
  s = s.replace(/highp\s*/g, "");
  s = s.replace(/medp\s*/g, "");
  s = s.replace(/lowp\s*/g, "");
  s = s.replace(/xlv_TEXCOORD0/g, "uv");
  s = s.replace(/_glesFragData\[0\] = (.+);\n\}/, (m, v) => `ret = ${v}.xyz;\n}`);
  s = s.replace("out vec4 _glesFragData[4];\n", "");
  const parts = getShaderParts(s);
  const header = parts[0].split("\n")
    .filter((l) => !(l.startsWith("in") || (l.startsWith("uniform") && !isUserSampler(l))))
    .join("\n");
  let body = parts[1].split(";");
  let k = 0;
  while (k < body.length) {
    const t = body[k].trim();
    if (!t.startsWith("xlat_mutable")) break;
    const m = t.match(/xlat_mutable(.+)\s*=\s*(.+)/);
    if (!(m && m.length === 3 && m[1].trim() === m[2].trim())) break;
    k++;
  }
  body = body.slice(k);
  return `${header} shader_body { ${body.join(";")} }`;
}
