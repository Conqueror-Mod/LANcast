import { clientNote } from "@/lib/clientNote";

/*
 * How evenly a visualizer draws, written to the desktop window's log.
 *
 * The film stutter was found with two-minute PresentMon runs plus mpv's own
 * drop count, because PresentMon cannot see a frame its maker never handed
 * over (ADR 0111). A visualizer has the same
 * blind spot, and no mpv to fill it: the page makes its own frames on
 * requestAnimationFrame, so a frame can go wrong in three places and only one
 * of them is visible to PresentMon.
 *
 *   - **The page was late.** The frame's own work, or something else on the
 *     main thread, ran past the refresh. PresentMon sees a long gap between
 *     presents and calls nothing lost. Here: `late`, `work_*`, `longtasks`.
 *   - **The GPU was late.** Little JavaScript, yet the next tick still comes
 *     late, because the browser waits on the GPU before it ticks again. Here:
 *     `late` with low `work_*`.
 *   - **DWM held or dropped it.** Steady ticks here, losses in PresentMon.
 *
 * So this is the page's half of the measurement, and a run is read beside a
 * PresentMon capture of msedgewebview2.exe (presentmon-runs/summarise-viz.ps1).
 *
 * It summarises rather than logs frames: one line per 30 s window, with the
 * worst frame's place in the window so a hitch can be found in the capture.
 * Every window is written for the first three minutes of a run (long enough
 * for a capture started after the music), then only windows that had a late
 * frame, plus a clean one every five minutes so a quiet log still says the
 * visualizer was running. An evening of music stays a few dozen lines.
 */

/** One summary line covers this much drawing. */
export const WINDOW_MS = 30_000;
/** Every window is written for this long after a run starts. */
export const EVERY_WINDOW_FOR_MS = 180_000;
/** After that, a clean window is written at most this often. */
export const CLEAN_EVERY_MS = 300_000;
/** A tick this far behind the last is the window being hidden, not a hitch. */
export const GAP_MS = 1_000;
/** A tick later than this many typical ticks missed at least one refresh. */
export const LATE_FACTOR = 1.5;
/** Work past this share of a tick leaves the compositor too little of it. */
export const BUDGET_SHARE = 0.75;
/** A partial window shorter than this is not worth a line when a run stops. */
const MIN_PARTIAL_MS = 5_000;
/** Enough room for 30 s at 240 Hz; a faster screen keeps counting, not storing. */
const MAX_SAMPLES = 8192;

export interface FrameSummary {
  windowMs: number;
  frames: number;
  /** The median tick, which is the refresh unless every frame is slow. */
  paceMs: number;
  late: number;
  /** Refreshes the late ticks stood across, beyond the one each should take. */
  missed: number;
  worstMs: number;
  /** Where the worst tick ended, from the start of the window. */
  worstAtMs: number;
  p95Ms: number;
  p99Ms: number;
  workP50: number;
  workP95: number;
  workMax: number;
  /** Frames whose own work ran past BUDGET_SHARE of a tick. */
  slowWork: number;
  longTasks: number;
  longTaskMs: number;
  gaps: number;
  events: Record<string, number>;
}

function percentile(sorted: ArrayLike<number>, p: number): number {
  if (sorted.length === 0) return 0;
  const i = Math.min(sorted.length - 1, Math.floor(sorted.length * p));
  return sorted[i];
}

/**
 * Summarise one window: the gaps between ticks, how long each frame's own
 * work took, and when each gap ended (ms from the window's start). Pure, so
 * the arithmetic is tested without a clock.
 */
export function summarise(
  intervals: ArrayLike<number>,
  work: ArrayLike<number>,
  endsAt: ArrayLike<number>,
  extra: { windowMs: number; frames: number; gaps: number; longTasks: number; longTaskMs: number; events: Record<string, number> },
): FrameSummary {
  const iv = Float64Array.from(intervals).sort();
  const wk = Float64Array.from(work).sort();
  const pace = percentile(iv, 0.5);
  let late = 0;
  let missed = 0;
  let worst = 0;
  let worstAt = 0;
  for (let i = 0; i < intervals.length; i++) {
    const d = intervals[i];
    if (pace > 0 && d > pace * LATE_FACTOR) {
      late++;
      missed += Math.max(1, Math.round(d / pace) - 1);
    }
    if (d > worst) {
      worst = d;
      worstAt = endsAt[i];
    }
  }
  let slow = 0;
  for (let i = 0; i < work.length; i++) if (pace > 0 && work[i] > pace * BUDGET_SHARE) slow++;
  return {
    ...extra,
    paceMs: pace,
    late,
    missed,
    worstMs: worst,
    worstAtMs: worstAt,
    p95Ms: percentile(iv, 0.95),
    p99Ms: percentile(iv, 0.99),
    workP50: percentile(wk, 0.5),
    workP95: percentile(wk, 0.95),
    workMax: wk.length ? wk[wk.length - 1] : 0,
    slowWork: slow,
  };
}

const f1 = (n: number) => n.toFixed(1);

/** The log line. key=value, like the Go side's, so it greps the same way. */
export function formatSummary(kind: string, size: string, s: FrameSummary, clock = "raf"): string {
  const rate = s.paceMs > 0 ? 1000 / s.paceMs : 0;
  const events = Object.entries(s.events)
    .map(([k, v]) => `${k}:${v}`)
    .join(",");
  return (
    `visualizer frames kind=${kind} size=${size} clock=${clock} window=${f1(s.windowMs / 1000)}s frames=${s.frames}` +
    ` pace_ms=${f1(s.paceMs)} rate=${f1(rate)}/s late=${s.late} missed=${s.missed}` +
    ` worst_ms=${f1(s.worstMs)} worst_at=+${f1(s.worstAtMs / 1000)}s p95_ms=${f1(s.p95Ms)} p99_ms=${f1(s.p99Ms)}` +
    ` work_p50=${f1(s.workP50)} work_p95=${f1(s.workP95)} work_max=${f1(s.workMax)} slow_work=${s.slowWork}` +
    ` longtasks=${s.longTasks}/${Math.round(s.longTaskMs)}ms gaps=${s.gaps}` +
    (events ? ` events=${events}` : "")
  );
}

export interface FrameRecorder {
  /** Once per tick, with rAF's timestamp and the ms this frame's own work took. */
  tick(now: number, workMs: number): void;
  /** The drawing buffer's size, as the frame just set it. */
  size(w: number, h: number): void;
  /** Something that may explain a hitch, such as a preset load. Counted per window. */
  event(name: string): void;
  /**
   * What is driving the frames: "raf", or "display@29.97" for the screen's own
   * ticks (frameDriver.ts). A change closes the window, so no line mixes two.
   */
  clock(label: string): void;
  /** The run has ended (paused, hidden, unmounted). Writes what is owed. */
  stop(): void;
}

type Report = (line: string) => void;

const defaultReport: Report = (line) => clientNote("info", "visualizer", line);

/**
 * A recorder for one run of one visualizer's loop. `kind` names it in the log
 * (spectrum, scope, a trail preset, milkdrop).
 */
export function createFrameRecorder(kind: string, report: Report = defaultReport): FrameRecorder {
  const intervals = new Float64Array(MAX_SAMPLES);
  const work = new Float64Array(MAX_SAMPLES);
  const endsAt = new Float64Array(MAX_SAMPLES);
  let n = 0;
  let frames = 0;
  let gaps = 0;
  let events: Record<string, number> = {};
  let longTasks = 0;
  let longTaskMs = 0;
  let runStart = -1;
  let windowStart = -1;
  let last = -1;
  let lastWritten = -Infinity;
  let w = 0;
  let h = 0;
  let clockLabel = "raf";

  /*
   * Long tasks anywhere on the page, so a hitch can be told apart as this
   * loop's own work or somebody else's (a React render, a decode, a fetch
   * handler). Absent in jsdom and some browsers; then the count stays 0.
   */
  let observer: PerformanceObserver | undefined;
  try {
    if (typeof PerformanceObserver === "function" && PerformanceObserver.supportedEntryTypes?.includes("longtask")) {
      observer = new PerformanceObserver((list) => {
        for (const e of list.getEntries()) {
          longTasks++;
          longTaskMs += e.duration;
        }
      });
      observer.observe({ type: "longtask" });
    }
  } catch {
    observer = undefined;
  }

  const reset = (at: number) => {
    windowStart = at;
    n = 0;
    frames = 0;
    gaps = 0;
    events = {};
    longTasks = 0;
    longTaskMs = 0;
  };

  const write = (at: number, partial: boolean) => {
    const span = at - windowStart;
    if (frames === 0 || (partial && span < MIN_PARTIAL_MS)) return;
    const s = summarise(intervals.subarray(0, n), work.subarray(0, n), endsAt.subarray(0, n), {
      windowMs: span,
      frames,
      gaps,
      longTasks,
      longTaskMs,
      events,
    });
    const early = windowStart - runStart < EVERY_WINDOW_FOR_MS;
    if (early || s.late > 0 || at - lastWritten >= CLEAN_EVERY_MS) {
      lastWritten = at;
      const dpr = typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1;
      report(formatSummary(kind, `${w}x${h}@${f1(dpr)}`, s, clockLabel));
    }
  };

  return {
    tick(now, workMs) {
      if (runStart < 0) {
        // Not reset(): a preset load or a long task before the first frame
        // belongs to the first window, and is the likeliest thing in it.
        runStart = now;
        windowStart = now;
      }
      if (last >= 0) {
        const d = now - last;
        if (d >= GAP_MS) gaps++;
        else if (n < MAX_SAMPLES) {
          intervals[n] = d;
          work[n] = workMs;
          endsAt[n] = now - windowStart;
          n++;
        }
      }
      last = now;
      frames++;
      if (now - windowStart >= WINDOW_MS) {
        write(now, false);
        reset(now);
      }
    },
    size(nw, nh) {
      w = nw;
      h = nh;
    },
    event(name) {
      events[name] = (events[name] ?? 0) + 1;
    },
    clock(label) {
      if (label === clockLabel) return;
      if (last >= 0) {
        write(last, true);
        reset(last);
      }
      clockLabel = label;
    },
    stop() {
      observer?.disconnect();
      if (last >= 0) write(last, true);
      last = -1;
    },
  };
}
