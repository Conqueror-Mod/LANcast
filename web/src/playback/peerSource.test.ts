import { describe, it, expect } from "vitest";
import {
  peerSourceURL,
  peerSubtitleURL,
  peerSubtitlesURL,
  peerPlaybackURL, asMember } from "./peerSource";

/*
 * Where a peer's film comes from (ADR 0071 §5).
 *
 * The failure this file exists for does not throw and does not 404: every
 * local stream URL names a real item on *this* server, so a peer URL that
 * loses its peer plays the wrong film perfectly. There is no error to assert
 * on, which is why the assertion is on the URL.
 */

const FP = "F7H2WQKUYHLQBW6GQKSIPP7TZGOZ72UJIORYLO6FUZQDTNLIORYQ";

describe("a peer's playback URLs", () => {
  /*
   * The whole hazard, stated once: **nothing here may produce a local route.**
   *
   * Asserted as a property over every builder rather than case by case,
   * because the mistake is not getting one wrong — it is a future edit adding
   * a seventh builder that forgets.
   */
  it("never produces a URL that names this server's own items", () => {
    const urls = [
      peerSourceURL(FP, 42, "direct"),
      peerSourceURL(FP, 42, "hls"),
      peerSourceURL(FP, 42, "progressive"),
      peerSubtitlesURL(FP, 42),
      peerSubtitleURL(FP, 42, "en"),
      peerPlaybackURL(FP, 42),
    ];
    for (const url of urls) {
      expect(url.startsWith(`/api/peers/${FP}/`)).toBe(true);
      // `/api/stream/42` and `/api/items/42/…` are real routes here, holding a
      // real and different film.
      expect(url).not.toMatch(/^\/api\/(stream|items)\//);
    }
  });

  it("takes the route the far server's decision asks for", () => {
    expect(peerSourceURL(FP, 42, "direct")).toBe(`/api/peers/${FP}/stream?item=42`);
    expect(peerSourceURL(FP, 42, "progressive")).toBe(
      `/api/peers/${FP}/transcode?item=42`,
    );
  });

  /*
   * The item is in the path on the HLS route, and that is load-bearing rather
   * than a style choice: a playlist names its segments with a prefix and
   * cannot carry a query string, so a query here would not survive either hop.
   */
  it("puts the item in the path for HLS, where a playlist prefix can reach it", () => {
    const url = peerSourceURL(FP, 42, "hls");
    expect(url).toBe(`/api/peers/${FP}/hls/42/index.m3u8`);
    expect(url).not.toContain("?");
  });

  // A fingerprint is base32 today and a URL segment always. Encoding it is the
  // difference between a peer id changing shape later and a broken route.
  it("encodes what goes into a path segment", () => {
    expect(peerSubtitleURL("a/b", 42, "en/gb")).toBe(
      "/api/peers/a%2Fb/subtitles/42/en%2Fgb",
    );
  });
});

/*
 * A room member's URLs say so, and nobody else's do (federation Phase 5).
 *
 * The far server admits a film that was not shared only to a person in the
 * room, and this server names the caller only when the URL asks it to. A URL
 * that forgot would play the trailer of a refusal; a URL that always said it
 * would name this household's people while they browse.
 */
describe("asMember", () => {
  it("leaves a URL alone outside a room", () => {
    expect(asMember("/api/peers/F/stream?item=1", false)).toBe("/api/peers/F/stream?item=1");
  });
  it("adds to an existing query", () => {
    expect(asMember("/api/peers/F/stream?item=1", true)).toBe(
      "/api/peers/F/stream?item=1&together=1",
    );
  });
  it("starts a query where there is none", () => {
    expect(asMember("/api/peers/F/item/1", true)).toBe("/api/peers/F/item/1?together=1");
  });
});
