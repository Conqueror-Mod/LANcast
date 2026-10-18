// SPIKE — throwaway. Shared by bc3.html (butterchurn 3, WASM) and bc2.html
// (butterchurn 2 + the closure interpreter): synthetic music, the readback,
// and the photosensitivity and gold measurements.

export const FRAMES = 300; // 5 s at 60 fps of preset time
export const W = 960, H = 540;
const CW = 32, CH = 18; // luminance cells
const GW = 64, GH = 36; // gold sample

// ---- CSP and shader-compile watch --------------------------------------------
export const violations = [];
document.addEventListener("securitypolicyviolation", (e) => violations.push(`${e.violatedDirective} blocked ${e.blockedURI || "(inline/eval)"}`));
export const shaderFailures = [];
export let current = "";
export function setCurrent(n) { current = n; }
const origCompile = WebGL2RenderingContext.prototype.compileShader;
WebGL2RenderingContext.prototype.compileShader = function (s) {
  origCompile.call(this, s);
  if (!this.getShaderParameter(s, this.COMPILE_STATUS)) shaderFailures.push({ preset: current, log: (this.getShaderInfoLog(s) || "").slice(0, 160) });
};

// ---- music: a 120 bpm kick, a held note, hats --------------------------------
function rng(seed) { return () => { seed = (seed * 1664525 + 1013904223) >>> 0; return seed / 4294967296; }; }
export function audioAt(frame) {
  const r = rng(frame + 1);
  const t0 = frame / 60;
  const n = 1024, a = new Uint8Array(n);
  for (let i = 0; i < n; i++) {
    const t = t0 + i / 44100;
    const kt = t % 0.5, ht = (t + 0.25) % 0.5;
    const kick = 90 * Math.exp(-kt * 10) * Math.sin(2 * Math.PI * (50 + 60 * Math.exp(-kt * 30)) * kt);
    const note = 18 * Math.sin(2 * Math.PI * 330 * t) + 10 * Math.sin(2 * Math.PI * 660 * t);
    const hat = 25 * (r() * 2 - 1) * Math.exp(-ht * 40);
    a[i] = Math.max(0, Math.min(255, Math.round(128 + kick + note + hat)));
  }
  return { timeByteArray: a, timeByteArrayL: a, timeByteArrayR: a };
}

// ---- readback -------------------------------------------------------------------
const small = Object.assign(document.createElement("canvas"), { width: GW, height: GH });
const sctx = small.getContext("2d", { willReadFrequently: true });
const lin = (c) => { c /= 255; return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; };
const LUT = Float32Array.from({ length: 256 }, (_, i) => lin(i));

/** One frame: relative luminance per cell (CW x CH), and the gold fraction. */
export function sample(canvas) {
  sctx.drawImage(canvas, 0, 0, GW, GH);
  const d = sctx.getImageData(0, 0, GW, GH).data;
  const cells = new Float32Array(CW * CH);
  let gold = 0;
  for (let y = 0; y < GH; y++) for (let x = 0; x < GW; x++) {
    const i = (y * GW + x) * 4, r = d[i], g = d[i + 1], b = d[i + 2];
    const L = 0.2126 * LUT[r] + 0.7152 * LUT[g] + 0.0722 * LUT[b];
    cells[((y >> 1) * CW) + (x >> 1)] += L / 4;
    // gold: hue 20–75°, saturated, not near black (vizMath: GOLD_HUE_MIN/MAX, s >= 0.12)
    const mx = Math.max(r, g, b), mn = Math.min(r, g, b);
    if (mx < 40 || mx === mn) continue;
    const l = (mx + mn) / 510, s = (mx - mn) / 255 / (1 - Math.abs(2 * l - 1) || 1);
    if (s < 0.12) continue;
    let h;
    if (mx === r) h = 60 * (((g - b) / (mx - mn)) % 6);
    else if (mx === g) h = 60 * ((b - r) / (mx - mn) + 2);
    else h = 60 * ((r - g) / (mx - mn) + 4);
    if (h < 0) h += 360;
    if (h >= 20 && h <= 75) gold++;
  }
  return { cells, gold: gold / (GW * GH) };
}

// ---- flashes (WCAG 2.3.1 general flash, simplified) ------------------------------
// A transition is a change of >= 0.1 relative luminance in a window where the
// darker state is below 0.8; a flash is two opposing transitions. A window is a
// third of the width by a third of the height (~11% of the screen, about the
// "25% of a 10° field" area at living-room distance), at a stride of a sixth.
function windows(cells) {
  const out = [];
  for (let wy = 0; wy <= 4; wy++) for (let wx = 0; wx <= 4; wx++) {
    let s = 0, n = 0;
    const x0 = Math.round((wx * CW) / 6), y0 = Math.round((wy * CH) / 6);
    for (let y = y0; y < y0 + CH / 3; y++) for (let x = x0; x < x0 + CW / 3; x++) { s += cells[y * CW + x]; n++; }
    out.push(s / n);
  }
  return out;
}
export function flashesPerSecond(frames) {
  const series = frames.map((c) => windows(c));
  let worst = 0;
  for (let w = 0; w < series[0].length; w++) {
    const times = [];
    // ext follows the running extreme in the current direction; a reversal of
    // >= 0.1 from it is a transition. (The first version let ext follow every
    // value while no direction was set, so it never counted anything — caught
    // by the synthetic-strobe positive control.)
    let ext = series[0][w], dir = 0;
    for (let f = 1; f < series.length; f++) {
      const L = series[f][w];
      const big = Math.abs(L - ext) >= 0.1 && Math.min(L, ext) < 0.8;
      if (dir === 1 && L > ext) ext = L;
      else if (dir === -1 && L < ext) ext = L;
      else if (big) { times.push(f); dir = L > ext ? 1 : -1; ext = L; }
    }
    for (let i = 0; i < times.length; i++) {
      let j = i;
      while (j < times.length && times[j] - times[i] < 60) j++;
      worst = Math.max(worst, (j - i) / 2);
    }
  }
  return worst;
}

// A limiter LANcast could put after any preset: each cell's luminance may move
// at most RATE per second. A flash needs two 0.1 transitions, so three a second
// need 0.6/s of movement: RATE 0.6 makes >3/s impossible in any window, since a
// window's mean cannot move faster than its fastest cell. Returns the limited
// frames and how far the picture had to be pulled from what the preset drew.
export function limit(frames, rate) {
  const step = rate / 60;
  const cur = Float32Array.from(frames[0]);
  const out = [Float32Array.from(cur)];
  let pulled = 0, n = 0;
  for (let f = 1; f < frames.length; f++) {
    const t = frames[f];
    for (let i = 0; i < cur.length; i++) {
      const d = Math.max(-step, Math.min(step, t[i] - cur[i]));
      cur[i] += d;
      pulled += Math.abs(t[i] - cur[i]); n++;
    }
    out.push(Float32Array.from(cur));
  }
  return { frames: out, meanPull: pulled / n };
}

export function stats(xs) {
  const s = [...xs].sort((a, b) => a - b);
  return { p50: +s[Math.floor(s.length / 2)].toFixed(2), p95: +s[Math.floor(s.length * 0.95)].toFixed(2) };
}
