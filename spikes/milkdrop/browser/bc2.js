// SPIKE — butterchurn 2.6.7 handed closures from eel.mjs instead of JS strings.
// loadPreset compiles with new Function only `if (typeof preset.init_eqs !==
// 'function')`; these are functions, so it never does — and the CSP would
// throw if it tried.
import { FRAMES, W, H, violations, shaderFailures, setCurrent, audioAt, sample, flashesPerSecond, limit, stats } from "./common.js?v=2";
import { equations } from "./eel.js";

const log = (s) => (document.getElementById("log").textContent += "\n" + s);
document.getElementById("log").textContent = "";
const results = [];
window.__spike = { results, violations, shaderFailures, done: false };
let evalBlocked = false;
try { new Function("return 1")(); } catch { evalBlocked = true; }
log(`new Function blocked by CSP: ${evalBlocked}`);
window.__spike.evalBlocked = evalBlocked;

const blank = (src) => (src && src.trim() ? equations(src) : null);
function toButterchurn2(p) {
  const m = p.milk;
  return {
    baseVals: p.baseVals, warp: p.warp, comp: p.comp,
    init_eqs: equations(m.init), frame_eqs: equations(m.frame), pixel_eqs: blank(m.pixel) || "",
    shapes: p.shapes.map((s, i) => ({ baseVals: s.baseVals, init_eqs: equations(m.shapes[i].init), frame_eqs: equations(m.shapes[i].frame) })),
    waves: p.waves.map((w, i) => ({ baseVals: w.baseVals, init_eqs: equations(m.waves[i].init), frame_eqs: equations(m.waves[i].frame), point_eqs: blank(m.waves[i].point) || "" })),
  };
}

const presets = await (await fetch(new URLSearchParams(location.search).get("set") || "presets.json")).json();
const audioCtx = new AudioContext();
const canvas = document.getElementById("c");
const viz = window.butterchurn.default.createVisualizer(audioCtx, canvas, { width: W, height: H });

for (const p of presets) {
  if (p.error) { results.push({ name: p.name, error: "converter: " + p.error }); continue; }
  setCurrent(p.name);
  const before = shaderFailures.length;
  const r = { name: p.name, md2: p.md2, hlsl: p.hlsl };
  try {
    viz.loadPreset(toButterchurn2(p), 0);
    const frames = [], renderMs = [];
    for (let f = 0; f < FRAMES; f++) {
      const t = performance.now();
      viz.render({ audioLevels: audioAt(f), elapsedTime: 1 / 60 });
      renderMs.push(performance.now() - t);
      frames.push(sample(canvas).cells);
      if (f % 60 === 59) await new Promise((res) => setTimeout(res, 0));
    }
    r.renderMs = stats(renderMs);
    r.meanLum = +(frames.reduce((s, c) => s + c.reduce((a, b) => a + b, 0) / c.length, 0) / frames.length).toFixed(3);
    r.flashes = flashesPerSecond(frames);
  } catch (e) { r.error = String(e && e.message || e).slice(0, 160); }
  r.shaderFailures = shaderFailures.length - before;
  results.push(r);
  log(`${r.name.slice(0, 60).padEnd(60)} ${r.error ? "ERROR " + r.error : `render ${r.renderMs.p50}ms lum ${r.meanLum} flashes ${r.flashes}/s shaderFail ${r.shaderFailures}`}`);
}
log(`done. CSP violations: ${violations.length}`);
window.__spike.done = true;
