import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import {
  DENIAL_TTL_MS,
  clearDenials,
  deniedCapabilities,
  deny,
} from "./capabilities";

const KEY = "lancast:codec-denied";

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
});

/*
 * The bug this file exists for.
 *
 * Denials used to be permanent. Found on a real install, this key held every
 * claim the client can make — `["hevc","hevc10","ac3","eac3"]` — and the
 * machine had been serving a full 4K HEVC re-encode plus an audio re-encode for
 * every such film. Clearing it and replaying the same file direct-played with
 * no ffmpeg at all, so all four were false.
 *
 * What made it survive is that nothing failed. The fallback is correct
 * behaviour, so a permanently downgraded machine is indistinguishable from a
 * working one.
 */
describe("a denial does not last for ever", () => {
  it("withholds a claim while the denial is fresh", () => {
    expect(deny("hevc")).toBe(true);
    expect(deniedCapabilities().map((d) => d.name)).toEqual(["hevc"]);
  });

  it("does not report the same denial as news twice", () => {
    expect(deny("hevc")).toBe(true);
    // The caller retries only on news; saying yes twice is how a failing file
    // becomes a retry loop.
    expect(deny("hevc")).toBe(false);
  });

  it("forgets a denial once it has aged out", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    deny("hevc");
    expect(deniedCapabilities()).toHaveLength(1);

    vi.setSystemTime(Date.now() + DENIAL_TTL_MS + 1000);
    expect(deniedCapabilities()).toHaveLength(0);
  });

  it("keeps a denial that has not aged out yet", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    deny("hevc");

    vi.setSystemTime(Date.now() + DENIAL_TTL_MS - 1000);
    expect(deniedCapabilities().map((d) => d.name)).toEqual(["hevc"]);
  });

  it("counts an expired denial as news again, so the claim is retried", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    deny("hevc");

    vi.setSystemTime(Date.now() + DENIAL_TTL_MS + 1000);
    // News, because the machine may have changed — a driver, a codec
    // extension. That is the whole point of the expiry.
    expect(deny("hevc")).toBe(true);
  });
});

/*
 * The upgrade path, which is the repair for every install already carrying one.
 *
 * A legacy array was written when denials were permanent, so it has been
 * standing for an unknown length of time and cannot be dated. Reading it as
 * expired gives every machine exactly one retry; a denial that is still true
 * comes straight back on the next failure.
 */
describe("the legacy format", () => {
  it("expires a bare array rather than honouring it for ever", () => {
    localStorage.setItem(KEY, JSON.stringify(["hevc", "hevc10", "ac3", "eac3"]));
    expect(deniedCapabilities()).toHaveLength(0);
  });

  it("lets the retry happen, rather than swallowing it silently", () => {
    localStorage.setItem(KEY, JSON.stringify(["hevc"]));
    expect(deny("hevc")).toBe(true);
  });
});

describe("clearing by hand", () => {
  it("forgets everything", () => {
    deny("hevc");
    deny("ac3");
    expect(deniedCapabilities()).toHaveLength(2);

    clearDenials();
    expect(deniedCapabilities()).toHaveLength(0);
  });

  it("leaves the store in a shape the next denial can write to", () => {
    deny("hevc");
    clearDenials();
    expect(deny("hevc")).toBe(true);
    expect(deniedCapabilities().map((d) => d.name)).toEqual(["hevc"]);
  });
});

/*
 * Storage is allowed to be unavailable or corrupt. A browser in private mode
 * throws on access, and a half-written value is a thing that happens; neither
 * may take playback down with it.
 */
describe("a store that cannot be trusted", () => {
  it("treats unparseable content as nothing denied", () => {
    localStorage.setItem(KEY, "{not json");
    expect(deniedCapabilities()).toHaveLength(0);
  });

  it("ignores entries that are not timestamps", () => {
    localStorage.setItem(KEY, JSON.stringify({ hevc: "yesterday", ac3: null }));
    expect(deniedCapabilities()).toHaveLength(0);
  });

  /*
   * A clock that jumped forward and came back would otherwise leave a denial
   * dated in the future, which no elapsed time can ever clear — permanence
   * reintroduced by accident, which is the exact bug being fixed.
   */
  it("does not let a future timestamp outlive the ceiling", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const wayAhead = Date.now() + 10 * DENIAL_TTL_MS;
    localStorage.setItem(KEY, JSON.stringify({ hevc: wayAhead }));

    expect(deniedCapabilities()).toHaveLength(1);
    vi.setSystemTime(Date.now() + DENIAL_TTL_MS + 1000);
    expect(deniedCapabilities()).toHaveLength(0);
  });
});

/*
 * Withholding a codec by hand.
 *
 * The automatic denial cannot catch the worst version of this fault, and this
 * is the case that proved it: HEVC Main 10 direct-played with hardware decode
 * genuinely engaged, the server idle and the bytes arriving — and a juddering
 * picture in two different browsers, while the same film re-encoded to H.264
 * played perfectly. `canPlayType` answered `probably`, which means "I can
 * decode this", not "I can decode this well enough to watch".
 *
 * Nothing failed, so `deny` was never called and never would be.
 */
describe("withholding a codec by hand", () => {
  const WKEY = "lancast:codec-withheld";

  it("is kept apart from the automatic denials", async () => {
    const { withhold } = await import("./capabilities");
    withhold("hevc10");

    // Separate keys, because they answer different questions and only one of
    // them is allowed to expire.
    expect(JSON.parse(localStorage.getItem(WKEY)!)).toEqual(["hevc10"]);
    expect(localStorage.getItem(KEY)).toBeNull();
    expect(deniedCapabilities()).toEqual([]);
  });

  /*
   * The assertion that matters most.
   *
   * An automatic denial expires after a fortnight because it describes a
   * machine on a day. A person's judgement does not, and expiring it would put
   * the judder back every two weeks with no way to make it stop.
   */
  it("does not expire the way a failure-driven denial does", async () => {
    const { withhold, withheldCapabilities } = await import("./capabilities");
    withhold("hevc10");

    vi.useFakeTimers();
    vi.setSystemTime(Date.now() + DENIAL_TTL_MS * 4);

    expect(withheldCapabilities()).toEqual(["hevc10"]);
  });

  it("can be turned back on", async () => {
    const { withhold, restore, withheldCapabilities } = await import(
      "./capabilities"
    );
    withhold("hevc10");
    withhold("ac3");
    restore("hevc10");

    expect(withheldCapabilities()).toEqual(["ac3"]);
  });

  it("survives nonsense in the store rather than refusing to play", async () => {
    // Withholding nothing is the safe direction: the worst case is a file that
    // stutters, where the alternative would be a client that claims nothing and
    // re-encodes the whole library.
    localStorage.setItem(WKEY, "{not json");
    const { withheldCapabilities } = await import("./capabilities");
    expect(withheldCapabilities()).toEqual([]);
  });

  // Clearing the automatic denials must not quietly re-enable something a
  // person turned off — "try them again" is about failures, not judgements.
  it("is not undone by clearing denials", async () => {
    const { withhold, withheldCapabilities } = await import("./capabilities");
    deny("ac3");
    withhold("hevc10");

    clearDenials();

    expect(deniedCapabilities()).toEqual([]);
    expect(withheldCapabilities()).toEqual(["hevc10"]);
  });
});
