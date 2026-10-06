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
 * tab, and an element once routed stays routed. The context's output follows
 * whatever is playing (outputChannels), so a 5.1 film after an evening of music
 * is not folded to stereo by a graph that is doing nothing.
 *
 * And the volume moves. The element's own volume scales the sound *before*
 * the graph, so a slider at 10% fed the compressor a signal 20 dB down. That
 * sits under the threshold, gets almost no compression and all of the node's
 * automatic make-up gain, and comes out louder: measured live, night mode was
 * 9 dB quieter at full volume, level at 25%, and 4 dB louder at 10%. Every lab
 * measurement had been taken at full volume. So once the element is routed it
 * plays at full volume and the slider is a gain after the graph (`level`), and
 * the compressor always sees the track at its real level.
 *
 * It follows the source, not the device. The first version opened the output
 * to the device's full channel count, and on a machine whose output is a
 * virtual 7.1 device (SteelSeries Sonar reports 8 channels) every stereo track
 * went out as a 7.1 stream once night mode had been used. The element on its
 * own sends stereo, so night mode changed what reached the mixer as well as the
 * level, and the owner heard night mode as louder than off on two releases
 * running while every measurement taken before the destination said it was
 * 9 dB quieter.
 */
import { buildGraph, fxActive, type ElementFX, type FXGraph } from "./elementAudio";

type SinkContext = AudioContext & { setSinkId?: (id: string) => Promise<void> };

export interface ElementEngine {
  ctx: SinkContext;
  graph: FXGraph;
  /** The player's volume, applied after the graph. */
  level: GainNode;
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
  sourceChannels: number,
): ElementEngine | undefined {
  let engine = engines.get(el);
  if (!engine) {
    if (!fxActive(fx) || !elementFXSupported()) return undefined;
    const ctx: SinkContext = new AudioContext();
    const source = ctx.createMediaElementSource(el);
    const graph = buildGraph(ctx);
    const level = ctx.createGain();
    level.channelCountMode = "max";
    // The slider's current value moves from the element to the gain.
    level.gain.value = el.volume;
    el.volume = 1;
    source.connect(graph.input);
    graph.output.connect(level);
    level.connect(ctx.destination);
    engine = { ctx, graph, level };
    engines.set(el, engine);
    setContextSink(engine, sinkId);
  }
  const dest = engine.ctx.destination;
  dest.channelCount = outputChannels(sourceChannels, dest.maxChannelCount);
  engine.graph.set(fx);
  if (fxActive(fx)) void resume(engine);
  return engine;
}

/**
 * How many channels the routed output carries: as many as the source has, at
 * least two (the element's own stereo for mono), and no more than the device
 * takes. Zero means the probe has not said, and stereo is what the element
 * would have sent.
 */
export function outputChannels(sourceChannels: number, deviceMax: number): number {
  const want = Number.isFinite(sourceChannels) && sourceChannels > 2 ? Math.floor(sourceChannels) : 2;
  return Math.max(1, Math.min(want, deviceMax || 2));
}

/**
 * Sets the player's volume on el: on the element itself until it is routed,
 * and after the graph from then on, with the element held at full volume so
 * night mode's compressor sees the track at its real level.
 */
export function setElementVolume(el: HTMLMediaElement, volume: number) {
  const engine = engines.get(el);
  if (!engine) {
    el.volume = volume;
    return;
  }
  el.volume = 1;
  engine.level.gain.value = volume;
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
