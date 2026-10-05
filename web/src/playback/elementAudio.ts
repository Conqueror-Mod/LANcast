/*
 * The audio pass on the media element (docs/audio-pass-plan.md, Phase 2).
 *
 * Music plays through the <video> element everywhere, including the desktop
 * client, so its night mode runs in Web Audio rather than in mpv, built from
 * nodes rather than from a filter string.
 *
 * Night mode only. A vocals control shipped beside it in v0.9.57, lowering the
 * stereo sides under the centre, and the listening test found no discernible
 * difference at either level, and a track that sounded better without it. The
 * measurement agrees with the ear: in a modern mix the bass, drums and lead
 * instruments share the centre with the voice, and the sides were already
 * 9 dB under it, so lowering them only narrowed the stereo image. Lifting a
 * voice needs an equaliser (Phase 3) or real centre extraction, not this.
 *
 * This file is the graph and nothing else: no element, no context of its own.
 * It takes any BaseAudioContext, so the same code that plays is the code that
 * was measured, rendered offline through an OfflineAudioContext and read back
 * with ffmpeg's ebur128.
 */

/** What the person has asked for, on music. */
export interface ElementFX {
  /** Compress the dynamic range, and sit lower: night listening. */
  night: boolean;
}

export const FX_OFF: ElementFX = { night: false };

/**
 * Whether night mode can do anything for a source with this many channels:
 * mono or stereo, because a DynamicsCompressorNode carries at most two
 * channels and would fold a 5.1 source to stereo without saying so. Zero means
 * not known yet; nothing engages on a guess.
 */
export function fxApplies(fx: ElementFX, channels: number): ElementFX {
  return { night: fx.night && channels >= 1 && channels <= 2 };
}

export function fxActive(fx: ElementFX): boolean {
  return fx.night;
}

/*
 * Night mode's numbers, chosen by measurement (docs/audio-pass-plan.md, "Phase
 * 2, measured").
 *
 * The compressor is gentler than the film's: Phase 1's -40 dB at 8:1 flattened
 * an already-loud pop master to 1.1 LU, a wall. -30 dB at 4:1 takes a symphony
 * from 20.8 to 11.3 LU. The release is the longest the node allows (1 s);
 * Phase 1 found that a compressor that lets go between transients is heard as a
 * volume change and nothing else.
 *
 * The trim is the second listening test's correction. DynamicsCompressorNode
 * adds make-up gain of its own, about +13 dB at these settings, so with no trim
 * every track came out between -13 and -18 LUFS: even, but at ordinary
 * listening level, and a rock track was "quite loud" for a mode called night.
 * The trim puts that spread around -20 LUFS instead.
 */
export const NIGHT = {
  threshold: -30,
  knee: 10,
  ratio: 4,
  attack: 0.05,
  release: 1,
  /** Applied after the compressor, whose own make-up gain is automatic. */
  trimDb: -5,
  /** Where the ceiling starts to bend, and the level it never passes, linear. */
  knee2: 0.5,
  ceiling: 0.7,
};

export type NightParams = typeof NIGHT;

/** The nodes, wired between an input and an output, re-routed by set(). */
export interface FXGraph {
  input: AudioNode;
  output: AudioNode;
  /** Re-route for fx, which the caller has already passed through fxApplies. */
  set(fx: ElementFX): void;
}

/*
 * buildGraph makes the nodes once and re-routes them, never rebuilds them.
 *
 * The routing is the bypass. With night mode off the input feeds the output
 * directly and nothing touches the samples. Parameters set to "neutral" would
 * not be: a compressor at ratio 1 still applies its automatic make-up gain.
 */
export function buildGraph(ctx: BaseAudioContext, night: NightParams = NIGHT): FXGraph {
  const input = ctx.createGain();
  const output = ctx.createGain();
  // Neither end may fold channels: a 5.1 film in a browser tab plays through
  // these when night mode is off. "max" passes whatever arrives.
  for (const n of [input, output]) {
    n.channelCountMode = "max";
  }

  // The compressor, a trim, and a ceiling. The ceiling is a waveshaper, not a
  // second compressor, because DynamicsCompressorNode always adds make-up gain:
  // a "limiter" built from one makes everything louder.
  const comp = ctx.createDynamicsCompressor();
  comp.threshold.value = night.threshold;
  comp.knee.value = night.knee;
  comp.ratio.value = night.ratio;
  comp.attack.value = night.attack;
  comp.release.value = night.release;
  const trim = ctx.createGain();
  trim.gain.value = Math.pow(10, night.trimDb / 20);
  const ceiling = ctx.createWaveShaper();
  ceiling.curve = softCeiling(night.knee2, night.ceiling);
  ceiling.oversample = "4x";
  comp.connect(trim);
  trim.connect(ceiling);

  const set = (fx: ElementFX) => {
    for (const n of [input, ceiling]) n.disconnect();
    if (fx.night) {
      input.connect(comp);
      ceiling.connect(output);
    } else {
      input.connect(output);
    }
  };
  set(FX_OFF);
  return { input, output, set };
}

/*
 * softCeiling is a transfer curve that is exactly linear below `knee` and
 * bends smoothly towards `max` above it, so quiet material passes untouched
 * and only the peaks the compressor let through are rounded off. Odd-symmetric,
 * so it adds no offset. A waveshaper clamps input beyond its curve to the
 * curve's ends, so `max` is a hard ceiling on every sample.
 */
export function softCeiling(knee: number, max: number, size = 4096): Float32Array<ArrayBuffer> {
  const curve = new Float32Array(size);
  const room = max - knee;
  for (let i = 0; i < size; i++) {
    const x = (i / (size - 1)) * 2 - 1;
    const a = Math.abs(x);
    const y = a <= knee ? a : knee + room * Math.tanh((a - knee) / room);
    curve[i] = Math.sign(x) * y;
  }
  return curve;
}
