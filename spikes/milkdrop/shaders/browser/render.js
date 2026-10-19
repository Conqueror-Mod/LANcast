// SPIKE (ADR 0091 step g). Do the converted shaders compile in WebGL 2, and
// do they change the picture?
//
// Served beside: butterchurn.min.js (web/vendor/butterchurn, LANcast's patched
// build), common.js (../../browser/common.js) and the set from bundle/
// (?set=presets.json&from=0&to=200). Audio is synthetic bytes handed to
// render(); nothing is ever routed to a speaker.
//
// Each preset runs twice from a black frame: once as step 1 stores it (no
// shaders, butterchurn's defaults) and once with the converted GLSL. The page
// records compile and link failures, GL errors, a lost context, frame time,
// mean luminance, and how far the shader run's picture is from the bare run's.
import { W, H, violations, shaderFailures, setCurrent, audioAt, sample, stats } from "./common.js";
import Butterchurn from "./butterchurn.min.js";

const log = (s) => (document.getElementById("log").textContent = s + "\n" + document.getElementById("log").textContent.slice(0, 4000));
document.getElementById("log").textContent = "";

const linkFailures = [];
const origLink = WebGL2RenderingContext.prototype.linkProgram;
let current = "";
WebGL2RenderingContext.prototype.linkProgram = function (p) {
  origLink.call(this, p);
  if (!this.getProgramParameter(p, this.LINK_STATUS)) linkFailures.push({ preset: current, log: (this.getProgramInfoLog(p) || "").slice(0, 160) });
};

let evalBlocked = false;
try { new Function("return 1")(); } catch { evalBlocked = true; }

const q = new URLSearchParams(location.search);
const FRAMES = +(q.get("frames") || 90);
const ONLY = q.get("only") ? new Set(q.get("only").split("|")) : null;
const all = await (await fetch(q.get("set") || "presets.json")).json();
const from = +(q.get("from") || 0), to = +(q.get("to") || all.length);
const set = all.slice(from, to).filter((e) => !ONLY || ONLY.has(e.id.split("/").pop()));

const results = [];
window.__spike = { results, violations, shaderFailures, linkFailures, evalBlocked, lost: 0, done: false, total: set.length };

const audioCtx = new AudioContext();
const canvas = document.getElementById("c");
canvas.addEventListener("webglcontextlost", () => window.__spike.lost++);
const viz = Butterchurn.createVisualizer(audioCtx, canvas, { width: W, height: H, onlyUseWASM: true, deterministic: true });
// butterchurn's own GL context (it draws off-page and copies to the 2D canvas).
const gl = viz.gl || viz.renderer?.gl || null;
window.__spike.hasGL = !!gl;

const off = { enabled: 0 };
const BLACK = {
  version: 2, baseVals: { decay: 0, wave_a: 0, ob_size: 0, ib_size: 0, mv_a: 0, echo_alpha: 0, gammaadj: 1 },
  init_eqs_eel: "", frame_eqs_eel: "", pixel_eqs_eel: "", warp: "", comp: "",
  shapes: Array.from({ length: 4 }, () => ({ baseVals: off, init_eqs_eel: "", frame_eqs_eel: "" })),
  waves: Array.from({ length: 4 }, () => ({ baseVals: off, init_eqs_eel: "", frame_eqs_eel: "", point_eqs_eel: "" })),
};

async function run(preset) {
  await viz.loadPreset(structuredClone(BLACK), 0);
  for (let f = 0; f < 4; f++) viz.render({ audioLevels: audioAt(0), elapsedTime: 1 / 60 });
  const t0 = performance.now();
  await viz.loadPreset(structuredClone(preset), 0);
  const loadMs = performance.now() - t0;
  const frames = [], ms = [];
  let glErrors = 0;
  for (let f = 0; f < FRAMES; f++) {
    const t = performance.now();
    viz.render({ audioLevels: audioAt(f), elapsedTime: 1 / 60 });
    const s = sample(canvas);
    ms.push(performance.now() - t);
    frames.push(s.cells);
    if (gl && gl.getError() !== 0) glErrors++;
    if (f % 30 === 29) await new Promise((r) => setTimeout(r, 0));
  }
  const lum = frames.map((c) => c.reduce((a, b) => a + b, 0) / c.length);
  return { frames, loadMs, ms, glErrors, lum };
}

const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length;
for (let i = 0; i < set.length; i++) {
  const e = set[i];
  current = e.id;
  setCurrent(e.id);
  const r = { id: e.id };
  const sf = shaderFailures.length, lf = linkFailures.length;
  try {
    const bare = await run(e.bare);
    const sh = await run(e.preset);
    r.compileFailures = shaderFailures.length - sf;
    r.linkFailures = linkFailures.length - lf;
    r.glErrors = sh.glErrors;
    r.loadMs = +sh.loadMs.toFixed(1);
    r.bareLoadMs = +bare.loadMs.toFixed(1);
    r.ms = stats(sh.ms);
    r.bareMs = stats(bare.ms);
    r.lum = +mean(sh.lum).toFixed(3);
    r.bareLum = +mean(bare.lum).toFixed(3);
    r.lumTail = +mean(sh.lum.slice(-30)).toFixed(4);
    r.bareLumTail = +mean(bare.lum.slice(-30)).toFixed(4);
    // A frame is "black" if its mean luminance is under 0.002.
    r.blackFrames = sh.lum.filter((l) => l < 0.002).length;
    r.bareBlackFrames = bare.lum.filter((l) => l < 0.002).length;
    // Mean absolute per-cell luminance difference between the two runs.
    let d = 0, n = 0;
    for (let f = 0; f < FRAMES; f++) for (let c = 0; c < sh.frames[f].length; c++) { d += Math.abs(sh.frames[f][c] - bare.frames[f][c]); n++; }
    r.diff = +(d / n).toFixed(4);
  } catch (err) {
    r.error = String((err && err.message) || err).slice(0, 160);
  }
  results.push(r);
  log(`${from + i} ${e.id.slice(0, 70).padEnd(70)} ${r.error ? "ERROR " + r.error : `cf ${r.compileFailures} lf ${r.linkFailures} lum ${r.bareLum}->${r.lum} diff ${r.diff} ${r.ms.p50}ms`}`);
}
window.__spike.done = true;
log(`done ${results.length}; CSP violations ${violations.length}; eval blocked ${evalBlocked}`);
