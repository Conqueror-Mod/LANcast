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
  /** Decibels per EQ_BANDS band; absent or all zero is flat (Phase 3). */
  eq?: number[];
}

/*
 * The equaliser (docs/audio-pass-plan.md, Phase 3; music only, decided
 * 2026-10-09). Five bands: shelves at the ends, so "more bass" lifts
 * everything below rather than a hump at one frequency, and peaking bands
 * an octave-and-more apart in between, at Q 1 so neighbours overlap smoothly.
 */
export const EQ_BANDS: { hz: number; type: BiquadFilterType; label: string }[] = [
  { hz: 60, type: "lowshelf", label: "60 Hz" },
  { hz: 230, type: "peaking", label: "230 Hz" },
  { hz: 910, type: "peaking", label: "910 Hz" },
  { hz: 3600, type: "peaking", label: "3.6 kHz" },
  { hz: 14000, type: "highshelf", label: "14 kHz" },
];
export const EQ_Q = 1;
export const EQ_LIMIT_DB = 12;
export const EQ_FLAT = [0, 0, 0, 0, 0];

/*
 * Starting points, not measurements: a preset is a shape somebody asked for
 * by name, and the sliders are there to correct it by ear.
 */
export const EQ_PRESETS: { id: string; label: string; gains: number[] }[] = [
  { id: "flat", label: "Flat", gains: EQ_FLAT },
  { id: "bass", label: "Bass boost", gains: [6, 3, 0, 0, 0] },
  // Retuned after the first listening test (2026-10-09): "not enough
  // difference from bass boost" (treble) and "could use adjusting" (vocal).
  { id: "treble", label: "Treble boost", gains: [0, 0, 1, 4, 7] },
  { id: "vocal", label: "Vocal presence", gains: [-3, -1, 3, 5, 1] },
  // Ears lose the extremes first as the level drops (equal-loudness curves),
  // so quiet listening gets both ends back.
  { id: "loudness", label: "Loud at low volume", gains: [6, 2, 0, 1, 5] },
];

/** Whether an equaliser setting changes anything at all. */
export function eqActive(eq?: number[]): boolean {
  return !!eq && eq.some((g) => Math.abs(g) >= 0.05);
}

export function clampGain(g: number): number {
  return Math.max(-EQ_LIMIT_DB, Math.min(EQ_LIMIT_DB, Number.isFinite(g) ? g : 0));
}

/*
 * The cut in front of the bands, so a boosted band cannot push a full-scale
 * track past 0 dBFS. Without night mode nothing downstream catches a peak,
 * and a +12 dB shelf on a mastered track would clip at once. Cuts need no
 * room, so only boosts count.
 *
 * **Weighted by where a track's peaks are.** The first version cut by the
 * largest boost, whatever band it was in. Music carries most of its energy,
 * and so its peaks, low down, and falls away with frequency, so a +6 dB
 * treble shelf adds little to a peak while a +6 dB bass shelf adds nearly all
 * of it. Cutting 6 dB for both left the treble preset back where it started
 * and everything below it 6 dB down: heard as "not enough difference between
 * it and bass boost" (2026-10-09). Each band's boost now counts for its share
 * of the peak (EQ_PEAK_SHARE), an estimate from that spectral tilt rather
 * than a measurement of any one track.
 */
export const EQ_PEAK_SHARE = [1, 1, 0.75, 0.5, 0.35];

export function eqPreampGain(eq?: number[]): number {
  const boost = Math.max(0, ...(eq ?? []).map((g, i) => clampGain(g) * (EQ_PEAK_SHARE[i] ?? 1)));
  return Math.pow(10, -boost / 20);
}

export const FX_OFF: ElementFX = { night: false };

/**
 * Whether night mode can do anything for a source with this many channels:
 * mono or stereo, because a DynamicsCompressorNode carries at most two
 * channels and would fold a 5.1 source to stereo without saying so. Zero means
 * not known yet; nothing engages on a guess.
 */
export function fxApplies(fx: ElementFX, channels: number): ElementFX {
  // The equaliser is filters, which carry any channel count unchanged; only
  // the compressor folds, so only night mode is held to mono or stereo.
  return { night: fx.night && channels >= 1 && channels <= 2, eq: fx.eq };
}

export function fxActive(fx: ElementFX): boolean {
  return fx.night || eqActive(fx.eq);
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

  /*
   * The equaliser: a pre-cut and five filters in a fixed chain, made the first
   * time somebody asks for it, so a graph nobody equalises is exactly the
   * graph it was before Phase 3. It sits before night mode, so night mode
   * evens out the shape that was asked for rather than the other way round.
   */
  let eq: { pre: GainNode; bands: BiquadFilterNode[] } | null = null;
  const ensureEq = () => {
    if (eq) return eq;
    const pre = ctx.createGain();
    pre.channelCountMode = "max";
    const bands = EQ_BANDS.map((b) => {
      const f = ctx.createBiquadFilter();
      f.type = b.type;
      f.frequency.value = b.hz;
      f.Q.value = EQ_Q;
      f.channelCountMode = "max";
      return f;
    });
    let prev: AudioNode = pre;
    for (const f of bands) {
      prev.connect(f);
      prev = f;
    }
    eq = { pre, bands };
    return eq;
  };

  const set = (fx: ElementFX) => {
    for (const n of [input, ceiling]) n.disconnect();
    if (eq) eq.bands[eq.bands.length - 1].disconnect();
    let head: AudioNode = input;
    if (eqActive(fx.eq)) {
      const e = ensureEq();
      e.pre.gain.value = eqPreampGain(fx.eq);
      e.bands.forEach((f, i) => (f.gain.value = clampGain(fx.eq?.[i] ?? 0)));
      head.connect(e.pre);
      head = e.bands[e.bands.length - 1];
    }
    if (fx.night) {
      head.connect(comp);
      ceiling.connect(output);
    } else {
      head.connect(output);
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
