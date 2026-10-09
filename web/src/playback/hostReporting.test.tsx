/*
 * The host's reports: a pause, a play or a jump reaches the room at once, not
 * on the next three-second beat. Found 2026-10-09: a guest on another server
 * paused about two seconds after the host, and could take five.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { useHostReporting } from "./together";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let puts: { position_ms: number; paused: boolean }[];
let clock: { positionMS: number; paused: boolean };

function Host() {
  useHostReporting("room1", true, () => clock);
  return null;
}

beforeEach(() => {
  vi.useFakeTimers();
  puts = [];
  clock = { positionMS: 600_000, paused: false };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") puts.push(JSON.parse(String(init.body)));
      return new Response(null, { status: 204 });
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Host />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const advance = (ms: number) =>
  act(() => {
    vi.advanceTimersByTime(ms);
  });

describe("the host's reports", () => {
  it("reports at once when the room starts", () => {
    expect(puts).toHaveLength(1);
  });

  it("reports a pause within a quarter of a second, not on the next beat", () => {
    advance(1000);
    clock = { ...clock, positionMS: clock.positionMS + 1000, paused: true };
    advance(300);
    expect(puts.at(-1)).toEqual({ position_ms: 601_000, paused: true });
    expect(puts).toHaveLength(2);
  });

  it("reports a jump at once, and stays quiet while playing normally", () => {
    advance(1000);
    clock = { ...clock, positionMS: clock.positionMS + 1000 };
    advance(300);
    expect(puts).toHaveLength(1);
    clock = { ...clock, positionMS: clock.positionMS + 60_000 };
    advance(300);
    expect(puts).toHaveLength(2);
    expect(puts.at(-1)?.position_ms).toBe(661_000);
  });
});
