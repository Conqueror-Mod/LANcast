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
  followerShouldCorrect,
  nextLead,
  CONVERTING_TOLERANCE_MS,
  CONVERTING_CORRECT_MS,
  CONVERTING_DEFAULT_LEAD_MS,
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
 * Joining a room whose film is converted for you, simulated.
 *
 * The host plays on; the follower polls the room every 500 ms. A converted
 * stream shows nothing for `startup` after a seek, then runs from `slop`
 * before where it was asked to start (an old poll, the keyframe it had to
 * begin at). Both cases come from one evening between two real servers:
 *
 * - 12 s starts, slop 0: the conversion restarted at 94, 104 and 114 s with
 *   nothing served (the "old" rule).
 * - 4 s starts, 4 s slop: after #807 the picture came in four seconds and
 *   sat eight seconds behind for the whole film (the "v0.9.71" rule).
 */
type Rules = "old" | "v0.9.71" | "new";

function simulate(startup: number, rules: Rules, slop = 0) {
  const host = (t: number) => 94_000 + t;
  const tick = 500;
  let local = 0;
  let lastSeek = -Infinity;
  let seekAt = -Infinity;
  let started = true;
  let playingSince = 0;
  let lead = rules === "new" ? CONVERTING_DEFAULT_LEAD_MS : 0;
  let leadUsed = 0;
  let measuredStartup = 0;
  let corrections = 0;
  let seeks = 0;
  const at = (now: number) => (started ? local + (now - playingSince) : local);
  let now = 0;
  for (now = 0; now <= 120_000; now += tick) {
    if (!started && now - seekAt >= startup) {
      started = true;
      playingSince = now;
      measuredStartup = now - seekAt;
      if (rules === "new") lead = nextLead(leadUsed, host(now) - local);
    }
    const local_ = at(now);
    const far =
      seeks === 0 ||
      (rules === "old"
        ? followerShouldSeek(local_, host(now), true, now - lastSeek)
        : followerShouldSeek(local_, host(now), true, now - lastSeek, started));
    const close =
      rules === "new" &&
      !far &&
      started &&
      followerShouldCorrect(local_, host(now), true, now - playingSince, corrections);
    if (far || close) {
      if (close) corrections++;
      seeks++;
      lastSeek = now;
      seekAt = now;
      started = false;
      leadUsed = rules === "new" ? lead : rules === "v0.9.71" ? measuredStartup : 0;
      local = followerSeekTarget(host(now), true, leadUsed) - slop;
    }
  }
  const end = now - tick;
  return { seeks, corrections, drift: Math.abs(host(end) - at(end)) };
}

describe("joining a converted room", () => {
  it("is the loop #807 replaced, under the old rule, when a start takes twelve seconds", () => {
    // The positive control for the first evening.
    expect(simulate(12_000, "old").seeks).toBeGreaterThanOrEqual(5);
  });

  it("starts once on a twelve-second start, and lands within the tolerance", () => {
    for (const rules of ["v0.9.71", "new"] as const) {
      const r = simulate(12_000, rules);
      expect(r.seeks).toBeLessThanOrEqual(3);
      expect(r.drift).toBeLessThanOrEqual(CONVERTING_TOLERANCE_MS);
    }
  });

  it("sat eight seconds behind under v0.9.71 when a four-second start landed late", () => {
    // The positive control for the second evening: never corrected, because
    // eight seconds is inside the converted tolerance.
    const r = simulate(4000, "v0.9.71", 4000);
    expect(r.drift).toBeGreaterThan(CONVERTING_CORRECT_MS);
  });

  it("closes that gap with at most two more seeks", () => {
    const r = simulate(4000, "new", 4000);
    expect(r.seeks).toBeLessThanOrEqual(3);
    expect(r.drift).toBeLessThanOrEqual(CONVERTING_CORRECT_MS);
  });

  it("stops correcting after two tries, whatever the landing", () => {
    // A far server whose streams land further behind than the lead may reach
    // (the cap is 20 s). The close-gap corrections stop at two; what remains
    // is the ordinary 8 s rule, unchanged from before.
    expect(simulate(4000, "new", 30_000).corrections).toBe(2);
  });
});

describe("the follow rules on their own", () => {
  it("does not move a converted stream again until it has played", () => {
    expect(followerShouldSeek(0, 600_000, true, 12_000, false)).toBe(false);
    expect(followerShouldSeek(0, 600_000, true, 12_000, true)).toBe(true);
    expect(followerShouldSeek(0, 600_000, true, 31_000, false)).toBe(true);
    expect(followerShouldSeek(60_000, 64_000, false, 10_000, false)).toBe(true);
  });

  it("aims ahead by the lead on a converted stream only, capped", () => {
    expect(followerSeekTarget(100_000, true, 12_000)).toBe(112_000);
    expect(followerSeekTarget(100_000, false, 12_000)).toBe(100_000);
    expect(followerSeekTarget(100_000, true, 90_000)).toBe(120_000);
  });

  it("learns the lead from where a start landed, either side", () => {
    expect(nextLead(4000, 4000)).toBe(8000);
    expect(nextLead(8000, -3000)).toBe(5000);
    expect(nextLead(1000, -5000)).toBe(0);
  });

  it("corrects a close gap only on a converted stream that has played a moment, twice at most", () => {
    expect(followerShouldCorrect(100_000, 108_000, true, 5000, 0)).toBe(true);
    expect(followerShouldCorrect(100_000, 102_000, true, 5000, 0)).toBe(false);
    expect(followerShouldCorrect(100_000, 108_000, true, 500, 0)).toBe(false);
    expect(followerShouldCorrect(100_000, 108_000, true, 5000, 2)).toBe(false);
    expect(followerShouldCorrect(100_000, 108_000, false, 5000, 0)).toBe(false);
  });
});
