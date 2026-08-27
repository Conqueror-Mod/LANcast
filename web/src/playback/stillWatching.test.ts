import { describe, it, expect } from "vitest";
import {
  NO_RUN,
  UNATTENDED_ITEMS,
  UNATTENDED_MS,
  advanced,
  attended,
  describeRun,
  shouldAsk,
} from "./stillWatching";

const T0 = 1_700_000_000_000;

/** A run of n automatic advances starting at T0. */
function run(n: number) {
  let r = NO_RUN;
  for (let i = 0; i < n; i++) r = advanced(r, T0);
  return r;
}

describe("what counts as unattended", () => {
  it("says nothing while there is no run at all", () => {
    expect(shouldAsk(NO_RUN, T0 + UNATTENDED_MS * 10)).toBe(false);
  });

  /*
   * The case this exists to protect: somebody watching a long film properly.
   * They chose it, they touch nothing for two hours, and interrupting them is
   * the failure mode that teaches people to hate this feature.
   */
  it("never asks the person watching one long thing they chose", () => {
    expect(shouldAsk(NO_RUN, T0 + 3 * UNATTENDED_MS)).toBe(false);
    expect(shouldAsk(advanced(NO_RUN, T0), T0 + 3 * UNATTENDED_MS)).toBe(false);
  });

  it("needs the count and the clock, not either alone", () => {
    // Enough things, not enough time — three cartoons back to back is an
    // evening, not an absence.
    expect(shouldAsk(run(UNATTENDED_ITEMS), T0 + 60_000)).toBe(false);
    // Enough time, not enough things — one long film that auto-started.
    expect(shouldAsk(run(1), T0 + UNATTENDED_MS * 2)).toBe(false);
    // Both.
    expect(shouldAsk(run(UNATTENDED_ITEMS), T0 + UNATTENDED_MS)).toBe(true);
  });

  it("counts from when the run began, not from the last advance", () => {
    let r = NO_RUN;
    r = advanced(r, T0);
    r = advanced(r, T0 + UNATTENDED_MS - 1);
    r = advanced(r, T0 + UNATTENDED_MS);
    // The third advance happened at the two-hour mark, and the run started at
    // T0 — so the run is two hours old, which is the honest reading.
    expect(r.since).toBe(T0);
    expect(shouldAsk(r, T0 + UNATTENDED_MS)).toBe(true);
  });
});

describe("attention resets it", () => {
  it("forgets the run when somebody does something deliberate", () => {
    const r = run(UNATTENDED_ITEMS + 5);
    expect(shouldAsk(r, T0 + UNATTENDED_MS)).toBe(true);
    expect(shouldAsk(attended(), T0 + UNATTENDED_MS)).toBe(false);
  });

  /*
   * A person who answers the prompt has plainly not left, so answering must
   * buy a full new run rather than a few minutes of quiet. Otherwise the
   * prompt returns almost immediately and the feature becomes the nuisance it
   * was trying to prevent.
   */
  it("gives a full run back after the prompt is answered", () => {
    let r = attended();
    r = advanced(r, T0 + UNATTENDED_MS);
    expect(shouldAsk(r, T0 + UNATTENDED_MS * 2)).toBe(false);
  });
});

describe("what the prompt says", () => {
  it("states what the machine did rather than accusing the viewer", () => {
    const r = run(3);
    const text = describeRun(r, T0 + 2 * 3_600_000);
    expect(text).toContain("3 things have played automatically");
    expect(text).toContain("2 hours");
  });

  it("drops the duration when there is not a whole hour to report", () => {
    expect(describeRun(run(2), T0 + 60_000)).toBe(
      "2 things have played automatically.",
    );
  });

  it("gets the singular right", () => {
    expect(describeRun(run(2), T0 + 3_600_000)).toContain("1 hour.");
  });
});
