/*
 * Where a stream restarts after the one before it died.
 *
 * The fault this file exists for, from a real evening: Dogma was paused
 * overnight, its transcode session was reaped after ten idle minutes, and on
 * waking the client asked for `seg00994.m4s` — segment 994 of six seconds, so
 * 1h39m — and in the same millisecond opened a new session at `start_at=736`,
 * twelve minutes. It then wrote 736 back as the saved position, destroying the
 * real one.
 *
 * The arithmetic below is the whole of it. `offset` is the *session's base* and
 * does not move while a film plays; the film's position is `offset +
 * currentTime`. Rebuilding from the base rather than from the sum is what threw
 * away ninety minutes, and it is invisible in any test that starts a stream at
 * zero — because there the two are the same number.
 */
import { describe, it, expect } from "vitest";
// The player's own rule, not a copy of it. A test that redeclared this would
// pass while the player did something else.
import { resumePointAfterFailure } from "./resumePoint";

describe("restarting after a session dies", () => {
  it("comes back where the film was, not where the stream started", () => {
    /*
     * Dogma, exactly. The session opened at 736s and the player had reached
     * 5964s — segment 994 — when the reaped session started refusing segments.
     */
    const at = resumePointAfterFailure({ id: 7021, at: 5964 }, 7021, 736);
    expect(at).toBe(5964);
  });

  it("falls back to the session base when nothing has played yet", () => {
    // A stream that failed before its first timeupdate has no live position,
    // and the base is the only thing known. Restarting there is right.
    const at = resumePointAfterFailure({ id: 0, at: 0 }, 7021, 736);
    expect(at).toBe(736);
  });

  it("ignores a live position belonging to a different item", () => {
    /*
     * The queue can move on while a stream is still failing. A position from
     * the previous film would restart this one in the middle of nowhere, and
     * on a shorter film past its end.
     */
    const at = resumePointAfterFailure({ id: 9999, at: 5964 }, 7021, 736);
    expect(at).toBe(736);
  });

  it("is the identity at the start of a film", () => {
    /*
     * Why this went unnoticed. A stream opened at zero has base 0, so the base
     * and the sum agree and the wrong one is indistinguishable from the right
     * one — every test that pressed play from the beginning passed.
     */
    expect(resumePointAfterFailure({ id: 1, at: 0 }, 1, 0)).toBe(0);
    expect(resumePointAfterFailure({ id: 1, at: 120 }, 1, 0)).toBe(120);
  });
});

/*
 * The second half of the same fault, which is the half that lost the data.
 *
 * After rebuilding, `offset` has to become the *new* stream's base. Left at the
 * old one, every later sum is wrong by the difference — the clock on screen,
 * the subtitle timing, and the position written back to the server. That last
 * one is how 736 came to overwrite a good position rather than merely being a
 * bad place to resume.
 */
describe("what the clock reads after a rebuild", () => {
  const displayTime = (base: number, currentTime: number) => base + currentTime;

  it("is right when the base was rebased", () => {
    // Rebuilt at 5964 and played five seconds.
    expect(displayTime(5964, 5)).toBe(5969);
  });

  it("is wrong by the whole gap when it was not", () => {
    /*
     * The bug, stated as the number somebody would see: the stream restarts at
     * 5964 but the base still says 736, so five seconds in the clock reads
     * 12m21s — and that is what gets saved.
     */
    expect(displayTime(736, 5)).toBe(741);
    expect(displayTime(736, 5)).not.toBe(5969);
  });
});
