// SPIKE — throwaway. Three engines, one preset, identical inputs:
//
//   ref     butterchurn 2's converter -> JS strings -> new Function, in this
//           realm with butterchurn's helpers on globalThis (what LANcast must
//           NOT ship; here only as the yardstick for behaviour and speed)
//   interp  eel.mjs: the original EEL -> AST -> closures, no code generation
//   wasm    eel-wasm 0.0.17 (what butterchurn 3 uses): the original EEL ->
//           a WebAssembly module whose only imports are variable globals and
//           maths shims
//
// Frame variables follow butterchurn 2's PresetEquationRunner exactly: each
// frame starts from base values + q-after-init + last frame's user variables
// + this frame's globals (time, bass, …). Compares the per-frame variables of
// all three after every frame, then times frame + per-vertex (48x36 mesh).
//
//   node diff3.mjs <pkgs> <out.json> <file.milk>...

import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { readMilk } from "./milk.mjs";
import { equations, tokenize, FUNCS } from "./eel.mjs";

const [pkgs, outFile, ...files] = process.argv.slice(2);
globalThis.window = globalThis; globalThis.self = globalThis;
globalThis.document = { currentScript: { src: "" }, createTextNode: () => ({}), title: "" };
globalThis.location = { href: "" };
const require = createRequire(import.meta.url);
const conv = require(path.join(pkgs, "milkdrop-preset-converter-0.1.2/package/dist/milkdrop-preset-converter.min.js"));
const eelwasm = await import("file://" + path.resolve(pkgs, "eel-wasm-0.0.17/package/dist/index.js").replace(/\\/g, "/"));

const bc = fs.readFileSync(path.join(pkgs, "butterchurn-2.6.7/package/lib/butterchurn.js"), "utf8");
const hStart = bc.indexOf("var EPSILON = 0.00001;");
new Function(bc.slice(hStart, bc.indexOf("/* eslint-enable */", hStart)))(); // helpers onto globalThis, as butterchurn does onto window
const dStart = bc.indexOf("this.baseValsDefaults = {");
const DEFAULTS = new Function("return " + bc.slice(bc.indexOf("{", dStart), bc.indexOf("};", dStart) + 1))();
const ref = (src) => (src && src.trim() ? new Function("a", `${src} return a;`) : (a) => a);

function mulberry32(seed) {
  return () => {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const FRAMES = 120, MW = 48, MH = 36;
const QS = Array.from({ length: 32 }, (_, i) => "q" + (i + 1));
const GLOBALS = ["time", "frame", "fps", "progress", "bass", "mid", "treb", "bass_att", "mid_att", "treb_att"];
function globalsAt(f) {
  const t = f / 60;
  const beat = Math.max(0, Math.sin(t * Math.PI * 4)) ** 4; // a 120 bpm kick
  return {
    time: t, frame: f, fps: 60, progress: (f % 600) / 600,
    bass: 0.4 + 1.6 * beat, mid: 0.8 + 0.4 * Math.sin(t * 3), treb: 0.7 + 0.5 * Math.sin(t * 7.3),
    bass_att: 0.6 + beat, mid_att: 0.9 + 0.2 * Math.sin(t * 1.3), treb_att: 0.8 + 0.2 * Math.sin(t * 2.1),
  };
}
function baseState(baseVals) {
  return { ...baseVals, meshx: MW, meshy: MH, pixelsx: 1280, pixelsy: 720, aspectx: 0.5625, aspecty: 1, ...globalsAt(0) };
}

// --- the JS-object engines (ref and interp) share one driver -----------------
function driveObj(eqs, baseVals, trace) {
  Math.random = mulberry32(7);
  const mdVS = baseState(baseVals);
  mdVS.megabuf = new Array(1048576).fill(0);
  mdVS.gmegabuf = new Array(1048576).fill(0);
  const nonUser = new Set([...QS, ...Object.keys(mdVS)]);
  const afterInit = eqs.init({ ...mdVS });
  const qInit = pick(afterInit, QS);
  let user = omit(afterInit, nonUser);
  let userKeys = null;
  let ms = 0;
  for (let f = 0; f < FRAMES; f++) {
    const t0 = performance.now();
    let a = eqs.frame(Object.assign({}, mdVS, qInit, user, globalsAt(f), { megabuf: mdVS.megabuf, gmegabuf: mdVS.gmegabuf }));
    userKeys ??= Object.keys(omit(a, nonUser));
    user = pick(a, userKeys);
    if (eqs.pixel) {
      const v = { ...a };
      for (let iz = 0; iz <= MH; iz++) for (let ix = 0; ix <= MW; ix++) {
        const x = (ix / MW) * 2 - 1, y = (iz / MH) * 2 - 1;
        v.x = x * 0.5 * 0.5625 + 0.5; v.y = y * -0.5 + 0.5; v.rad = Math.hypot(x * 0.5625, y); v.ang = Math.atan2(y, x * 0.5625);
        v.zoom = a.zoom; v.rot = a.rot; v.warp = a.warp; v.cx = a.cx; v.cy = a.cy; v.dx = a.dx; v.dy = a.dy; v.sx = a.sx; v.sy = a.sy;
        eqs.pixel(v);
        if (trace && iz === 18 && ix % 16 === 0) trace.push(["vertex", f, ix, pickNum(v)]);
      }
    }
    ms += performance.now() - t0;
    if (trace) trace.push(["frame", f, 0, pickNum(a)]);
  }
  return ms / FRAMES;
}

// --- the wasm engine ----------------------------------------------------------
function identifiers(src) {
  const out = new Set();
  try { for (const t of tokenize(src)) if (t.t === "id" && !FUNCS[t.v] && !["if", "loop", "while", "megabuf", "gmegabuf", "assign", "memcpy", "memset"].includes(t.v)) out.add(t.v); } catch {}
  return out;
}
async function buildWasm(p, baseVals) {
  const mdVS = baseState(baseVals);
  const frameNames = new Set([...Object.keys(mdVS), ...QS, ...identifiers(p.init), ...identifiers(p.frame)]);
  const pixelNames = new Set([...Object.keys(mdVS), ...QS, "x", "y", "rad", "ang", ...identifiers(p.pixel)]);
  const G = (names) => Object.fromEntries([...names].filter((n) => typeof mdVS[n] !== "object" || mdVS[n] === undefined).map((n) => [n, new WebAssembly.Global({ value: "f64", mutable: true }, 0)]));
  const pools = { perFrame: G(frameNames), perVertex: G(pixelNames) };
  const functions = { presetInit: { pool: "perFrame", code: p.init }, perFrame: { pool: "perFrame", code: p.frame } };
  if (p.pixel.trim()) functions.perPixel = { pool: "perVertex", code: p.pixel };
  const t0 = performance.now();
  const bytes = eelwasm.compileModule({ pools: Object.fromEntries(Object.entries(pools).map(([k, v]) => [k, new Set(Object.keys(v))])), functions, eelVersion: 1 });
  const mod = await WebAssembly.compile(bytes);
  const inst = await WebAssembly.instantiate(mod, { ...pools, shims: eelwasm.shims });
  return { inst, pools, mdVS, bytes: bytes.length, compileMs: performance.now() - t0 };
}
function driveWasm(w, trace) {
  Math.random = mulberry32(7);
  const { inst, pools, mdVS } = w;
  const F = pools.perFrame, V = pools.perVertex;
  const set = (pool, obj) => { for (const k in obj) if (pool[k] && typeof obj[k] === "number") pool[k].value = obj[k]; };
  const read = (pool) => { const o = {}; for (const k in pool) o[k] = pool[k].value; return o; };
  set(F, mdVS);
  inst.exports.presetInit?.();
  const qInit = pick(read(F), QS);
  let ms = 0;
  for (let f = 0; f < FRAMES; f++) {
    const t0 = performance.now();
    // base values reset, q reset to after-init, user variables persist in their globals
    set(F, mdVS); set(F, qInit); set(F, globalsAt(f));
    inst.exports.perFrame?.();
    if (inst.exports.perPixel) {
      // butterchurn clones the frame's variables once per frame for the vertex
      // pass: pixel-only variables start each frame at their after-init value (0)
      for (const k in V) V[k].value = F[k] ? F[k].value : 0;
      for (let iz = 0; iz <= MH; iz++) for (let ix = 0; ix <= MW; ix++) {
        const x = (ix / MW) * 2 - 1, y = (iz / MH) * 2 - 1;
        V.x.value = x * 0.5 * 0.5625 + 0.5; V.y.value = y * -0.5 + 0.5; V.rad.value = Math.hypot(x * 0.5625, y); V.ang.value = Math.atan2(y, x * 0.5625);
        for (const k of ["zoom", "rot", "warp", "cx", "cy", "dx", "dy", "sx", "sy"]) V[k].value = F[k].value;
        inst.exports.perPixel();
        if (trace && iz === 18 && ix % 16 === 0) trace.push(["vertex", f, ix, read(V)]);
      }
    }
    ms += performance.now() - t0;
    if (trace) trace.push(["frame", f, 0, read(F)]);
  }
  return ms / FRAMES;
}

// --- helpers ------------------------------------------------------------------------
function pick(o, keys) { const r = {}; for (const k of keys) if (k in o) r[k] = o[k]; return r; }
function omit(o, set) { const r = {}; for (const k in o) if (!set.has(k)) r[k] = o[k]; return r; }
function pickNum(o) { const r = {}; for (const k in o) if (typeof o[k] === "number") r[k] = o[k]; return r; }
function same(u, v) {
  if (u === v) return true;
  if (Number.isNaN(u) && Number.isNaN(v)) return true;
  return Math.abs(u - v) <= 1e-9 * Math.max(1, Math.abs(u));
}
// First difference between two traces, over the variables both have.
function firstDiff(tA, tB, onlyKeys) {
  const n = Math.min(tA.length, tB.length);
  for (let i = 0; i < n; i++) {
    const [kind, f, ix, a] = tA[i], b = tB[i][3];
    for (const k in a) {
      if (onlyKeys && !onlyKeys.has(k)) continue;
      if (!(k in b)) continue;
      if (!same(a[k], b[k] ?? 0)) return `${kind} ${f}${kind === "vertex" ? "/" + ix : ""}: ${k} = ${a[k]} vs ${b[k]}`;
    }
  }
  return null;
}

const results = [];
for (const file of files) {
  const text = fs.readFileSync(file, "latin1");
  const p = readMilk(text);
  const r = { file: path.basename(file), md2: p.version >= 200, ref: null, interp: null, wasm: null, interpVsRef: null, wasmVsRef: null, ms: {}, wasmBytes: null, wasmCompileMs: null, warp: null, comp: null };
  let c;
  try { c = await conv.convertPreset(text); } catch (e) { r.ref = "converter threw: " + String(e?.message || e).slice(0, 80); results.push(r); continue; }
  if (p.warp.trim()) r.warp = c.warp && c.warp.trim() ? "glsl" : "lost";
  if (p.comp.trim()) r.comp = c.comp && c.comp.trim() ? "glsl" : "lost";
  const baseVals = { ...DEFAULTS, ...c.baseVals };
  const tRef = [], tI = [], tW = [];
  try { r.ms.ref = driveObj({ init: ref(c.init_eqs_str), frame: ref(c.frame_eqs_str), pixel: c.pixel_eqs_str?.trim() ? ref(c.pixel_eqs_str) : null }, baseVals, tRef); }
  catch (e) { r.ref = "ref failed: " + String(e.message).slice(0, 80); }
  try { r.ms.interp = driveObj({ init: equations(p.init), frame: equations(p.frame), pixel: p.pixel.trim() ? equations(p.pixel) : null }, baseVals, tI); }
  catch (e) { r.interp = String(e.message).slice(0, 100); }
  try {
    const w = await buildWasm(p, baseVals);
    r.wasmBytes = w.bytes; r.wasmCompileMs = +w.compileMs.toFixed(2);
    r.ms.wasm = driveWasm(w, tW);
  } catch (e) { r.wasm = String(e.message).split("\n")[0].slice(0, 120); }
  if (!r.ref && !r.interp) r.interpVsRef = firstDiff(tI, tRef);
  if (!r.ref && !r.wasm) r.wasmVsRef = firstDiff(tW, tRef);
  // timing again without trace copies
  if (!r.ref) r.ms.ref = driveObj({ init: ref(c.init_eqs_str), frame: ref(c.frame_eqs_str), pixel: c.pixel_eqs_str?.trim() ? ref(c.pixel_eqs_str) : null }, baseVals, null);
  if (!r.interp) r.ms.interp = driveObj({ init: equations(p.init), frame: equations(p.frame), pixel: p.pixel.trim() ? equations(p.pixel) : null }, baseVals, null);
  if (!r.wasm) r.ms.wasm = driveWasm(await buildWasm(p, baseVals), null);
  results.push(r);
}
fs.writeFileSync(outFile, JSON.stringify(results, null, 1));

const med = (xs) => { const s = xs.filter(Number.isFinite).sort((a, b) => a - b); return s.length ? s[Math.floor(s.length / 2)] : NaN; };
const pct = (xs, p) => { const s = xs.filter(Number.isFinite).sort((a, b) => a - b); return s.length ? s[Math.min(s.length - 1, Math.floor(s.length * p))] : NaN; };
const all3 = results.filter((r) => !r.ref && !r.interp && !r.wasm);
const tot = (k) => all3.reduce((s, r) => s + r.ms[k], 0);
console.log(JSON.stringify({
  presets: results.length,
  refFailed: results.filter((r) => r.ref).length,
  interp: { rejected: results.filter((r) => r.interp).length, comparedWithRef: results.filter((r) => !r.ref && !r.interp).length, identical: results.filter((r) => !r.ref && !r.interp && !r.interpVsRef).length },
  wasm: { rejected: results.filter((r) => r.wasm).length, comparedWithRef: results.filter((r) => !r.ref && !r.wasm).length, identical: results.filter((r) => !r.ref && !r.wasm && !r.wasmVsRef).length },
  msPerFrame_allThreeRan: {
    n: all3.length,
    ref: { p50: med(all3.map((r) => r.ms.ref)).toFixed(3), p90: pct(all3.map((r) => r.ms.ref), 0.9).toFixed(3), p99: pct(all3.map((r) => r.ms.ref), 0.99).toFixed(3) },
    interp: { p50: med(all3.map((r) => r.ms.interp)).toFixed(3), p90: pct(all3.map((r) => r.ms.interp), 0.9).toFixed(3), p99: pct(all3.map((r) => r.ms.interp), 0.99).toFixed(3) },
    wasm: { p50: med(all3.map((r) => r.ms.wasm)).toFixed(3), p90: pct(all3.map((r) => r.ms.wasm), 0.9).toFixed(3), p99: pct(all3.map((r) => r.ms.wasm), 0.99).toFixed(3) },
    totalRatio: { interpOverRef: (tot("interp") / tot("ref")).toFixed(2), wasmOverRef: (tot("wasm") / tot("ref")).toFixed(2) },
  },
  wasmModule: { bytesP50: med(results.map((r) => r.wasmBytes)), compileMsP50: med(results.map((r) => r.wasmCompileMs)), compileMsP99: pct(results.map((r) => r.wasmCompileMs), 0.99) },
  shaders: {
    warp: { glsl: results.filter((r) => r.warp === "glsl").length, lost: results.filter((r) => r.warp === "lost").length },
    comp: { glsl: results.filter((r) => r.comp === "glsl").length, lost: results.filter((r) => r.comp === "lost").length },
  },
}, null, 2));
