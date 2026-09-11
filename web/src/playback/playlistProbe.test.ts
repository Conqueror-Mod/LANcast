import { describe, it, expect } from "vitest";
import { probePlaylist, playlistWasServed, PLAYLIST_KIND_HEADER } from "./fileTransport";

/*
 * Asking the playlist endpoint what it actually handed over.
 *
 * Two facts, both needed before a refusal is remembered: whether a playlist was
 * served at all (a 503 is the server, not the engine), and whether it was still
 * growing (WebView2 refuses a growing playlist and plays a complete one).
 */

function answer(status: number, headers: Record<string, string>) {
  return (async () => new Response("#EXTM3U", { status, headers })) as unknown as typeof fetch;
}

const M3U = "application/vnd.apple.mpegurl";

describe("probePlaylist", () => {
  it("reads a complete playlist as served and not growing", async () => {
    const p = await probePlaylist("/x", answer(200, { "Content-Type": M3U, [PLAYLIST_KIND_HEADER]: "complete" }));
    expect(p).toEqual({ served: true, growing: false });
  });

  it("reads a growing playlist as served and growing", async () => {
    const p = await probePlaylist("/x", answer(200, { "Content-Type": M3U, [PLAYLIST_KIND_HEADER]: "growing" }));
    expect(p).toEqual({ served: true, growing: true });
  });

  // A server from before the header existed is treated as it always was, so an
  // older server's refusals still count exactly as they did.
  it("treats a server that does not say as not growing", async () => {
    const p = await probePlaylist("/x", answer(200, { "Content-Type": M3U }));
    expect(p).toEqual({ served: true, growing: false });
  });

  it("reads a server failure as not served", async () => {
    const p = await probePlaylist("/x", answer(503, { "Content-Type": "application/json" }));
    expect(p.served).toBe(false);
  });

  it("reads no answer at all as no verdict", async () => {
    const failing = (async () => {
      throw new TypeError("network");
    }) as unknown as typeof fetch;
    expect((await probePlaylist("/x", failing)).served).toBeNull();
    expect(await playlistWasServed("/x", failing)).toBeNull();
  });
});
