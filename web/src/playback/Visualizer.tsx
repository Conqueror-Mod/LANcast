import { useEffect, useRef, useState } from "react";
import type { Stage } from "./prefs";
import type { Surface } from "./PlaybackProvider";
import {
  calmMotion,
  canvasSize,
  css,
  fallPeaks,
  groupBins,
  logBands,
  MOTION,
  NEBULA_PALETTE,
  newPeaks,
  paletteFrom,
  scopePoints,
  smooth,
  type BinRange,
  type Peaks,
  type RGB,
} from "./vizMath";

/*
 * The classic visualizers in Now Playing's stage (docs/now-playing-plan.md,
 * step 2): spectrum bars with falling caps, and the scope's one line.
 *
 * Mounted by the provider as a sibling of `.playback__cover`, never inside or
 * beside `.playback__slot` (see the slot comment there, and
 * crossDocumentMove.test.tsx). It draws on a transparent canvas over the
 * cover's wash, the same square the cover took, so the field never changes
 * with the music: only the bars and the line move, and how fast they can fall
 * is what holds them to three flashes a second (vizMath.ts, MOTION).
 *
 * No gold. The colours are the cover's, nudged off gold hues, or the nebula.
 */

export type VizKind = Exclude<Stage, "cover">;

/** Bars in the spectrum. Enough to read as a spectrum, few enough to read as bars. */
export const SPECTRUM_BARS = 48;

/*
 * vizShouldRun: one loop, and only while somebody can see it move. Not on the
 * mini player (the cover shows there), not in a hidden tab, and not while
 * paused, when the last frame holds rather than going black.
 */
export function vizShouldRun(s: { surface: Surface; stage: Stage; playing: boolean; hidden: boolean }): boolean {
  return s.surface === "full" && s.stage !== "cover" && s.playing && !s.hidden;
}

/** Whether the tab is hidden, kept current. */
export function useDocumentHidden(): boolean {
  const [hidden, setHidden] = useState(() => typeof document !== "undefined" && document.hidden);
  useEffect(() => {
    const on = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", on);
    return () => document.removeEventListener("visibilitychange", on);
  }, []);
  return hidden;
}

/** prefers-reduced-motion, kept current; false where it cannot be asked. */
export function usePrefersReducedMotion(): boolean {
  const query = "(prefers-reduced-motion: reduce)";
  const [reduced, setReduced] = useState(
    () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia(query).matches,
  );
  useEffect(() => {
    if (typeof window.matchMedia !== "function") return;
    const mq = window.matchMedia(query);
    const on = () => setReduced(mq.matches);
    mq.addEventListener?.("change", on);
    return () => mq.removeEventListener?.("change", on);
  }, []);
  return reduced;
}

/*
 * The cover's palette: the art drawn into an 8×8 canvas and read back. The
 * cover is the server's own artwork, same origin, so the read is allowed; if
 * anything refuses (no 2D context, a tainted canvas), the nebula.
 */
function useCoverPalette(cover: string | undefined): RGB[] {
  const [palette, setPalette] = useState<RGB[]>(NEBULA_PALETTE);
  useEffect(() => {
    setPalette(NEBULA_PALETTE);
    if (!cover) return;
    let live = true;
    const img = new Image();
    img.onload = () => {
      if (!live) return;
      try {
        const c = document.createElement("canvas");
        c.width = 8;
        c.height = 8;
        const g = c.getContext("2d", { willReadFrequently: true });
        if (!g) return;
        g.drawImage(img, 0, 0, 8, 8);
        setPalette(paletteFrom(g.getImageData(0, 0, 8, 8).data));
      } catch {
        // Tainted or unsupported: the nebula stands.
      }
    };
    img.src = cover;
    return () => {
      live = false;
    };
  }, [cover]);
  return palette;
}

export function Visualizer({
  kind,
  analyser,
  running,
  calm,
  cover,
}: {
  kind: VizKind;
  /** The analyser on whichever path music is on, looked up each frame: the
   *  engine may be built after this mounts, and the path can change. */
  analyser: () => AnalyserNode | undefined;
  running: boolean;
  calm: boolean;
  cover: string | undefined;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const palette = useCoverPalette(cover);
  // Read by the loop through a ref, so a palette arriving does not restart it.
  const paletteRef = useRef(palette);
  paletteRef.current = palette;
  const analyserRef = useRef(analyser);
  analyserRef.current = analyser;

  useEffect(() => {
    if (!running) return; // the last frame holds
    const canvas = canvasRef.current;
    const g = canvas?.getContext("2d");
    if (!canvas || !g) return;
    const motion = calm ? calmMotion(MOTION) : MOTION;
    let bands: BinRange[] = [];
    let bandsFor = "";
    let freq: Uint8Array<ArrayBuffer> = new Uint8Array(0);
    let time: Uint8Array<ArrayBuffer> = new Uint8Array(0);
    const raw = new Float32Array(SPECTRUM_BARS);
    const bars = new Float32Array(SPECTRUM_BARS);
    const peaks: Peaks = newPeaks(SPECTRUM_BARS);
    let last = -1;
    let id = 0;

    const frame = (now: number) => {
      id = requestAnimationFrame(frame);
      // A frame after a long gap (a pause, a dropped tab) moves by one frame's
      // worth, not by the whole gap at once.
      const dt = last < 0 ? 1 / 60 : Math.min(0.1, (now - last) / 1000);
      last = now;

      const box = canvas.getBoundingClientRect();
      const { w, h } = canvasSize(box.width, box.height, window.devicePixelRatio);
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      const ratio = box.width > 0 ? w / box.width : 1;
      g.clearRect(0, 0, w, h);
      const node = analyserRef.current();
      if (!node) return;
      const pal = paletteRef.current;

      if (kind === "spectrum") {
        const key = `${node.frequencyBinCount}@${node.context.sampleRate}`;
        if (key !== bandsFor) {
          bands = logBands(SPECTRUM_BARS, node.frequencyBinCount, node.context.sampleRate);
          freq = new Uint8Array(node.frequencyBinCount);
          bandsFor = key;
        }
        node.getByteFrequencyData(freq);
        groupBins(freq, bands, raw);
        smooth(bars, raw, dt, motion);
        fallPeaks(peaks, bars, dt, motion);
        drawSpectrum(g, w, h, ratio, bars, peaks.level, pal);
      } else {
        if (time.length !== node.fftSize) time = new Uint8Array(node.fftSize);
        node.getByteTimeDomainData(time);
        // Calm draws the line at half the height: half the motion.
        drawScope(g, w, h, ratio, time, calm ? 0.45 : 0.9, pal);
      }
    };
    id = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(id);
  }, [running, kind, calm]);

  return <canvas ref={canvasRef} className="playback__viz-canvas" data-viz={kind} aria-hidden="true" />;
}

function drawSpectrum(
  g: CanvasRenderingContext2D,
  w: number,
  h: number,
  ratio: number,
  bars: Float32Array,
  caps: Float32Array,
  pal: RGB[],
) {
  const pad = Math.round(14 * ratio);
  const capH = Math.max(2, Math.round(2 * ratio));
  const n = bars.length;
  const slot = (w - pad * 2) / n;
  const bw = Math.max(1, slot * 0.72);
  const floor = h - pad;
  const room = h - pad * 2 - capH * 2;
  const grad = g.createLinearGradient(0, floor, 0, pad);
  grad.addColorStop(0, css(pal[0], 0.95));
  grad.addColorStop(1, css(pal[1], 0.95));
  g.fillStyle = grad;
  for (let i = 0; i < n; i++) {
    const bh = Math.max(capH / 2, bars[i] * room);
    g.fillRect(pad + i * slot, floor - bh, bw, bh);
  }
  g.fillStyle = css(pal[2], 0.9);
  for (let i = 0; i < n; i++) {
    const y = floor - caps[i] * room - capH * 2;
    g.fillRect(pad + i * slot, y, bw, capH);
  }
}

function drawScope(
  g: CanvasRenderingContext2D,
  w: number,
  h: number,
  ratio: number,
  time: Uint8Array,
  gain: number,
  pal: RGB[],
) {
  const pts = scopePoints(time, Math.min(512, Math.max(64, Math.round(w / 2))), w, h, gain);
  g.lineJoin = "round";
  g.lineCap = "round";
  g.beginPath();
  for (let i = 0; i < pts.length; i += 2) {
    if (i === 0) g.moveTo(pts[i], pts[i + 1]);
    else g.lineTo(pts[i], pts[i + 1]);
  }
  // A wide faint stroke under a thin bright one: a glow without shadowBlur,
  // which is the expensive way to get the same thing.
  g.strokeStyle = css(pal[1], 0.35);
  g.lineWidth = 6 * ratio;
  g.stroke();
  g.strokeStyle = css(pal[0], 0.95);
  g.lineWidth = 2 * ratio;
  g.stroke();
}
