import { describe, it, expect } from "vitest";
import {
  NO_RUN,
  UNATTENDED_ITEMS,
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
    expect(shouldAsk(NO_RUN)).toBe(false);
  });

  /*
   * The case this exists to protect: somebody watching a long film properly.
   * They chose it, they touch nothing for two hours, and interrupting them is
   * the failure mode that teaches people to hate this feature.
   *
   * Nothing about the *duration* protects them now that the clock is gone —
   * what protects them is that they chose the thing, so there is no run.
   */
  it("never asks the person watching one long thing they chose", () => {
    expect(shouldAsk(NO_RUN)).toBe(false);
    expect(shouldAsk(advanced(NO_RUN, T0))).toBe(false);
  });

  /*
   * The rule that replaced "the count and the clock".
   *
   * The clock made this nearly unreachable: three episodes of an ordinary
   * drama clear three advances long before they clear two hours, so the count
   * was satisfied and the prompt never came. Three things nobody chose is the
   * signal, and how long they ran says nothing about whether anyone is there.
   */
  it("asks on the third automatic advance, however long they took", () => {
    expect(shouldAsk(run(UNATTENDED_ITEMS - 1))).toBe(false);
    expect(shouldAsk(run(UNATTENDED_ITEMS))).toBe(true);
  });

  it("asks after three short things as readily as three long ones", () => {
    // Eighteen minutes of cartoons and six hours of films are the same run.
    let quick = NO_RUN;
    for (let i = 0; i < 3; i++) quick = advanced(quick, T0 + i * 360_000);
    let slow = NO_RUN;
    for (let i = 0; i < 3; i++) slow = advanced(slow, T0 + i * 7_200_000);
    expect(shouldAsk(quick)).toBe(true);
    expect(shouldAsk(slow)).toBe(true);
  });

  it("still remembers when the run began, because the prompt says so", () => {
    let r = NO_RUN;
    r = advanced(r, T0);
    r = advanced(r, T0 + 3_600_000);
    // `since` no longer gates anything; it is only what describeRun reads.
    expect(r.since).toBe(T0);
  });
});

describe("attention resets it", () => {
  it("forgets the run when somebody does something deliberate", () => {
    const r = run(UNATTENDED_ITEMS + 5);
    expect(shouldAsk(r)).toBe(true);
    expect(shouldAsk(attended())).toBe(false);
  });

  /*
   * A person who answers the prompt has plainly not left, so answering must
   * buy a full new run rather than a few minutes of quiet. Otherwise the
   * prompt returns almost immediately and the feature becomes the nuisance it
   * was trying to prevent.
   */
  it("gives a full run back after the prompt is answered", () => {
    let r = attended();
    r = advanced(r, T0);
    expect(shouldAsk(r)).toBe(false);
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
