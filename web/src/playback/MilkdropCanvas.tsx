import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Preset, Visualizer as Butterchurn } from "butterchurn";
import { fetchMilkdropPreset } from "@/api/hooks";
import { loadButterchurn, type MilkdropEngine } from "./loadButterchurn";
import { createOutputPass, type OutputPass } from "./milkdropPass";
import { limitRate } from "./limiterMath";
import { dropPreset, milkdropStatusText, setMilkdropStatus, useMilkdropStatus } from "./milkdropStage";
import { trailSize } from "./trailMath";

/*
 * The MilkDrop stage (ADR 0091, steps d and e).
 *
 * butterchurn draws each frame into a canvas of its own that is never in the
 * page; the canvas that is in the page belongs to the output pass
 * (milkdropPass.ts), which takes butterchurn's frame as a texture and puts it
 * on the screen through the gold guard and the luminance limiter. So nothing a
 * preset draws reaches the screen any other way.
 *
 * The rules the other stages keep hold here too: one loop, only while
 * somebody can see it move (the provider's `running`), and paused, the last
 * frame holds. The buffers are sized as the trails' are, capped for a 4K TV.
 * The canvas is the provider's sibling of `.playback__cover`, like the rest.
 */

/** How long one preset blends into the next, in seconds. */
const BLEND_SECONDS = 2;

/*
 * How long a switch between presets may take before the stage says it is
 * loading. The old preset keeps drawing meanwhile, so a quick switch needs no
 * words, and words that flash for a frame read as a fault.
 */
export const SLOW_SWITCH_MS = 400;

/** How often to look for the music's analyser before the engine can be made. */
const ANALYSER_POLL_MS = 250;

/*
 * butterchurn's own audio window: it was written around an analyser of 1024
 * samples and copies exactly that many, so it is fed the newest 1024 of the
 * tap's 2048, which is the same span of time its own analyser would see.
 */
const BUTTERCHURN_SAMPLES = 1024;

/*
 * One butterchurn per AudioContext, for the page's life. It holds a WebGL2
 * context of its own, and a browser keeps only a handful of those alive: one
 * per visit to Now Playing would have the browser closing older contexts
 * (the trails' among them) after a dozen visits. It costs memory between
 * visits, which is why it is made only once MilkDrop is first shown.
 */
interface Engine {
  viz: Butterchurn;
  out: HTMLCanvasElement;
  w: number;
  h: number;
  /** The preset loaded, or "" before the first. */
  loaded: string;
}
const engines = new WeakMap<BaseAudioContext, Promise<Engine>>();

/*
 * The chunk on its own, which needs no AudioContext: asked for as soon as the
 * stage mounts, so the download is under way before the music's graph is, and
 * a failure can be said at once. A failed download is not cached either.
 */
let chunk: Promise<MilkdropEngine> | null = null;

function butterchurnChunk(): Promise<MilkdropEngine> {
  if (!chunk) {
    const p = loadButterchurn();
    p.catch(() => {
      if (chunk === p) chunk = null;
    });
    chunk = p;
  }
  return chunk;
}

/** For tests: forget the chunk, so the next stage loads it again. */
export function forgetButterchurnForTests() {
  chunk = null;
}

function engineFor(ctx: BaseAudioContext): Promise<Engine> {
  let e = engines.get(ctx);
  if (!e) {
    e = butterchurnChunk().then((bc) => {
      const out = document.createElement("canvas");
      out.width = 2;
      out.height = 2;
      const viz = bc.createVisualizer(ctx as AudioContext, out, { width: 2, height: 2, pixelRatio: 1, textureRatio: 1 });
      return { viz, out, w: 2, h: 2, loaded: "" };
    });
    // A failed load is not cached, so a later visit can try again.
    e.catch(() => engines.delete(ctx));
    engines.set(ctx, e);
  }
  return e;
}

/** The newest `out.length` samples of `td`, or silence where there are fewer. */
export function newestSamples(td: Uint8Array, out: Uint8Array): Uint8Array {
  if (td.length >= out.length) out.set(td.subarray(td.length - out.length));
  else {
    out.fill(128);
    out.set(td, out.length - td.length);
  }
  return out;
}

/*
 * The presets either side of `id` in the stepper's order: fetched ahead once
 * `id` is drawing, so ‹ and › wait only for the compile, not for the network
 * too. Only the JSON: compiling one means loading it into the engine, which
 * would switch to it, and there is one engine per context (above).
 */
export function presetNeighbours(id: string, ids: string[]): string[] {
  const at = ids.indexOf(id);
  const n = ids.length;
  if (at < 0 || n < 2) return [];
  const next = ids[(at + 1) % n];
  const prev = ids[(at - 1 + n) % n];
  return next === prev ? [next] : [next, prev];
}

interface Props {
  /** The preset to play, already resolved; "" draws nothing. */
  preset: string;
  /** Every stored preset, for moving on from one that fails. */
  ids: string[];
  running: boolean;
  calm: boolean;
  analyser: { current: () => AnalyserNode | undefined };
}

export function MilkdropCanvas({ preset, ids, running, calm, analyser: analyserRef }: Props) {
  const qc = useQueryClient();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const passRef = useRef<OutputPass | null>(null);
  const engineRef = useRef<Engine | null>(null);
  // Whether the engine has the wanted preset and may be drawn.
  const readyRef = useRef(false);
  const presetRef = useRef(preset);
  presetRef.current = preset;
  const idsRef = useRef(ids);
  idsRef.current = ids;
  const calmRef = useRef(calm);
  calmRef.current = calm;
  const loadToken = useRef(0);
  const slowTimer = useRef(0);
  const status = useMilkdropStatus();

  useEffect(
    () => () => {
      passRef.current?.dispose();
      passRef.current = null;
      window.clearTimeout(slowTimer.current);
      setMilkdropStatus("idle");
    },
    [],
  );

  /*
   * The engine, made as soon as the stage is up rather than on its first
   * playing frame: the chunk starts downloading at once, and the engine is
   * made when the music's AudioContext exists, playing or not. Until then the
   * stage says it is loading; if the chunk fails, it says that instead.
   */
  useEffect(() => {
    let live = true;
    setMilkdropStatus("loading");
    const failed = (err: unknown) => {
      if (!live) return;
      console.warn("milkdrop: butterchurn did not load", err);
      setMilkdropStatus("failed");
    };
    butterchurnChunk().catch(failed);
    const tryEngine = () => {
      const node = analyserRef.current();
      if (!node) return false;
      engineFor(node.context)
        .then((made) => {
          if (!live) return;
          engineRef.current = made;
          loadRef.current(made);
        })
        .catch(failed);
      return true;
    };
    let poll = 0;
    if (!tryEngine()) {
      poll = window.setInterval(() => {
        if (tryEngine()) window.clearInterval(poll);
      }, ANALYSER_POLL_MS);
    }
    return () => {
      live = false;
      window.clearInterval(poll);
    };
  }, [analyserRef]);

  /*
   * Load the wanted preset into the engine. Called when the engine first
   * arrives and whenever the preset changes; a load overtaken by a newer one
   * leaves the stage to the newer.
   */
  const loadRef = useRef<(e: Engine) => void>(() => {});
  loadRef.current = (e: Engine) => {
    const id = presetRef.current;
    if (!id) {
      readyRef.current = false;
      return;
    }
    window.clearTimeout(slowTimer.current);
    if (e.loaded === id) {
      readyRef.current = true;
      setMilkdropStatus("ready");
      return;
    }
    const token = ++loadToken.current;
    const blend = e.loaded && readyRef.current ? BLEND_SECONDS : 0;
    // With nothing drawn yet, say so at once. A switch keeps the old preset
    // drawing through the load, and speaks only if it is slow.
    if (readyRef.current) {
      slowTimer.current = window.setTimeout(() => {
        if (token === loadToken.current) setMilkdropStatus("loading");
      }, SLOW_SWITCH_MS);
    } else {
      setMilkdropStatus("loading");
    }
    fetchMilkdropPreset(qc, id)
      .then((p) => e.viz.loadPreset(p.preset as unknown as Preset, blend))
      .then(() => {
        if (token !== loadToken.current) return;
        window.clearTimeout(slowTimer.current);
        e.loaded = id;
        readyRef.current = true;
        setMilkdropStatus("ready");
        for (const near of presetNeighbours(id, idsRef.current)) {
          // A failure here is the real load's to report, not the prefetch's.
          fetchMilkdropPreset(qc, near).catch(() => {});
        }
      })
      .catch((err) => {
        if (token !== loadToken.current) return;
        window.clearTimeout(slowTimer.current);
        console.warn("milkdrop: preset did not load, skipping it", id, err);
        readyRef.current = false;
        dropPreset(id, idsRef.current);
      });
  };

  useEffect(() => {
    if (engineRef.current) loadRef.current(engineRef.current);
  }, [preset]);

  useEffect(() => {
    if (!running) return; // the last frame holds
    const canvas = canvasRef.current;
    if (!canvas) return;
    if (!passRef.current) passRef.current = createOutputPass(canvas);
    const pass = passRef.current;
    // No pass, no MilkDrop: an unguarded preset is what ADR 0091 refuses.
    if (!pass) return;
    let td: Uint8Array<ArrayBuffer> = new Uint8Array(0);
    const samples = new Uint8Array(BUTTERCHURN_SAMPLES);
    const levels = { timeByteArray: samples, timeByteArrayL: samples, timeByteArrayR: samples };
    let last = -1;
    let id = 0;

    const frame = (now: number) => {
      id = requestAnimationFrame(frame);
      const dt = last < 0 ? 1 / 60 : Math.min(0.1, (now - last) / 1000);
      last = now;
      const node = analyserRef.current();
      if (!node) return;
      // The engine is made in the background (above); until it has the
      // wanted preset, the stage says it is loading.
      const e = engineRef.current;
      if (!e || !readyRef.current) return;

      const box = canvas.getBoundingClientRect();
      const { w, h } = trailSize(box.width, box.height, window.devicePixelRatio);
      if (e.w !== w || e.h !== h) {
        e.out.width = w;
        e.out.height = h;
        e.viz.setRendererSize(w, h, { pixelRatio: 1, textureRatio: 1 });
        e.w = w;
        e.h = h;
      }
      if (td.length !== node.fftSize) td = new Uint8Array(node.fftSize);
      node.getByteTimeDomainData(td);
      newestSamples(td, samples);
      try {
        e.viz.render({ audioLevels: levels, elapsedTime: dt });
      } catch (err) {
        // A trap inside the preset's equations: that preset goes, the stage
        // stays. The frame it was drawing never reaches the pass.
        const bad = presetRef.current;
        console.warn("milkdrop: preset failed while running, skipping it", bad, err);
        readyRef.current = false;
        e.loaded = "";
        dropPreset(bad, idsRef.current);
        return;
      }
      pass.frame(e.out, w, h, dt, limitRate(calmRef.current));
    };
    id = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(id);
  }, [running, analyserRef]);

  /*
   * The words sit on the stage, in the canvas's own box, so they land in the
   * square on a phone, beside the column on a wide screen and in the middle of
   * the full-screen visualizer alike. The provider's layer is aria-hidden and
   * takes no clicks: the same words are said, with a way back to the cover,
   * under the stage (VizStep in NowPlaying.tsx).
   */
  const words = milkdropStatusText(status);
  return (
    <div className="playback__viz-box">
      <canvas ref={canvasRef} className="playback__viz-canvas" data-viz="milkdrop" aria-hidden="true" />
      {words && (
        <p className="playback__viz-note" data-status={status}>
          {words}
        </p>
      )}
    </div>
  );
}
