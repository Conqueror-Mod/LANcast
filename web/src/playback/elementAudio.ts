/*
 * The audio pass on the media element (docs/audio-pass-plan.md, Phase 2).
 *
 * Music plays through the <video> element everywhere, including the desktop
 * client, so its night mode and vocal lift run in Web Audio rather than in
 * mpv. Two controls, the same two Phase 1 gave films, built from nodes rather
 * than from a filter string.
 *
 * This file is the graph and nothing else: no element, no context of its own.
 * It takes any BaseAudioContext, so the same code that plays is the code that
 * was measured, rendered offline through an OfflineAudioContext and read back
 * with ffmpeg's ebur128.
 */

/** What the person has asked for, on music. */
export interface ElementFX {
  /** Compress the dynamic range: the quiet passages up, the loud ones down. */
  night: boolean;
  /** 0 off, 1 low, 2 high: how far the stereo sides are lowered under the centre. */
  vocals: number;
}

export const FX_OFF: ElementFX = { night: false, vocals: 0 };

/**
 * What each vocals level multiplies the side signal by. The same gains Phase 1
 * gives every surround channel but the centre (-6 dB, -9 dB), and for the same
 * reason: lowering what is around the voice cannot clip, raising the voice can.
 */
export const SIDE_GAIN = [1, 0.5, 0.35] as const;

/**
 * Whether a control can do anything for a source with this many channels.
 *
 * Vocals needs exactly two: in music the voice is mixed to the centre, which
 * in stereo is what both sides share. Night mode takes mono or stereo, because
 * a DynamicsCompressorNode carries at most two channels and would fold a 5.1
 * source to stereo without saying so. Zero means not known yet; nothing
 * engages on a guess.
 */
export function fxApplies(fx: ElementFX, channels: number): ElementFX {
  return {
    night: fx.night && channels >= 1 && channels <= 2,
    vocals: channels === 2 ? clampLevel(fx.vocals) : 0,
  };
}

export function fxActive(fx: ElementFX): boolean {
  return fx.night || fx.vocals > 0;
}

function clampLevel(v: number): number {
  if (!Number.isFinite(v) || v <= 0) return 0;
  return v >= SIDE_GAIN.length - 1 ? SIDE_GAIN.length - 1 : Math.floor(v);
}

/*
 * Night mode's numbers, chosen by measurement (docs/audio-pass-plan.md, "Phase
 * 2, measured"). Gentler than the film's: Phase 1's -40 dB at 8:1 halved a
 * symphony's range well, and flattened an already-loud pop master to 1.1 LU, a
 * wall. -30 dB at 4:1 takes the symphony from 20.8 to 11.3 LU, keeps quiet
 * material within about a decibel of where it was, and brings the loud master
 * down 4 dB. The release is the longest the node allows (1 s); Phase 1 found
 * that a compressor that lets go between transients is heard as a volume
 * change and nothing else.
 */
export const NIGHT = {
  threshold: -30,
  knee: 10,
  ratio: 4,
  attack: 0.05,
  release: 1,
  /** Applied after the compressor, whose own make-up gain is automatic. */
  trimDb: 0,
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
 * The routing is the bypass. Every stage that is off is disconnected, so with
 * everything off the input feeds the output directly and nothing touches the
 * samples. Parameters set to "neutral" would not be: a compressor at ratio 1
 * still applies its automatic make-up gain.
 */
export function buildGraph(ctx: BaseAudioContext, night: NightParams = NIGHT): FXGraph {
  const input = ctx.createGain();
  const output = ctx.createGain();
  // Neither end may fold channels: a 5.1 film in a browser tab plays through
  // these when both controls are off. "max" passes whatever arrives.
  for (const n of [input, output]) {
    n.channelCountMode = "max";
  }

  // Vocals: a 2x2 matrix on the stereo pair. With mid M = (L+R)/2 and side
  // S = (L-R)/2, lowering S by g gives L' = M + gS = aL + bR, where
  // a = (1+g)/2 and b = (1-g)/2. L' is a weighted average of L and M, and M
  // lies between L and R, so no output sample exceeds the input's peak.
  const split = ctx.createChannelSplitter(2);
  const merge = ctx.createChannelMerger(2);
  const ll = ctx.createGain();
  const rl = ctx.createGain();
  const lr = ctx.createGain();
  const rr = ctx.createGain();
  split.connect(ll, 0);
  split.connect(rl, 1);
  split.connect(lr, 0);
  split.connect(rr, 1);
  ll.connect(merge, 0, 0);
  rl.connect(merge, 0, 0);
  lr.connect(merge, 0, 1);
  rr.connect(merge, 0, 1);

  // Night: the compressor, a trim, and a ceiling. The ceiling is a waveshaper,
  // not a second compressor, because DynamicsCompressorNode always adds
  // make-up gain: a "limiter" built from one makes everything louder.
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
    for (const n of [input, merge, ceiling]) n.disconnect();
    let at: AudioNode = input;
    if (fx.vocals > 0) {
      const g = SIDE_GAIN[clampLevel(fx.vocals)];
      ll.gain.value = rr.gain.value = (1 + g) / 2;
      rl.gain.value = lr.gain.value = (1 - g) / 2;
      at.connect(split);
      at = merge;
    }
    if (fx.night) {
      at.connect(comp);
      at = ceiling;
    }
    at.connect(output);
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
