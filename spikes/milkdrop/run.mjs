// SPIKE — throwaway. Bulk pass: every .milk under a directory is parsed,
// compiled to closures and run for FRAMES frames the way butterchurn runs
// them (per-frame, per-vertex over a 48x36 mesh, per-wave per-point, per-shape
// per-instance). Records what fails and why, and how long the equations take.
//
//   node run.mjs <dir> [--limit N] [--out results.json]

import fs from "node:fs";
import path from "node:path";
import { readMilk } from "./milk.mjs";
import { equations, EelError } from "./eel.mjs";

const args = process.argv.slice(2);
const dir = args[0];
const limit = +(args[args.indexOf("--limit") + 1] || 0) || Infinity;
const outFile = args.includes("--out") ? args[args.indexOf("--out") + 1] : null;
const FRAMES = 60;
const MESH_W = 48, MESH_H = 36;

// Deterministic Math.random, so runs are repeatable.
function mulberry32(seed) {
  return () => {
    seed |= 0; seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function walk(d, out = []) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (e.name.toLowerCase().endsWith(".milk")) out.push(p);
  }
  return out;
}

const BUF = 1048576;
const bufs = Array.from({ length: 10 }, () => new Float64Array(BUF));

export function frameVars(base, frame) {
  const t = frame / 60;
  const beat = Math.max(0, Math.sin(t * Math.PI * 4)) ** 4; // 120 bpm pulse
  const a = {
    time: t, frame, fps: 60, progress: 0,
    bass: 0.6 + beat, mid: 0.8 + 0.3 * Math.sin(t * 3), treb: 0.7 + 0.2 * Math.sin(t * 7),
    bass_att: 0.7 + beat * 0.6, mid_att: 0.85, treb_att: 0.75,
    meshx: MESH_W, meshy: MESH_H, pixelsx: 1280, pixelsy: 720, aspectx: 0.5625, aspecty: 1,
    rand_start: [0.1, 0.2, 0.3, 0.4], rand_preset: [0.5, 0.6, 0.7, 0.8],
  };
  for (const [k, v] of Object.entries(base)) { const n = parseFloat(v); if (Number.isFinite(n)) a[k] = n; }
  for (let i = 1; i <= 32; i++) a["q" + i] ??= 0;
  return a;
}

const CATS = [
  [/unknown function (\w+)/, (m) => `unknown function ${m[1]}()`],
  [/takes .* arguments/, () => "wrong argument count"],
  [/x\[i\] memory/, () => "x[i] memory addressing"],
  [/unexpected character "(.)"/, (m) => `unexpected character ${m[1]}`],
  [/cannot assign/, () => "assignment to a non-variable"],
  [/./, (m) => m.input.replace(/\d+/g, "N").slice(0, 60)],
];
function category(msg) {
  for (const [re, f] of CATS) { const m = re.exec(msg); if (m) return f(m); }
}

function runPreset(file) {
  const text = fs.readFileSync(file, "latin1");
  const p = readMilk(text);
  const r = {
    file: path.relative(dir, file), version: p.version, psWarp: p.psWarp, psComp: p.psComp,
    hasWarp: p.warp.trim().length > 0, hasComp: p.comp.trim().length > 0,
    errors: [], runtime: null, ms: 0, nan: false, loopCapHits: 0,
  };
  const stats = { loopCapHits: 0 };
  const blocks = [["init", p.init], ["frame", p.frame], ["pixel", p.pixel]];
  p.waves.forEach((w, i) => w.enabled && blocks.push([`wave${i}.init`, w.init], [`wave${i}.frame`, w.frame], [`wave${i}.point`, w.point]));
  p.shapes.forEach((s, i) => s.enabled && blocks.push([`shape${i}.init`, s.init], [`shape${i}.frame`, s.frame]));
  const fns = {};
  for (const [k, src] of blocks) {
    try { fns[k] = equations(src, stats); }
    catch (e) {
      if (!(e instanceof EelError)) throw e;
      r.errors.push({ block: k, msg: e.message, cat: category(e.message), at: src.slice(Math.max(0, (e.pos ?? 0) - 30), (e.pos ?? 0) + 30) });
    }
  }
  if (r.errors.length) return r;

  Math.random = mulberry32(1);
  bufs.forEach((b) => b.fill(0));
  const t0 = performance.now();
  try {
    let a = frameVars(p.base, 0);
    a.megabuf = bufs[0]; a.gmegabuf = bufs[1];
    a = fns.init(a);
    const after = { ...a };
    const wv = p.waves.map((w, i) => w.enabled ? fns[`wave${i}.init`]({ ...frameVars(w, 0), megabuf: bufs[2 + i], gmegabuf: bufs[1] }) : null);
    const sv = p.shapes.map((s, i) => s.enabled ? fns[`shape${i}.init`]({ ...frameVars(s, 0), megabuf: bufs[6 + i], gmegabuf: bufs[1] }) : null);
    for (let f = 0; f < FRAMES; f++) {
      a = fns.frame(Object.assign(frameVars(p.base, f), after, pick(a), { megabuf: bufs[0], gmegabuf: bufs[1] }));
      if (p.pixel.trim()) {
        const v = { ...a };
        for (let iz = 0; iz <= MESH_H; iz++) for (let ix = 0; ix <= MESH_W; ix++) {
          const x = (ix / MESH_W) * 2 - 1, y = (iz / MESH_H) * 2 - 1;
          v.x = x * 0.5 + 0.5; v.y = y * -0.5 + 0.5; v.rad = Math.hypot(x, y); v.ang = Math.atan2(y, x);
          v.zoom = a.zoom; v.rot = a.rot; v.warp = a.warp; v.cx = a.cx; v.cy = a.cy; v.dx = a.dx; v.dy = a.dy; v.sx = a.sx; v.sy = a.sy;
          fns.pixel(v);
        }
      }
      p.waves.forEach((w, i) => {
        if (!w.enabled) return;
        const fr = fns[`wave${i}.frame`](Object.assign({}, wv[i], qs(a)));
        if (w.point.trim()) {
          const n = Math.min(512, w.samples);
          for (let s = 0; s < n; s++) { fr.sample = s / (n - 1 || 1); fr.value1 = Math.sin(s * 0.1); fr.value2 = Math.cos(s * 0.1); fns[`wave${i}.point`](fr); }
        }
      });
      p.shapes.forEach((s, i) => {
        if (!s.enabled) return;
        for (let k = 0; k < Math.min(1024, s.instances); k++) fns[`shape${i}.frame`](Object.assign({}, sv[i], qs(a), { instance: k }));
      });
      for (const k of ["zoom", "rot", "warp", "dx", "dy", "cx", "cy", "decay"]) if (a[k] !== undefined && !Number.isFinite(a[k])) r.nan = true;
    }
  } catch (e) {
    r.runtime = String(e && e.message || e).slice(0, 120);
  }
  r.ms = (performance.now() - t0) / FRAMES;
  r.loopCapHits = stats.loopCapHits;
  return r;
}

function pick(a) {
  // user variables carry over between frames, as butterchurn copies them
  const o = {};
  for (const k in a) if (typeof a[k] === "number") o[k] = a[k];
  return o;
}
function qs(a) {
  const o = {};
  for (let i = 1; i <= 32; i++) o["q" + i] = a["q" + i];
  return o;
}

const files = walk(dir).slice(0, limit);
const results = [];
const t0 = Date.now();
for (const f of files) results.push(runPreset(f));
const ok = results.filter((r) => !r.errors.length && !r.runtime);
const cats = {};
for (const r of results) for (const c of new Set(r.errors.map((e) => e.cat))) cats[c] = (cats[c] || 0) + 1;
const ms = ok.map((r) => r.ms).sort((x, y) => x - y);
const q = (p) => ms[Math.min(ms.length - 1, Math.floor(p * ms.length))]?.toFixed(3);
console.log(JSON.stringify({
  dir: path.basename(dir), presets: results.length, seconds: (Date.now() - t0) / 1000,
  parsedAndRan: ok.length, parseFailures: results.filter((r) => r.errors.length).length,
  runtimeFailures: results.filter((r) => r.runtime).length,
  nanInMotionVars: ok.filter((r) => r.nan).length,
  loopCapHit: ok.filter((r) => r.loopCapHits).length,
  md2: results.filter((r) => r.version >= 200).length,
  withWarpShader: results.filter((r) => r.hasWarp).length, withCompShader: results.filter((r) => r.hasComp).length,
  eqMsPerFrame: { p50: q(0.5), p90: q(0.9), p99: q(0.99), max: ms.at(-1)?.toFixed(2) },
  failureCategories: cats,
}, null, 2));
if (outFile) fs.writeFileSync(outFile, JSON.stringify(results, null, 1));
