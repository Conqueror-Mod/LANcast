/*
 * The queue's start and end, tapered (Chris's notes, paired with gapless).
 *
 * A listening session fades in from silence when somebody presses play on
 * something, and the last track of the queue fades out over its final
 * seconds instead of stopping dead. Nothing in between is touched: a track
 * joining the next is gapless, and a pause is a pause.
 *
 * "Last" means nothing will follow — the end of the queue, or Auto play off —
 * and never repeat-one, which loops rather than ends. Pure, so the curve and
 * the rules are tested without an element.
 */
export const TAPER_IN_MS = 1500;
export const TAPER_OUT_S = 4;

export interface TaperState {
  /** Milliseconds since play was pressed on something, or null once past the fade. */
  msSinceStart: number | null;
  /** Seconds left in this track; Infinity when not known. */
  remainingS: number;
  /** Whether nothing will play after this track. */
  last: boolean;
}

// A quarter-sine ramp: gentle at the quiet end, where a linear one sounds
// like a step, and arriving at full level without a corner.
function ramp(t: number): number {
  return Math.sin((Math.min(1, Math.max(0, t)) * Math.PI) / 2);
}

export function taperGain(s: TaperState): number {
  let g = 1;
  if (s.msSinceStart !== null && s.msSinceStart < TAPER_IN_MS) g *= ramp(s.msSinceStart / TAPER_IN_MS);
  if (s.last && Number.isFinite(s.remainingS) && s.remainingS < TAPER_OUT_S) {
    g *= ramp(s.remainingS / TAPER_OUT_S);
  }
  return g;
}
