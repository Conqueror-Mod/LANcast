// SPIKE — does eel-wasm 0.0.17 compile every EEL block of every preset? (compile + WebAssembly.compile, no run)
//   node wasm-bulk.mjs <pkgs> <dir>...
import fs from "node:fs"; import path from "node:path"; import { readMilk } from "./milk.mjs";
const pkgs = process.argv[2];
import { pathToFileURL } from "node:url";
const eelwasm = await import(pathToFileURL(path.resolve(pkgs, "eel-wasm-0.0.17/package/dist/index.js")).href);
function walk(d, o = []) { for (const e of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, e.name); e.isDirectory() ? walk(p, o) : e.name.endsWith(".milk") && o.push(p); } return o; }
let n = 0, ok = 0; const cats = {}; const ex = {};
for (const dir of process.argv.slice(3)) for (const f of walk(dir)) {
  n++;
  const p = readMilk(fs.readFileSync(f, "latin1"));
  const functions = { init: { pool: "a", code: p.init }, frame: { pool: "a", code: p.frame }, pixel: { pool: "a", code: p.pixel } };
  p.waves.forEach((w, i) => { if (w.enabled) { functions[`w${i}i`] = { pool: "a", code: w.init }; functions[`w${i}f`] = { pool: "a", code: w.frame }; functions[`w${i}p`] = { pool: "a", code: w.point }; } });
  p.shapes.forEach((s, i) => { if (s.enabled) { functions[`s${i}i`] = { pool: "a", code: s.init }; functions[`s${i}f`] = { pool: "a", code: s.frame }; } });
  try { await WebAssembly.compile(eelwasm.compileModule({ pools: { a: new Set(["time"]) }, functions, eelVersion: 2 })); ok++; }
  catch (e) {
    const m = String(e.message).split("\n")[0];
    const k = m.replace(/`[^`]*`|"[^"]*"|'[^']*'/g, "X").replace(/\d+/g, "N").slice(0, 70);
    cats[k] = (cats[k] || 0) + 1; ex[k] ??= `${path.basename(f)}: ${m.slice(0, 110)}`;
  }
}
console.log(JSON.stringify({ presets: n, compiled: ok, failed: n - ok, categories: Object.entries(cats).sort((a, b) => b[1] - a[1]).map(([k, v]) => `${v}x ${k}  e.g. ${ex[k]}`) }, null, 1));
