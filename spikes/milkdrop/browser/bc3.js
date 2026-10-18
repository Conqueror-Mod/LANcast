// SPIKE — butterchurn 3 with onlyUseWASM, under a CSP without 'unsafe-eval'.
import { FRAMES, W, H, violations, shaderFailures, setCurrent, audioAt, sample, flashesPerSecond, limit, stats } from "./common.js?v=2";
import Butterchurn from "./butterchurn3.js";

const log = (s) => (document.getElementById("log").textContent += "\n" + s);
document.getElementById("log").textContent = "";
const results = [];
window.__spike = { results, violations, shaderFailures, done: false };

// Is eval really blocked here? Prove the CSP bites before trusting its silence.
let evalBlocked = false;
try { new Function("return 1")(); } catch { evalBlocked = true; }
log(`new Function blocked by CSP: ${evalBlocked}`);
window.__spike.evalBlocked = evalBlocked;

const presets = await (await fetch(new URLSearchParams(location.search).get("set") || "presets.json")).json();
const audioCtx = new AudioContext();
const canvas = document.getElementById("c");
const viz = Butterchurn.createVisualizer(audioCtx, canvas, { width: W, height: H, onlyUseWASM: true, deterministic: true });

for (const p of presets) {
  if (p.error) { results.push({ name: p.name, error: "converter: " + p.error }); continue; }
  setCurrent(p.name);
  const before = shaderFailures.length;
  const r = { name: p.name, md2: p.md2, hlsl: p.hlsl };
  try {
    const t0 = performance.now();
    await viz.loadPreset(structuredClone(p), 0);
    r.loadMs = +(performance.now() - t0).toFixed(1);
    const frames = [], golds = [], renderMs = [];
    for (let f = 0; f < FRAMES; f++) {
      const t = performance.now();
      viz.render({ audioLevels: audioAt(f), elapsedTime: 1 / 60 });
      renderMs.push(performance.now() - t);
      const s = sample(canvas);
      frames.push(s.cells); golds.push(s.gold);
      if (f % 60 === 59) await new Promise((res) => setTimeout(res, 0));
    }
    r.renderMs = stats(renderMs);
    r.meanLum = +(frames.reduce((s, c) => s + c.reduce((a, b) => a + b, 0) / c.length, 0) / frames.length).toFixed(3);
    r.gold = +(golds.reduce((a, b) => a + b, 0) / golds.length).toFixed(3);
    r.flashes = flashesPerSecond(frames);
    const lim = limit(frames, 0.6);
    r.flashesLimited = flashesPerSecond(lim.frames);
    r.limiterPull = +lim.meanPull.toFixed(3);
  } catch (e) { r.error = String(e && e.message || e).slice(0, 160); }
  r.shaderFailures = shaderFailures.length - before;
  results.push(r);
  log(`${r.name.slice(0, 60).padEnd(60)} ${r.error ? "ERROR " + r.error : `render ${r.renderMs.p50}ms lum ${r.meanLum} gold ${r.gold} flashes ${r.flashes}/s -> ${r.flashesLimited}/s shaderFail ${r.shaderFailures}`}`);
}
log(`done. CSP violations: ${violations.length}`);
window.__spike.done = true;
