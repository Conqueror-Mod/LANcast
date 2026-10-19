import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { Preset, Visualizer as Butterchurn } from "butterchurn";
import { fetchMilkdropPreset } from "@/api/hooks";
import { loadButterchurn } from "./loadButterchurn";
import { createOutputPass, type OutputPass } from "./milkdropPass";
import { limitRate } from "./limiterMath";
import { dropPreset } from "./milkdropStage";
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

function engineFor(ctx: BaseAudioContext): Promise<Engine> {
  let e = engines.get(ctx);
  if (!e) {
    e = loadButterchurn().then((bc) => {
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

  useEffect(
    () => () => {
      passRef.current?.dispose();
      passRef.current = null;
    },
    [],
  );

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
    if (e.loaded === id) {
      readyRef.current = true;
      return;
    }
    const token = ++loadToken.current;
    const blend = e.loaded && readyRef.current ? BLEND_SECONDS : 0;
    fetchMilkdropPreset(qc, id)
      .then((p) => e.viz.loadPreset(p.preset as unknown as Preset, blend))
      .then(() => {
        if (token !== loadToken.current) return;
        e.loaded = id;
        readyRef.current = true;
      })
      .catch((err) => {
        if (token !== loadToken.current) return;
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
    let asking = false;
    let live = true;
    let last = -1;
    let id = 0;

    const frame = (now: number) => {
      id = requestAnimationFrame(frame);
      const dt = last < 0 ? 1 / 60 : Math.min(0.1, (now - last) / 1000);
      last = now;
      const node = analyserRef.current();
      if (!node) return;
      const e = engineRef.current;
      if (!e) {
        // The engine needs the music's AudioContext, so it is made on the
        // first frame that has one, and loads in the background.
        if (!asking) {
          asking = true;
          engineFor(node.context)
            .then((made) => {
              if (!live) return;
              engineRef.current = made;
              loadRef.current(made);
            })
            .catch((err) => console.warn("milkdrop: butterchurn did not load", err));
        }
        return;
      }
      if (!readyRef.current) return;

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
    return () => {
      live = false;
      cancelAnimationFrame(id);
    };
  }, [running, analyserRef]);

  return <canvas ref={canvasRef} className="playback__viz-canvas" data-viz="milkdrop" aria-hidden="true" />;
}
