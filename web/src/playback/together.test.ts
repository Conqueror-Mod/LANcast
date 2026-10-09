/*
 * The two pure rules synchronisation rests on.
 *
 * Everything else in together.ts is polling and HTTP, which the server's own
 * tests cover. These two decide whether the picture is in step, and both are
 * the kind of arithmetic that looks obviously right and is off by one interval
 * forever.
 */
import { describe, it, expect } from "vitest";
import {
  expectedPosition,
  shouldResync,
  followerShouldSeek,
  followerSeekTarget,
  CONVERTING_TOLERANCE_MS,
} from "./together";

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

/*
 * A follower of a room on another server, on a converted stream.
 *
 * A seek there restarts the far server's conversion, and for several seconds
 * the clock reads the new start while nothing plays. Under the ordinary
 * tolerance every poll in those seconds would seek again, restarting it each
 * time, and the film would never begin.
 */
describe("when a follower in another household's room seeks", () => {
  it("seeks a direct file at the ordinary tolerance", () => {
    expect(followerShouldSeek(60_000, 64_000, false, 10_000)).toBe(true);
    expect(followerShouldSeek(60_000, 61_000, false, 10_000)).toBe(false);
  });

  it("leaves a converted stream's few seconds of drift alone", () => {
    expect(followerShouldSeek(60_000, 64_000, true, 60_000)).toBe(false);
    expect(followerShouldSeek(60_000, 90_000, true, 60_000)).toBe(true);
  });

  it("does not seek again before the last seek has landed", () => {
    expect(followerShouldSeek(0, 600_000, true, 3_000)).toBe(false);
    expect(followerShouldSeek(0, 600_000, false, 1_000)).toBe(false);
  });
});

/*
 * Joining a room whose film is converted for you, on a far server slow to
 * start: found 2026-10-09, when the conversion restarted at 94 s, 104 s and
 * 114 s with nothing ever served, because each start took longer than the
 * settle and every poll found the follower behind.
 *
 * A small simulation of exactly that: the host plays on, the follower polls
 * the room every two seconds, and a converted stream plays nothing for
 * `startup` after each seek, then runs from where it was asked to start.
 */
function simulate(startupMS: number, rules: "old" | "new") {
  let now = 0;
  const host = (t: number) => 94_000 + t; // the host's film position
  let local = 0;
  let lastSeek = 0;
  let seekAt = -Infinity;
  let startedSinceSeek = true;
  let measured = 0;
  let seeks = 0;
  for (now = 0; now <= 90_000; now += 2000) {
    // The stream: nothing until `startup` after a seek, then it runs.
    if (!startedSinceSeek && now - seekAt >= startupMS) {
      startedSinceSeek = true;
      measured = now - seekAt;
    }
    const elapsedSincePlay = startedSinceSeek ? now - (seekAt + startupMS) : 0;
    const at = seeks === 0 ? 0 : local + Math.max(0, elapsedSincePlay);
    const should =
      rules === "new"
        ? followerShouldSeek(at, host(now), true, now - lastSeek, startedSinceSeek)
        : followerShouldSeek(at, host(now), true, now - lastSeek);
    if (seeks === 0 || should) {
      seeks++;
      lastSeek = now;
      seekAt = now;
      startedSinceSeek = false;
      local = rules === "new" ? followerSeekTarget(host(now), true, measured) : host(now);
    }
  }
  const finalAt = local + Math.max(0, now - 2000 - (seekAt + startupMS));
  return { seeks, drift: Math.abs(host(now - 2000) - finalAt) };
}

describe("joining a converted room on a slow server", () => {
  it("settles within two seeks, inside the tolerance, when a start takes twelve seconds", () => {
    const r = simulate(12_000, "new");
    expect(r.seeks).toBeLessThanOrEqual(2);
    expect(r.drift).toBeLessThanOrEqual(CONVERTING_TOLERANCE_MS);
  });

  it("is the loop it replaces, under the old rule", () => {
    // The positive control: the same far server, the old rule, restarting
    // over and over as the log showed.
    expect(simulate(12_000, "old").seeks).toBeGreaterThanOrEqual(5);
  });

  it("does not move a converted stream again until it has played", () => {
    expect(followerShouldSeek(0, 600_000, true, 12_000, false)).toBe(false);
    expect(followerShouldSeek(0, 600_000, true, 12_000, true)).toBe(true);
    // A conversion that never starts is retried in the end.
    expect(followerShouldSeek(0, 600_000, true, 31_000, false)).toBe(true);
    // A direct file is never held back by it.
    expect(followerShouldSeek(60_000, 64_000, false, 10_000, false)).toBe(true);
  });

  it("aims ahead by the measured start on a converted stream only", () => {
    expect(followerSeekTarget(100_000, true, 12_000)).toBe(112_000);
    expect(followerSeekTarget(100_000, false, 12_000)).toBe(100_000);
    expect(followerSeekTarget(100_000, true, 90_000)).toBe(120_000);
  });
});
