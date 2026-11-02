/*
 * The visualizer frame recorder (frameRecord.ts): the arithmetic of a window,
 * the line it writes, and when it writes one. Driven with made-up rAF
 * timestamps, so no clock and no canvas.
 */
import { describe, it, expect, afterEach, vi } from "vitest";
import {
  CLEAN_EVERY_MS,
  createFrameRecorder,
  EVERY_WINDOW_FOR_MS,
  formatSummary,
  summarise,
  WINDOW_MS,
} from "./frameRecord";

// 50 Hz, so windows close on exact multiples of WINDOW_MS and the tests
// below are not at the mercy of floating-point drift.
const TICK = 20;

/** Ticks at 50 Hz from `from` for `ms`, with `late` ticks standing across extra refreshes. */
function drive(
  rec: ReturnType<typeof createFrameRecorder>,
  from: number,
  ms: number,
  opts: { lateEvery?: number; work?: number } = {},
): number {
  let t = from;
  let i = 0;
  while (t < from + ms) {
    rec.tick(t, opts.work ?? 1);
    i++;
    t += opts.lateEvery && i % opts.lateEvery === 0 ? TICK * 3 : TICK;
  }
  return t;
}

describe("summarise", () => {
  it("takes the median tick as the pace and counts what stood across it", () => {
    const iv = [16.7, 16.7, 16.7, 16.7, 50, 16.7, 33.4];
    const ends = iv.map((_, i) => (i + 1) * 20);
    const s = summarise(iv, iv.map(() => 2), ends, {
      windowMs: 200,
      frames: 8,
      gaps: 0,
      longTasks: 0,
      longTaskMs: 0,
      events: {},
    });
    expect(s.paceMs).toBeCloseTo(16.7);
    expect(s.late).toBe(2); // 50 and 33.4
    expect(s.missed).toBe(3); // 50 missed two refreshes, 33.4 one
    expect(s.worstMs).toBe(50);
    expect(s.worstAtMs).toBe(100);
  });

  it("calls work slow when it leaves the compositor too little of a tick", () => {
    const iv = [16.7, 16.7, 16.7];
    const s = summarise(iv, [2, 14, 3], [1, 2, 3], {
      windowMs: 50,
      frames: 4,
      gaps: 0,
      longTasks: 0,
      longTaskMs: 0,
      events: {},
    });
    expect(s.slowWork).toBe(1);
    expect(s.workMax).toBe(14);
  });

  it("is all zeros for a window with no ticks rather than NaN", () => {
    const s = summarise([], [], [], { windowMs: 0, frames: 1, gaps: 0, longTasks: 0, longTaskMs: 0, events: {} });
    expect(s.paceMs).toBe(0);
    expect(s.late).toBe(0);
    expect(formatSummary("scope", "1x1@1.0", s)).not.toContain("NaN");
  });
});

describe("formatSummary", () => {
  it("writes key=value fields that grep like the Go side's", () => {
    const line = formatSummary("milkdrop", "1920x1080@1.0", {
      windowMs: 30_000,
      frames: 1800,
      paceMs: 1000 / 60,
      late: 2,
      missed: 3,
      worstMs: 58.12,
      worstAtMs: 12_340,
      p95Ms: 17.1,
      p99Ms: 18,
      workP50: 1.2,
      workP95: 3.4,
      workMax: 21,
      slowWork: 1,
      longTasks: 2,
      longTaskMs: 130.4,
      gaps: 0,
      events: { preset: 1 },
    });
    expect(line).toMatch(/^visualizer frames kind=milkdrop size=1920x1080@1\.0 window=30\.0s frames=1800 /);
    expect(line).toContain("rate=60.0/s late=2 missed=3 worst_ms=58.1 worst_at=+12.3s");
    expect(line).toContain("longtasks=2/130ms gaps=0 events=preset:1");
    // The binding cuts a note at 500 runes; the line must fit with room over.
    expect(line.length).toBeLessThan(400);
  });
});

describe("createFrameRecorder", () => {
  let lines: string[] = [];
  const report = (l: string) => lines.push(l);
  afterEach(() => {
    lines = [];
    vi.restoreAllMocks();
  });

  it("writes one line per window while a run is young, clean or not", () => {
    const rec = createFrameRecorder("spectrum", report);
    rec.size(800, 400);
    drive(rec, 1000, WINDOW_MS * 2 + TICK);
    expect(lines).toHaveLength(2);
    expect(lines[0]).toContain("kind=spectrum size=800x400@");
    expect(lines[0]).toContain("late=0");
  });

  it("goes quiet after the first minutes, except for windows with a late frame", () => {
    const rec = createFrameRecorder("scope", report);
    let t = drive(rec, 0, EVERY_WINDOW_FOR_MS + WINDOW_MS / 2);
    const early = lines.length;
    expect(early).toBe(EVERY_WINDOW_FOR_MS / WINDOW_MS);
    t = drive(rec, t, WINDOW_MS * 2); // clean: nothing
    expect(lines.length).toBe(early);
    drive(rec, t, WINDOW_MS + TICK, { lateEvery: 100 }); // late: written
    expect(lines.length).toBe(early + 1);
    expect(lines.at(-1)).toMatch(/late=[1-9]/);
  });

  it("still writes a clean window now and then, so a quiet log says it ran", () => {
    const rec = createFrameRecorder("scope", report);
    const t = drive(rec, 0, EVERY_WINDOW_FOR_MS + WINDOW_MS / 2);
    const early = lines.length;
    drive(rec, t, CLEAN_EVERY_MS + WINDOW_MS / 2);
    expect(lines.length).toBe(early + 1);
  });

  it("counts a hidden window's long pause as a gap, not a hitch", () => {
    const rec = createFrameRecorder("trails", report);
    let t = drive(rec, 0, 5_000);
    t += 4_000; // hidden: rAF stopped
    drive(rec, t, WINDOW_MS);
    expect(lines[0]).toContain("gaps=1");
    expect(lines[0]).toContain("late=0");
  });

  it("writes the partial window when the run stops, if it is long enough", () => {
    const rec = createFrameRecorder("milkdrop", report);
    rec.event("preset");
    rec.event("preset");
    drive(rec, 0, 8_000);
    rec.stop();
    expect(lines).toHaveLength(1);
    expect(lines[0]).toContain("events=preset:2");

    const short = createFrameRecorder("milkdrop", report);
    drive(short, 0, 2_000);
    short.stop();
    expect(lines).toHaveLength(1);
  });

  it("sends its lines to the window's log under the visualizer area", () => {
    const notes: string[][] = [];
    window.lancastClientNote = vi.fn(async (level: string, area: string, message: string) => {
      notes.push([level, area, message]);
      return true;
    });
    try {
      const rec = createFrameRecorder("scope");
      drive(rec, 0, WINDOW_MS + TICK);
      expect(notes).toHaveLength(1);
      expect(notes[0][0]).toBe("info");
      expect(notes[0][1]).toBe("visualizer");
    } finally {
      delete window.lancastClientNote;
    }
  });
});
