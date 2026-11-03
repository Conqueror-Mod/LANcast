/*
 * The visualizer frame driver (frameDriver.ts): rAF where rAF keeps the
 * screen's time, the window's display ticks where it does not (ADR 0113).
 * rAF is stepped by hand and the window's bindings are fakes, so every frame
 * and every switch is the test's to place.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { driveFrames, PACE_SAMPLES, sameClock } from "./frameDriver";
import type { FrameRecorder } from "./frameRecord";

let queue: Map<number, FrameRequestCallback>;
let nextId: number;
let clock: number;
let draws: number[];
let ticksCalls: boolean[];
let displayHz: number;
let clocks: string[];

const rec = (): FrameRecorder => ({
  tick: () => {},
  size: () => {},
  event: () => {},
  clock: (l) => clocks.push(l),
  stop: () => {},
});

/** One rAF tick at `now`, running whatever was requested for it. */
function raf(now: number) {
  clock = now;
  const due = [...queue.entries()];
  queue.clear();
  for (const [, cb] of due) cb(now);
}

/** rAF ticks at `hz` for `count` frames from `from`; returns the next time. */
function rafRun(from: number, hz: number, count: number): number {
  let t = from;
  for (let i = 0; i < count; i++) {
    raf(t);
    t += 1000 / hz;
  }
  return t;
}

const flush = () => new Promise((r) => setTimeout(r, 0));

function tick(n: number, hz: number, at?: number) {
  if (at !== undefined) clock = at;
  window.__lancastDisplayTick?.(n, hz);
}

function withBindings() {
  window.lancastDisplayRate = vi.fn(async () => ({ hz: displayHz, device: "\\\\.\\DISPLAY3" }));
  window.lancastDisplayTicks = vi.fn(async (on: boolean) => {
    ticksCalls.push(on);
    return on;
  });
}

beforeEach(() => {
  queue = new Map();
  nextId = 1;
  clock = 0;
  draws = [];
  ticksCalls = [];
  clocks = [];
  displayHz = 72;
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    const id = nextId++;
    queue.set(id, cb);
    return id;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => queue.delete(id));
  vi.spyOn(performance, "now").mockImplementation(() => clock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  delete window.lancastDisplayRate;
  delete window.lancastDisplayTicks;
  delete window.__lancastDisplayTick;
});

/** A run on a TV at 29.97 Hz from a 72 Hz rAF, switched over to the ticks. */
async function onTheTv() {
  withBindings();
  displayHz = 29.97;
  const d = driveFrames("scope", (dt) => draws.push(dt), rec());
  const t = rafRun(0, 72, PACE_SAMPLES + 2);
  await flush();
  draws = [];
  return { d, t };
}

describe("sameClock", () => {
  it("calls 59.94 and 60 one clock, and 72 and 30 two", () => {
    expect(sameClock(59.94, 60)).toBe(true);
    expect(sameClock(72, 29.97)).toBe(false);
    expect(sameClock(0, 60)).toBe(false);
  });
});

describe("driveFrames", () => {
  it("draws on rAF by the gap between ticks in a browser, with no window to ask", async () => {
    const d = driveFrames("spectrum", (dt) => draws.push(dt), rec());
    rafRun(0, 50, PACE_SAMPLES + 5);
    await flush();
    expect(draws[0]).toBeCloseTo(1 / 60); // the first frame has no gap to measure
    expect(draws[1]).toBeCloseTo(0.02);
    expect(draws).toHaveLength(PACE_SAMPLES + 5);
    d.stop();
  });

  it("stays on rAF when the monitor's rate is rAF's", async () => {
    withBindings();
    const d = driveFrames("spectrum", (dt) => draws.push(dt), rec());
    rafRun(0, 72, PACE_SAMPLES + 5);
    await flush();
    expect(window.lancastDisplayRate).toHaveBeenCalled();
    expect(ticksCalls).toEqual([]);
    d.stop();
  });

  it("moves to the screen's ticks when rAF runs at another rate, and moves by one refresh each", async () => {
    const { d, t } = await onTheTv();
    expect(ticksCalls).toEqual([true]);
    expect(clocks).toContain("display@29.97");
    // rAF no longer draws.
    rafRun(t, 72, 10);
    expect(draws).toEqual([]);
    tick(1, 29.97, 1000);
    tick(2, 29.97, 1033);
    tick(3, 29.97, 1067);
    expect(draws).toHaveLength(3);
    for (const dt of draws) expect(dt).toBeCloseTo(1 / 29.97);
    d.stop();
    expect(ticksCalls).toEqual([true, false]);
  });

  it("moves by every blank a missed tick covered", async () => {
    const { d } = await onTheTv();
    tick(5, 29.97, 1000);
    tick(7, 29.97, 1067); // the window thread lost one
    expect(draws[1]).toBeCloseTo(2 / 29.97);
    d.stop();
  });

  it("skips a burst of queued ticks and moves by all of them on the next", async () => {
    const { d } = await onTheTv();
    tick(1, 29.97, 1000);
    tick(2, 29.97, 1001); // queued behind a busy thread, delivered together
    tick(3, 29.97, 1002);
    expect(draws).toHaveLength(1);
    tick(4, 29.97, 1033);
    expect(draws[1]).toBeCloseTo(3 / 29.97);
    d.stop();
  });

  it("goes back to rAF for good when the window gives up the ticks", async () => {
    const { d, t } = await onTheTv();
    tick(0, 0, 1000);
    expect(ticksCalls).toEqual([true, false]);
    rafRun(t, 72, PACE_SAMPLES * 4);
    await flush();
    expect(draws.length).toBe(PACE_SAMPLES * 4);
    expect(ticksCalls).toEqual([true, false]); // never asked for them again
    expect(clocks.at(-1)).toBe("raf");
    d.stop();
  });

  it("goes back to rAF when the window is dragged onto a monitor at rAF's rate", async () => {
    const { d, t } = await onTheTv();
    tick(1, 29.97, 1000);
    tick(2, 72, 1033);
    expect(ticksCalls).toEqual([true, false]);
    draws = [];
    rafRun(t, 72, 3);
    expect(draws).toHaveLength(3);
    d.stop();
  });

  it("stops the ticks while the window is hidden and starts them when it is back", async () => {
    const { d } = await onTheTv();
    const vis = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    tick(1, 29.97, 1000);
    expect(draws).toEqual([]);
    vis.mockReturnValue("visible");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(ticksCalls).toEqual([true, false, true]);
    tick(1, 29.97, 2000); // restarted: counts from 1 again, one refresh
    expect(draws[0]).toBeCloseTo(1 / 29.97);
    d.stop();
  });

  it("draws nothing once stopped, from rAF or a tick", async () => {
    const { d, t } = await onTheTv();
    d.stop();
    tick(1, 29.97, 1000);
    rafRun(t, 72, 5);
    expect(draws).toEqual([]);
  });
});
