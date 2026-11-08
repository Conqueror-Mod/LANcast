import { afterEach, describe, expect, it, vi } from "vitest";
import { resetCapabilities } from "./capabilities";
import { peerPlaybackURL, peerSourceURL } from "./peerSource";

/*
 * A friend's film is decided by their server, from what this player says it
 * can decode.
 *
 * It said nothing. The far server then assumed the plainest browser, so a
 * film with AC3 sound was converted for a friend whose app plays AC3, and
 * watching it together took minutes to start (2026-11-07). The local player
 * has always sent `can=`; these are the same claims on the friend's routes.
 *
 * Driven through the real measurement (canPlayType), not a mock of the helper,
 * so the test fails if the peer builders stop asking it.
 */

const FP = "F7H2WQKUYHLQBW6GQKSIPP7TZGOZ72UJIORYLO6FUZQDTNLIORYQ";

function playerCan(answer: CanPlayTypeResult) {
  vi.spyOn(HTMLMediaElement.prototype, "canPlayType").mockReturnValue(answer);
  resetCapabilities();
}

afterEach(() => {
  vi.restoreAllMocks();
  resetCapabilities();
});

describe("a friend's playback carries this player's claims", () => {
  it("says what it can decode on every route the far server decides on", () => {
    playerCan("probably");
    for (const url of [
      peerPlaybackURL(FP, 42),
      peerSourceURL(FP, 42, "hls"),
      peerSourceURL(FP, 42, "hls", 300),
      peerSourceURL(FP, 42, "progressive"),
      peerSourceURL(FP, 42, "progressive", 300),
    ]) {
      const can = new URL(url, "https://x").searchParams.get("can") ?? "";
      expect(can.split(","), url).toContain("ac3");
    }
  });

  it("keeps the offset and the item beside the claims", () => {
    playerCan("probably");
    const hls = new URL(peerSourceURL(FP, 42, "hls", 300), "https://x");
    expect(hls.pathname).toBe(`/api/peers/${FP}/hls/42/index.m3u8`);
    expect(hls.searchParams.get("t")).toBe("300");
    const tr = new URL(peerSourceURL(FP, 42, "progressive", 300), "https://x");
    expect(tr.searchParams.get("item")).toBe("42");
    expect(tr.searchParams.get("t")).toBe("300");
  });

  it("sends no claims when the player claims nothing", () => {
    playerCan("");
    expect(peerPlaybackURL(FP, 42)).toBe(`/api/peers/${FP}/playback?item=42`);
    expect(peerSourceURL(FP, 42, "hls")).toBe(`/api/peers/${FP}/hls/42/index.m3u8`);
  });

  it("leaves direct play alone: those are the file's own bytes", () => {
    playerCan("probably");
    expect(peerSourceURL(FP, 42, "direct")).toBe(`/api/peers/${FP}/stream?item=42`);
  });
});
