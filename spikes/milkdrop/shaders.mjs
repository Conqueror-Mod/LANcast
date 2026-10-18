// SPIKE — throwaway. What do the HLSL warp/comp shaders in a preset pack need?
//   node shaders.mjs <dir>...
import fs from "node:fs";
import path from "node:path";
import { readMilk } from "./milk.mjs";

const BUILTIN = new Set(["main", "fw_main", "fc_main", "pw_main", "pc_main", "blur1", "blur2", "blur3",
  "noise_lq", "noise_lq_lite", "noise_mq", "noise_hq", "pw_noise_lq", "noisevol_lq", "noisevol_hq"]);

function walk(d, out = []) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (e.name.toLowerCase().endsWith(".milk")) out.push(p);
  }
  return out;
}

const tally = { presets: 0, withShader: 0, randTexture: 0, namedTexture: 0, loops: 0, tex3D: 0 };
const named = {};
for (const dir of process.argv.slice(2)) for (const f of walk(dir)) {
  tally.presets++;
  const p = readMilk(fs.readFileSync(f, "latin1"));
  const src = p.warp + "\n" + p.comp;
  if (!src.trim()) continue;
  tally.withShader++;
  const samplers = new Set([...src.matchAll(/sampler\s+sampler_(\w+)/gi)].map((m) => m[1].toLowerCase()));
  const user = [...samplers].filter((s) => !BUILTIN.has(s));
  if (user.some((s) => /^(\w+_)?rand\d\d/.test(s))) tally.randTexture++;
  const nm = user.filter((s) => !/^(\w+_)?rand\d\d/.test(s));
  if (nm.length) { tally.namedTexture++; for (const s of nm) named[s] = (named[s] || 0) + 1; }
  if (/\b(for|while)\s*\(/.test(src)) tally.loops++;
  if (/tex3D/i.test(src)) tally.tex3D++;
}
console.log(JSON.stringify({ ...tally, topNamedTextures: Object.entries(named).sort((a, b) => b[1] - a[1]).slice(0, 12) }, null, 1));
