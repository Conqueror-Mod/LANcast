import { describe, it, expect, beforeEach } from "vitest";
import {
  filePath,
  hlsWorthTrying,
  hlsVerdict,
  rememberHLS,
  isUnsupportedSource,
  HLS_VERDICT_KEY,
} from "./fileTransport";

/*
 * Choosing between one endless response and segments.
 *
 * The bug: `All About the Benjamins` logged twelve transcode sessions in
 * eighteen minutes, every one at `start_at=0`, with no ffmpeg error. A
 * progressive transcode cannot be range-served, so when Chromium evicts
 * buffered media it can only re-ask from byte zero — and the further into the
 * film, the longer that takes. Segments make an eviction cost one segment.
 */

beforeEach(() => {
  localStorage.clear();
});

const says = (answer: string) => () => answer;

describe("filePath", () => {
  /*
   * Direct play is not touched, and this is the case most worth pinning.
   *
   * Those are the file's own bytes over a range server — already seekable,
   * already re-askable. The eviction problem belongs to streams that cannot be
   * re-asked, so routing a direct play through a transcode-backed playlist
   * would spend an encode to fix a problem it does not have.
   */
  it("leaves a direct play alone even where a playlist would work", () => {
    expect(filePath("direct", true)).toBe("direct");
  });

  it("sends a conversion down the playlist when the engine can take one", () => {
    expect(filePath("transcode", true)).toBe("hls");
    expect(filePath("remux", true)).toBe("hls");
  });

  it("falls back to the endless response when it cannot", () => {
    expect(filePath("transcode", false)).toBe("progressive");
  });
});

describe("hlsWorthTrying", () => {
  /*
   * The reason this is a *trial* and not a question.
   *
   * Chromium answers "maybe" to a playlist whether or not it will play one, so
   * the string cannot separate an engine that works from one that shows a black
   * rectangle. "maybe" therefore means *try it*, not *it works*.
   */
  it('treats "maybe" as worth attempting, because it is all Chromium ever says', () => {
    expect(hlsWorthTrying(says("maybe"))).toBe(true);
  });

  /*
   * The one answer that is trustworthy. An engine returning the empty string is
   * saying it does not know what a playlist is, and there is no reason to spend
   * a failed load discovering that.
   */
  it("believes an outright no", () => {
    expect(hlsWorthTrying(says(""))).toBe(false);
  });

  it("stops asking once a device has refused a playlist", () => {
    rememberHLS("refused");
    // Even though the engine still claims "maybe".
    expect(hlsWorthTrying(says("maybe"))).toBe(false);
  });

  /*
   * And a device that has played one is not re-litigated by a capability
   * string. This is what stops an engine whose canPlayType regresses — or is
   * simply wrong — from losing a path it has been observed using.
   */
  it("keeps using it once a device has played one", () => {
    rememberHLS("playable");
    expect(hlsWorthTrying(says(""))).toBe(true);
  });

  /*
   * The verdict survives a reload, which is the whole point of writing it down:
   * the cost of discovering it is a visibly failed load, and paying that once
   * per device is the trade. Asserted through the store rather than through a
   * fresh read, because readDevice keeps a module-level cache that outlives
   * clearing localStorage — so a "starts out unknown" assertion here would pass
   * or fail on test order rather than on behaviour.
   */
  it("writes the verdict down so a reload does not re-ask", () => {
    rememberHLS("refused");
    expect(hlsVerdict()).toBe("refused");
    expect(localStorage.getItem(HLS_VERDICT_KEY)).toContain("refused");
  });
});

describe("isUnsupportedSource", () => {
  /*
   * Narrow on purpose. Only "I could not make sense of this at all" is evidence
   * about the *engine*; everything else is about this file or this moment, and
   * retiring the better path over a transient fault would be permanent.
   */
  it("counts an unreadable source", () => {
    expect(isUnsupportedSource({ code: 4 } as MediaError)).toBe(true);
  });

  it("does not count a decode error, which is about the file", () => {
    expect(isUnsupportedSource({ code: 3 } as MediaError)).toBe(false);
  });

  it("does not count a network error, which is about the moment", () => {
    expect(isUnsupportedSource({ code: 2 } as MediaError)).toBe(false);
  });

  it("does not count a missing error object", () => {
    expect(isUnsupportedSource(null)).toBe(false);
  });
});
