/*
 * The two pure rules synchronisation rests on.
 *
 * Everything else in together.ts is polling and HTTP, which the server's own
 * tests cover. These two decide whether the picture is in step, and both are
 * the kind of arithmetic that looks obviously right and is off by one interval
 * forever.
 */
import { describe, it, expect } from "vitest";
import { expectedPosition, shouldResync } from "./together";

const at = (positionMS: number, updatedAtSeconds: number, paused = false) => ({
  position_ms: positionMS,
  paused,
  updated_at: updatedAtSeconds,
});

// A session as a server with age_ms sends it. updated_at is deliberately
// nonsense: these tests must not depend on it.
const aged = (positionMS: number, ageMS: number) => ({
  position_ms: positionMS,
  paused: false,
  updated_at: 0,
  age_ms: ageMS,
});

describe("where the film should be now", () => {
  /*
   * The correction that makes following possible.
   *
   * A poll arrives with a position the host reported up to an interval ago.
   * Seeking to that number lands the follower permanently behind — it was
   * already stale when it was sent, and doing it again every two seconds never
   * closes the gap.
   */
  it("adds the server's age and the time since the answer arrived", () => {
    const received = 5_000;
    const now = 5_500;
    // The host reported 1.5 s before the server answered; half a second has
    // passed here since.
    expect(expectedPosition(aged(60_000, 1_500), received, now)).toBe(62_000);
  });

  /*
   * The clock trap, stated as a test.
   *
   * This device's clock is an hour wrong. With age_ms it does not matter,
   * because nothing compares it with the server's; the old arithmetic would
   * have put the film an hour out.
   */
  it("does not read this device's clock against the server's", () => {
    const hourWrong = 1_700_000_000_000 + 3_600_000;
    const session = { ...aged(60_000, 1_000), updated_at: 1_700_000_000 };
    expect(expectedPosition(session, hourWrong, hourWrong)).toBe(61_000);
  });

  // A paused film has not moved, however long ago that was said.
  it("does not advance a paused session", () => {
    expect(expectedPosition({ ...aged(60_000, 30_000), paused: true }, 0, 0)).toBe(60_000);
  });

  // A server from before age_ms: the old arithmetic still works.
  it("falls back to updated_at when the server sends no age", () => {
    const now = 1_700_000_002_000; // two seconds after the report
    expect(expectedPosition(at(60_000, 1_700_000_000), now, now)).toBe(62_000);
  });

  /*
   * Clocks between two machines are not the same clock.
   *
   * On the fallback path, a host timestamp ahead of this device makes the
   * elapsed time negative and the naive sum seeks *backwards* — on every
   * single poll, which presents as a film that will not play forwards.
   */
  it("refuses to run backwards when the clocks disagree", () => {
    const now = 1_699_999_995_000; // this device is behind the host
    expect(expectedPosition(at(60_000, 1_700_000_000), now, now)).toBe(60_000);
  });
});

describe("when a follower is worth correcting", () => {
  // Seeking is a visible stutter. Doing it every two seconds to fix a quarter
  // of a second nobody can perceive is worse than the drift it cures.
  it("leaves small drift alone", () => {
    expect(shouldResync(60_000, 60_400)).toBe(false);
    expect(shouldResync(60_000, 59_200)).toBe(false);
  });

  it("corrects drift people would notice", () => {
    expect(shouldResync(60_000, 64_000)).toBe(true);
    expect(shouldResync(64_000, 60_000)).toBe(true);
  });

  // Both directions: a follower who is *ahead* is as out of step as one behind,
  // and an absolute comparison is the only thing that catches a seek forwards.
  it("is symmetric", () => {
    expect(shouldResync(10_000, 20_000)).toBe(shouldResync(20_000, 10_000));
  });
});
