// SPIKE (ADR 0091 step g). results.jsonl -> converted shaders + a summary.
//
//   node post.mjs <results.jsonl> <jobs.jsonl.meta.json> <converted.json> <summary.json>
//
// converted.json: { "<preset id>": { warp, comp } } for every preset whose
// every shader converted, after processOptimizedShader (what butterchurn's
// packs hold). summary.json: rates per pack, failure reasons, and what the
// converted shaders ask of the textures butterchurn binds.
import fs from "node:fs";
import { processOptimizedShader } from "./pipeline.mjs";

const [resultsPath, metaPath, convPath, sumPath] = process.argv.slice(2);
const meta = JSON.parse(fs.readFileSync(metaPath, "utf8"));
const byPreset = {};
for (const line of fs.readFileSync(resultsPath, "utf8").split("\n")) {
  if (!line) continue;
  const r = JSON.parse(line);
  const cut = r.id.lastIndexOf("#"), id = r.id.slice(0, cut), kind = r.id.slice(cut + 1);
  (byPreset[id] ??= {})[kind] = r;
}

// The textures every MilkDrop 2 shader may name without the preset bringing
// them: butterchurn binds all of these itself.
const BUILTIN = new Set(["main", "fw_main", "pw_main", "fc_main", "pc_main", "noise_lq", "noise_lq_lite",
  "noise_mq", "noise_hq", "pw_noise_lq", "noisevol_lq", "noisevol_hq", "blur1", "blur2", "blur3"]);

const reason = (r) => {
  const first = (r.log || "").split("\n").find((l) => /error/i.test(l)) || r.log || "";
  return first.replace(/^.*?error:?\s*/i, "").replace(/\(\d+,\d+\)/g, "").replace(/`[^']*'/g, "`…'")
    .replace(/'[^']*'/g, "'…'").replace(/\d+/g, "N").trim().slice(0, 90);
};

const conv = {};
const s = {
  packs: {}, reasons: {}, rcs: {},
  textures: { presetsNamingUserSampler: 0, presetsNaming2PlusUserSamplers: 0, presetsUsingTexsizeOfUserSampler: 0,
    presetsUsingRandSampler: 0, samplerNames: {}, prefixes: {} },
  glslBytes: [],
};
for (const [id, m] of Object.entries(meta)) {
  const pack = id.split("/")[0];
  const p = (s.packs[pack] ??= { presets: 0, md2: 0, withShaders: 0, allConverted: 0, someFailed: 0 });
  p.presets++;
  if (m.version === 2) p.md2++;
  if (!m.warp && !m.comp) continue;
  p.withShaders++;
  const rs = byPreset[id] || {};
  const kinds = ["warp", "comp"].filter((k) => m[k]);
  for (const k of kinds) s.rcs[rs[k].rc] = (s.rcs[rs[k].rc] ?? 0) + 1;
  if (kinds.some((k) => rs[k].rc !== 0)) {
    p.someFailed++;
    for (const k of kinds) if (rs[k].rc !== 0) { const why = `rc${rs[k].rc}: ${reason(rs[k])}`; s.reasons[why] = (s.reasons[why] ?? 0) + 1; }
    continue;
  }
  p.allConverted++;
  const out = {};
  for (const k of ["warp", "comp"]) out[k] = m[k] ? processOptimizedShader(rs[k].glsl) : "";
  conv[id] = out;
  const both = out.warp + "\n" + out.comp;
  s.glslBytes.push(out.warp.length + out.comp.length);
  const users = new Set([...both.matchAll(/uniform sampler2D sampler_(.+?);/g)].map((x) => x[1]).filter((n) => !BUILTIN.has(n)));
  if (users.size) s.textures.presetsNamingUserSampler++;
  if (users.size >= 2) s.textures.presetsNaming2PlusUserSamplers++;
  if ([...users].some((n) => both.includes(`texsize_${n}`))) s.textures.presetsUsingTexsizeOfUserSampler++;
  if ([...users].some((n) => /^rand\d\d/.test(n))) s.textures.presetsUsingRandSampler++;
  for (const n of users) {
    s.textures.samplerNames[n] = (s.textures.samplerNames[n] ?? 0) + 1;
    const pre = n.match(/^(fw_|fc_|pw_|pc_)/)?.[1] ?? "(none)";
    s.textures.prefixes[pre] = (s.textures.prefixes[pre] ?? 0) + 1;
  }
}
s.glslBytes.sort((a, b) => a - b);
const q = (p) => s.glslBytes[Math.floor((s.glslBytes.length - 1) * p)];
s.glslBytes = { p50: q(0.5), p99: q(0.99), max: s.glslBytes.at(-1) };
s.reasons = Object.fromEntries(Object.entries(s.reasons).sort((a, b) => b[1] - a[1]).slice(0, 25));
const names = Object.entries(s.textures.samplerNames).sort((a, b) => b[1] - a[1]);
s.textures.distinctSamplerNames = names.length;
s.textures.samplerNames = Object.fromEntries(names.slice(0, 30));
fs.writeFileSync(convPath, JSON.stringify(conv));
fs.writeFileSync(sumPath, JSON.stringify(s, null, 1));
console.log(JSON.stringify(s, null, 1));
