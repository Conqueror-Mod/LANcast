/*
 * The mpv backend (ADR 0067) against stubbed window bindings. What matters is
 * that it behaves like the element the provider was written for: same events,
 * same order, play() honoured even when it arrives before the file is open, and
 * a seek asked for early applied once there is something to seek.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { MpvBackend, NATIVE_VIDEO_CLASS, streamItem } from "./mpvBackend";

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("streamItem", () => {
  it("reads the item from a direct-play source only", () => {
    expect(streamItem("/api/stream/42")).toBe(42);
    expect(streamItem("/api/stream/42?audio=2")).toBe(42);
    expect(streamItem("/api/stream/42/transcode?t=3")).toBeNull();
    expect(streamItem("https://elsewhere/api/stream/42")).toBeNull();
  });
});

describe("MpvBackend", () => {
  let commands: [string, number][];
  let opened: [number, string][];

  beforeEach(() => {
    commands = [];
    opened = [];
    window.lancastMpvOpen = vi.fn(async (id: number, t: string) => {
      opened.push([id, t]);
    });
    window.lancastMpvCommand = vi.fn(async (n: string, v: number) => {
      commands.push([n, v]);
    });
    window.lancastMpvStop = vi.fn(async () => {});
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ ticket: "tk", item_id: 42, expires_at: 0 }), {
        status: 200,
      }),
    ) as typeof fetch;
  });

  afterEach(() => {
    document.documentElement.classList.remove(NATIVE_VIDEO_CLASS);
    delete window.lancastMpvOpen;
    delete window.lancastMpvCommand;
    delete window.lancastMpvStop;
  });

  it("mints a ticket for the item and opens it, never a URL", async () => {
    const b = new MpvBackend();
    b.src = "/api/stream/42";
    b.load();
    await flush();
    await flush();
    expect(fetch).toHaveBeenCalledWith(
      "/api/items/42/stream-ticket",
      expect.objectContaining({ method: "POST" }),
    );
    expect(opened).toEqual([[42, "tk"]]);
    expect(document.documentElement.classList.contains(NATIVE_VIDEO_CLASS)).toBe(true);
  });

  it("honours a play() that arrived before the file was open", async () => {
    const b = new MpvBackend();
    b.src = "/api/stream/42";
    b.load();
    void b.play();
    await flush();
    await flush();
    await flush();
    expect(commands.filter(([n]) => n === "play").length).toBeGreaterThanOrEqual(1);
    const lastPlay = commands.map(([n]) => n).lastIndexOf("play");
    expect(lastPlay).toBe(commands.length - 1);
  });

  it("refuses a source that is not a direct stream with an unsupported error", () => {
    const b = new MpvBackend();
    const seen: string[] = [];
    b.addEventListener("error", () => seen.push("error"));
    b.src = "/api/stream/42/transcode";
    b.load();
    expect(seen).toEqual(["error"]);
    expect(b.error?.code).toBe(4);
  });

  it("re-raises the client's events and applies an early seek on metadata", () => {
    const b = new MpvBackend();
    const seen: string[] = [];
    for (const n of ["loadedmetadata", "loadeddata", "timeupdate"]) {
      b.addEventListener(n, () => seen.push(n));
    }
    b.currentTime = 600; // resume point set before the file is open
    expect(commands.some(([n]) => n === "seek")).toBe(false);

    b.receive({
      events: ["loadedmetadata", "loadeddata"],
      current_time: 0,
      duration: 5400,
      paused: true,
      ended: false,
    });
    expect(seen).toEqual(["loadedmetadata", "loadeddata"]);
    expect(b.duration).toBe(5400);
    expect(b.readyState).toBe(4);
    expect(commands).toContainEqual(["seek", 600]);

    b.receive({ events: ["timeupdate"], current_time: 601, duration: 5400, paused: false, ended: false });
    expect(b.currentTime).toBe(601);
    expect(b.paused).toBe(false);
  });

  it("reports an unknown duration as NaN, like the element", () => {
    const b = new MpvBackend();
    b.receive({ events: [], current_time: 0, duration: null, paused: true, ended: false });
    expect(Number.isNaN(b.duration)).toBe(true);
  });

  it("removing the source stops the client and drops the see-through page", async () => {
    const b = new MpvBackend();
    b.src = "/api/stream/42";
    b.load();
    await flush();
    await flush();
    b.removeAttribute("src");
    b.load(); // what the provider does next; must not raise an error
    expect(window.lancastMpvStop).toHaveBeenCalled();
    expect(document.documentElement.classList.contains(NATIVE_VIDEO_CLASS)).toBe(false);
    expect(b.error).toBeNull();
  });
});
