/*
 * The media element's audio, routed through Web Audio once it has to be
 * (docs/audio-pass-plan.md, Phase 2).
 *
 * Three traps the plan names, and where each is handled:
 *
 * - createMediaElementSource works once per element, for ever. So the engine
 *   is made on first need, kept per element, and never rebuilt: turning every
 *   control off re-routes the graph to a straight wire (FXGraph.set) rather
 *   than tearing anything down.
 *
 * - Once the element feeds a context, the element's own setSinkId routes
 *   nothing: the sound leaves through the context. So the context's sink is set
 *   too, and the output picker keeps working whichever path is live.
 *
 * - A context starts suspended until the page has been interacted with. It is
 *   resumed whenever a control engages and whenever playback starts.
 *
 * And the one this design adds: the element is shared with films in a browser
 * tab, and an element once routed stays routed. The context's output is
 * therefore opened to every channel the device has, so a 5.1 film after an
 * evening of music is not folded to stereo by a graph that is doing nothing.
 */
import { buildGraph, fxActive, type ElementFX, type FXGraph } from "./elementAudio";

type SinkContext = AudioContext & { setSinkId?: (id: string) => Promise<void> };

export interface ElementEngine {
  ctx: SinkContext;
  graph: FXGraph;
}

// Per element, because createMediaElementSource can be called once per element.
const engines = new WeakMap<HTMLMediaElement, ElementEngine>();

/** The engine already attached to el, if one ever was. */
export function engineFor(el: HTMLMediaElement): ElementEngine | undefined {
  return engines.get(el);
}

/** Whether this engine can route an element through Web Audio at all. */
export function elementFXSupported(): boolean {
  return typeof window !== "undefined" && typeof window.AudioContext === "function";
}

/*
 * applyElementFX routes el for fx (already passed through fxApplies) and
 * returns the engine, or undefined when nothing is engaged and nothing ever
 * was: an element that has never been asked for an effect is left exactly as
 * the browser plays it.
 */
export function applyElementFX(
  el: HTMLMediaElement,
  fx: ElementFX,
  sinkId: string,
): ElementEngine | undefined {
  let engine = engines.get(el);
  if (!engine) {
    if (!fxActive(fx) || !elementFXSupported()) return undefined;
    const ctx: SinkContext = new AudioContext();
    const source = ctx.createMediaElementSource(el);
    const graph = buildGraph(ctx);
    const dest = ctx.destination;
    dest.channelCount = Math.max(dest.channelCount, dest.maxChannelCount);
    source.connect(graph.input);
    graph.output.connect(dest);
    engine = { ctx, graph };
    engines.set(el, engine);
    setContextSink(engine, sinkId);
  }
  engine.graph.set(fx);
  if (fxActive(fx)) void resume(engine);
  return engine;
}

/** Points the context's output at the chosen device, when the engine can. */
export function setContextSink(engine: ElementEngine, sinkId: string) {
  engine.ctx.setSinkId?.(sinkId).catch(() => {
    // A device unplugged since it was chosen: the context stays on the
    // default, the same fallback the element's own sink takes.
  });
}

/** Resumes a suspended context. Harmless when it is already running. */
export function resume(engine: ElementEngine): Promise<void> {
  if (engine.ctx.state !== "suspended") return Promise.resolve();
  return engine.ctx.resume().catch(() => {
    // Refused without a user gesture; the next play is one.
  });
}
