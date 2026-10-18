// SPIKE — shared with run.mjs (copied out so diff.mjs can import it)
const MESH_W = 48, MESH_H = 36;
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
