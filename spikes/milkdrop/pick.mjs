// SPIKE — choose the named dozen and a seeded random browser sample.
import fs from "node:fs"; import path from "node:path"; import { readMilk } from "./milk.mjs";
function walk(d, o = []) { for (const e of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, e.name); e.isDirectory() ? walk(p, o) : e.name.endsWith(".milk") && o.push(p); } return o; }
let seed = 12345; const rnd = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296);
const shuffle = (a) => { a = [...a]; for (let i = a.length - 1; i > 0; i--) { const j = Math.floor(rnd() * (i + 1)); [a[i], a[j]] = [a[j], a[i]]; } return a; };
const info = (f) => { const t = fs.readFileSync(f, "latin1"); const p = readMilk(t); return { f, p, eel: p.init + p.frame + p.pixel + p.waves.map((w) => w.init + w.frame + w.point).join("") + p.shapes.map((s) => s.init + s.frame).join("") }; };
const orig = shuffle(walk("../presets/presets-milkdrop-original")).map(info);
const cream = shuffle(walk("../presets/presets-cream-of-the-crop")).slice(0, 1500).map(info);
const classic = shuffle(walk("../presets/presets-projectm-classic")).map(info);
const taken = new Set(); const pick = (list, pred, n) => { const r = list.filter((x) => !taken.has(x.f) && pred(x)).slice(0, n); r.forEach((x) => taken.add(x.f)); return r; };
const has = (x, re) => re.test(x.eel);
const dozen = [
  ...pick(orig, (x) => x.p.version < 200, 3),
  ...pick(orig, (x) => x.p.version >= 200 && x.p.warp.trim() && x.p.comp.trim(), 3),
  ...pick(cream, (x) => x.p.warp.trim() && has(x, /\bloop\s*\(/i) && has(x, /megabuf/i), 1),
  ...pick(cream, (x) => x.p.warp.trim() && has(x, /\bwhile\s*\(/i), 1),
  ...pick(cream, (x) => /tex3D/i.test(x.p.warp + x.p.comp), 1),
  ...pick(cream, (x) => /sampler_(fw_)?worms/i.test(x.p.warp + x.p.comp), 1),
  ...pick(cream, (x) => x.p.shapes.some((s) => s.enabled && s.instances > 50), 1),
  ...pick(classic, (x) => x.p.version < 200 && x.p.waves.some((w) => w.enabled && w.point.trim()), 1),
];
const sample = [...pick(orig, () => true, 50), ...pick(cream, () => true, 75), ...pick(classic, () => true, 25)];
fs.writeFileSync("dozen.txt", dozen.map((x) => x.f).join("\n") + "\n");
fs.writeFileSync("sample.txt", sample.map((x) => x.f).join("\n") + "\n");
for (const x of dozen) console.log(x.p.version, x.p.warp.trim() ? "W" : "-", x.p.comp.trim() ? "C" : "-", path.basename(x.f));
console.log("sample", sample.length);
