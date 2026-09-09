/*
 * The client says when the viewer has gone (ADR 0065).
 *
 * The server cannot infer it on the HLS path: a poll of a playlist is not a
 * lifetime, so the request context cannot end the encode — correctly, or the
 * channel would die between the playlist and its first segment. That leaves the
 * client's own knowledge as the only exact signal there is, and it used to be
 * thrown away: the Stop button paused the element, cleared some state, and told
 * the server nothing.
 *
 * What that cost on a real server: two minutes of channel surfing filled every
 * session slot with channels already left, and it began refusing to play
 * anything — `refused a channel: at the session ceiling running=3 max=3`, three
 * times. One abandoned channel was still pulling a provider's stream at about
 * 120 KB/s three minutes after Live TV had been left entirely.
 *
 * jsdom sees no layout, and none of this is about layout: it is about which
 * request is sent, when. Which is exactly where this failed.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const channels = [
  { id: 1, source_id: 1, name: "One", logo_url: null, group: "UK", position: 0, tvg_id: "one" },
  { id: 2, source_id: 1, name: "Two", logo_url: null, group: "UK", position: 1, tvg_id: "two" },
];

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; method: string; keepalive: boolean }[];

beforeEach(() => {
  sent = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);

  // The element never really plays in jsdom; nothing here depends on it.
  const proto = window.HTMLMediaElement.prototype;
  proto.play = vi.fn(async () => {});
  proto.pause = vi.fn();
  proto.load = vi.fn();

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        sent.push({
          url: String(url),
          method,
          keepalive: init?.keepalive === true,
        });
        return new Response(null, { status: 204 });
      }
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (String(url).includes("/guide"))
        return json({ at: 0, channels: {}, programs: [] });
      if (String(url).includes("/api/channels")) return json({ channels });
      return json({});
    }),
  );
});

afterEach(() => {
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LiveTV />
      </QueryClientProvider>,
    );
  });
  await flush();
}

function tiles() {
  return [...host.querySelectorAll<HTMLButtonElement>(".livetv__channel")];
}

async function play(index: number) {
  await act(async () => tiles()[index].click());
  await flush();
}

const stops = () => sent.filter((s) => s.url.includes("/stop"));

describe("leaving a channel", () => {
  it("tells the server when Stop is pressed", async () => {
    await render();
    await play(0);
    expect(stops()).toHaveLength(0);

    const stop = [...host.querySelectorAll("button")].find(
      (b) => b.textContent?.trim() === "Stop",
    )!;
    await act(async () => stop.click());
    await flush();

    expect(stops()).toHaveLength(1);
    expect(stops()[0].url).toContain("/api/channels/1/stop");
    expect(stops()[0].method).toBe("POST");
  });

  /*
   * keepalive, and this is the detail the whole thing turns on.
   *
   * The stop fires exactly when a page is going away, and an ordinary request
   * issued during unload is routinely cancelled. A stop that only arrives when
   * the tab happens to survive misses the case it exists for.
   */
  it("sends it with keepalive, so it survives the page going away", async () => {
    await render();
    await play(0);
    await act(async () => root.unmount());
    await flush();

    expect(stops()).toHaveLength(1);
    expect(stops()[0].keepalive).toBe(true);
  });

  it("tells the server when the screen is left", async () => {
    await render();
    await play(0);
    await act(async () => root.unmount());
    await flush();

    expect(stops().map((s) => s.url)).toEqual([
      expect.stringContaining("/api/channels/1/stop"),
    ]);
  });

  /*
   * Switching channels stops the old one first.
   *
   * Otherwise both are pulled at once for as long as the idle timeout takes to
   * notice, which is the mechanism that filled every session slot in two
   * minutes of surfing.
   */
  it("stops the previous channel before starting the next", async () => {
    await render();
    await play(0);
    await play(1);

    expect(stops()).toHaveLength(1);
    expect(stops()[0].url).toContain("/api/channels/1/stop");
    expect(stops()[0].url).not.toContain("/api/channels/2/stop");
  });

  // Nothing playing, nothing to stop: leaving the screen must not invent a
  // request for a channel nobody watched.
  it("says nothing when no channel was playing", async () => {
    await render();
    await act(async () => root.unmount());
    await flush();

    expect(stops()).toHaveLength(0);
  });

  // The same channel clicked twice is not a switch, and must not stop what it
  // is about to start.
  it("does not stop a channel it is re-selecting", async () => {
    await render();
    await play(0);
    await play(0);

    expect(stops()).toHaveLength(0);
  });
});
