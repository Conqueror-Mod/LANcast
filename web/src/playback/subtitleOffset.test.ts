/*
 * The subtitle offset shifts parsed cues in place, which makes it the one
 * control in the settings panel whose arithmetic can go wrong invisibly.
 *
 * Mutating in place means the shift has to be a *delta* against what has
 * already been applied. Shifting by the preference itself each time the effect
 * ran would compound: setting +2 and then re-rendering for an unrelated reason
 * would leave the cues at +4, then +6, and the subtitles would walk away from
 * the dialogue while the readout still said +2.00 s. Nothing about that is
 * visible in the control.
 *
 * The clamp is the other half. A cue with a negative start time is not a cue
 * that shows earlier — the engine drops it — so a large negative offset near
 * the beginning of a film would silently delete the opening lines rather than
 * moving them.
 */
import { describe, it, expect } from "vitest";

interface Cue {
  startTime: number;
  endTime: number;
}

/** The shift as the provider applies it: a delta, clamped at zero. */
function shift(cues: Cue[], want: number, applied: number): number {
  const delta = want - applied;
  if (delta === 0) return applied;
  for (const c of cues) {
    c.startTime = Math.max(0, c.startTime + delta);
    c.endTime = Math.max(0, c.endTime + delta);
  }
  return want;
}

const cues = (): Cue[] => [
  { startTime: 1, endTime: 3 },
  { startTime: 10, endTime: 12 },
];

describe("subtitle offset", () => {
  it("moves cues later for a positive offset", () => {
    const c = cues();
    shift(c, 2, 0);
    expect(c[1]).toEqual({ startTime: 12, endTime: 14 });
  });

  it("moves cues earlier for a negative offset", () => {
    // The direction the server's ShiftVTT cannot do, which is why this is
    // applied client-side at all.
    const c = cues();
    shift(c, -2, 0);
    expect(c[1]).toEqual({ startTime: 8, endTime: 10 });
  });

  it("does not compound when applied repeatedly at the same value", () => {
    const c = cues();
    let applied = shift(c, 2, 0);
    applied = shift(c, 2, applied);
    applied = shift(c, 2, applied);
    expect(c[1].startTime).toBe(12);
    expect(applied).toBe(2);
  });

  it("moves by the difference when the offset changes", () => {
    const c = cues();
    const applied = shift(c, 2, 0);
    shift(c, 5, applied);
    // 10 + 5, not 10 + 2 + 5.
    expect(c[1].startTime).toBe(15);
  });

  it("returns to where it started when the offset goes back to zero", () => {
    const c = cues();
    const applied = shift(c, 3.5, 0);
    shift(c, 0, applied);
    expect(c).toEqual(cues());
  });

  it("clamps at zero rather than producing a cue the engine drops", () => {
    const c = cues();
    shift(c, -5, 0);
    expect(c[0].startTime).toBe(0);
    expect(c[0].endTime).toBe(0);
  });

  // A remounted <track> parses fresh, unshifted cues, so the applied amount
  // resets with it. Without that reset the first shift after a track change
  // would be a delta against cues that had never been touched.
  it("re-applies in full against freshly parsed cues", () => {
    const c = cues();
    shift(c, 2, 0);
    const afterRemount = cues(); // new track element, unshifted
    shift(afterRemount, 2, 0); // applied reset to 0
    expect(afterRemount[1].startTime).toBe(12);
  });
});
