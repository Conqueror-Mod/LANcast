import { describe, it, expect } from "vitest";
import {
  struggling,
  MIN_SPAN_MS,
  MIN_FRAMES,
  DROP_LIMIT,
  type Sample,
} from "./decodeHealth";

/*
 * Deciding that a file is playing badly rather than not playing.
 *
 * Measured on the reporting install, direct play, same machine, minutes apart:
 * two HEVC Main 10 films dropped 19.8% and 19.9% of their frames; an H.264 film
 * from the same folder dropped none. Reported as heavy frame lag.
 *
 * Nothing could have predicted it. `canPlayType` answers "probably" for HEVC
 * Main 10, and `mediaCapabilities.decodingInfo()` — whose entire job is to
 * answer *will this be smooth* — returned smooth **and** power-efficient for
 * the exact resolution, rate and bitrate of the file dropping a fifth of its
 * frames. So the only honest signal is the playback, and these are the rules
 * for reading it.
 */

const s = (at: number, decoded: number, dropped: number): Sample => ({
  at,
  decoded,
  dropped,
});

describe("struggling", () => {
  // The measured bad case, near enough: ~24fps for 12 seconds, a fifth lost.
  it("catches the case this was built for", () => {
    expect(struggling(s(0, 0, 0), s(12_000, 288, 57))).toBe(true);
  });

  // And the measured good case, from the same shelf on the same machine.
  it("leaves a file that drops nothing alone", () => {
    expect(struggling(s(0, 0, 0), s(12_000, 288, 0))).toBe(false);
  });

  /*
   * A burst is not a verdict.
   *
   * Seeks, resizes and the first moments of a codec change all drop frames
   * legitimately. Without a floor on the span, any one of them would withdraw a
   * codec claim for a fortnight on the strength of a hiccup.
   */
  it("will not decide from a window too short to mean anything", () => {
    const brief = MIN_SPAN_MS - 1;
    expect(struggling(s(0, 0, 0), s(brief, 400, 200))).toBe(false);
  });

  /*
   * Nor from too few frames, which is what a barely-running element produces.
   * Ten seconds that yielded thirty frames describes a stall, and a stall is
   * not evidence about a decoder.
   */
  it("will not decide from too few frames, however bad they look", () => {
    expect(struggling(s(0, 0, 0), s(MIN_SPAN_MS + 5_000, MIN_FRAMES - 1, 60))).toBe(
      false,
    );
  });

  it("needs the drop rate to actually reach the limit", () => {
    const under = Math.floor(300 * DROP_LIMIT) - 1;
    expect(struggling(s(0, 0, 0), s(20_000, 300, under))).toBe(false);
    expect(struggling(s(0, 0, 0), s(20_000, 300, Math.ceil(300 * DROP_LIMIT)))).toBe(
      true,
    );
  });

  /*
   * The counters are cumulative for the life of the element, so only the
   * *difference* describes now.
   *
   * This is the case that matters for a long film: a bad first minute must not
   * keep condemning a file that recovered, and — the direction that actually
   * bites — a file that goes bad an hour in must still be caught, which a
   * lifetime average would bury under an hour of good frames.
   */
  it("reads the gap between two points, not the life of the element", () => {
    // Terrible early, fine since: 5000 dropped in the past, none in this window.
    const early = s(0, 100_000, 5_000);
    const now = s(20_000, 100_480, 5_000);
    expect(struggling(early, now)).toBe(false);

    // Fine for an hour, terrible now.
    const good = s(0, 100_000, 10);
    const bad = s(20_000, 100_480, 130);
    expect(struggling(good, bad)).toBe(true);
  });

  it("treats a counter that did not move as nothing to say", () => {
    expect(struggling(s(0, 500, 20), s(30_000, 500, 20))).toBe(false);
  });
});
