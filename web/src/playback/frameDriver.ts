import { createFrameRecorder, type FrameRecorder } from "./frameRecord";

/*
 * What a visualizer draws on: requestAnimationFrame, or the screen's own
 * refresh when the two disagree (ADR 0113).
 *
 * Chromium ticks rAF at one rate for the whole desktop. A window on a TV at
 * 30 Hz beside a 72 Hz main monitor drew 72 frames a second, and the TV showed
 * 2 of them, then 3, then 2, per picture: the motion stepped 28 ms then 42 ms
 * on every picture, which is the jitter, and 58% of the work was thrown away.
 *
 * In the desktop window the native side can wait on the monitor's own vertical
 * blank (cmd/lancast/displayclock.go). So a run starts on rAF, measures it,
 * and asks the window for its monitor's rate. If they agree, nothing changes.
 * If they do not, the run draws once per blank instead, moving by exactly one
 * refresh each time, and the rAF loop stops. It asks again every few seconds,
 * because the window can be dragged between monitors.
 *
 * In a browser there is no binding, and rAF is all there is; a browser on one
 * screen has no such problem to solve.
 */

/** rAF intervals measured before deciding. Half a second at 60 Hz. */
export const PACE_SAMPLES = 30;
/** How often a run on rAF asks again which monitor it is on. */
export const RECHECK_MS = 3_000;
/** Rates closer than this are one clock: 59.94 against 60 is not worth a switch. */
export const SAME_CLOCK = 0.03;
/** A blank that arrives sooner than this share of a refresh after the last draw is a burst. */
const BURST_SHARE = 0.5;
/** No step is longer than this, so a pause or a hidden window does not leap. */
const MAX_DT = 0.1;

declare global {
  interface Window {
    lancastDisplayRate?: () => Promise<{ hz?: number; device?: string; error?: string }>;
    lancastDisplayTicks?: (on: boolean) => Promise<boolean>;
    __lancastDisplayTick?: (n: number, hz: number) => void;
  }
}

type TickListener = (n: number, hz: number) => void;
const listeners = new Set<TickListener>();

/** The ticks are wanted while anybody listens, and only then. */
function listen(l: TickListener): () => void {
  listeners.add(l);
  if (listeners.size === 1) {
    window.__lancastDisplayTick = (n, hz) => {
      for (const each of [...listeners]) each(n, hz);
    };
    window.lancastDisplayTicks?.(true)?.catch?.(() => {});
  }
  return () => {
    if (!listeners.delete(l) || listeners.size > 0) return;
    window.lancastDisplayTicks?.(false)?.catch?.(() => {});
  };
}

/** Whether two rates are, for drawing, the same clock. */
export function sameClock(a: number, b: number): boolean {
  return a > 0 && b > 0 && Math.abs(a - b) / b < SAME_CLOCK;
}

function median(xs: number[]): number {
  const s = [...xs].sort((a, b) => a - b);
  return s.length ? s[Math.floor(s.length / 2)] : 0;
}

export interface FrameDriver {
  readonly recorder: FrameRecorder;
  stop(): void;
}

/**
 * Run `draw` once per frame until stopped. `draw` gets the seconds to move by,
 * already capped, and must not schedule frames itself.
 */
export function driveFrames(kind: string, draw: (dt: number) => void, rec: FrameRecorder = createFrameRecorder(kind)): FrameDriver {
  let stopped = false;
  let rafId = 0;
  let last = -1;
  let paces: number[] = [];
  let lastAsk = -Infinity;
  let asking = false;
  let unlisten: (() => void) | null = null;
  let tickHz = 0;
  let lastN = -1;
  let lastDrawAt = -Infinity;
  // The window gave up on the screen's ticks once: rAF for the rest of the run,
  // rather than flapping between the two.
  let refused = false;
  // The rate rAF was last measured at, so a tick at that rate (the window
  // dragged onto the main monitor) can send the run back to it.
  let rafHz = 0;

  const run = (now: number, dt: number) => {
    const t0 = performance.now();
    draw(Math.min(MAX_DT, dt));
    rec.tick(now, performance.now() - t0);
  };

  const frame = (now: number) => {
    rafId = requestAnimationFrame(frame);
    const dt = last < 0 ? 1 / 60 : (now - last) / 1000;
    if (last >= 0 && paces.length < 240) paces.push(now - last);
    last = now;
    run(now, dt);
    if (now - lastAsk >= RECHECK_MS && paces.length >= PACE_SAMPLES) {
      lastAsk = now;
      ask(1000 / median(paces));
      paces = [];
    }
  };

  const toRaf = () => {
    unlisten?.();
    unlisten = null;
    tickHz = 0;
    document.removeEventListener("visibilitychange", onVisible);
    if (stopped) return;
    rec.clock("raf");
    last = -1;
    paces = [];
    lastAsk = performance.now(); // a fresh measure before asking again
    cancelAnimationFrame(rafId);
    rafId = requestAnimationFrame(frame);
  };

  const onTick = (n: number, hz: number) => {
    if (stopped) return;
    if (hz <= 0) {
      // The window gave up on the monitor's refresh: back to rAF, for good.
      refused = true;
      toRaf();
      return;
    }
    if (sameClock(rafHz, hz)) {
      // Now on a monitor rAF already keeps time for.
      toRaf();
      return;
    }
    if (hz !== tickHz) {
      tickHz = hz;
      rec.clock(`display@${hz}`);
    }
    if (document.visibilityState === "hidden") return;
    const now = performance.now();
    const period = 1000 / hz;
    // A burst: ticks the window thread queued while busy, arriving together.
    // Skipped, and the next one moves by all of them.
    if (now - lastDrawAt < period * BURST_SHARE) return;
    // n restarts when the ticks do; then this blank is one refresh.
    const steps = lastN < 0 || n <= lastN ? 1 : n - lastN;
    lastN = n;
    lastDrawAt = now;
    run(now, steps / hz);
  };

  // Hidden, the ticks stop (rAF would have); shown, they start again.
  const onVisible = () => {
    if (!unlisten) return;
    if (document.visibilityState === "hidden") {
      window.lancastDisplayTicks?.(false)?.catch?.(() => {});
    } else {
      lastN = -1;
      window.lancastDisplayTicks?.(true)?.catch?.(() => {});
    }
  };

  const toDisplay = (hz: number) => {
    if (stopped || unlisten) return;
    cancelAnimationFrame(rafId);
    rafId = 0;
    lastN = -1;
    tickHz = hz;
    rec.clock(`display@${hz}`);
    unlisten = listen(onTick);
    document.addEventListener("visibilitychange", onVisible);
  };

  const ask = (measured: number) => {
    rafHz = measured;
    const rate = window.lancastDisplayRate;
    if (!rate || !window.lancastDisplayTicks || refused || asking) return;
    asking = true;
    Promise.resolve()
      .then(() => rate())
      .then((r) => {
        if (stopped || unlisten || !r || !r.hz) return;
        if (!sameClock(measured, r.hz)) toDisplay(r.hz);
      })
      .catch(() => {})
      .finally(() => {
        asking = false;
      });
  };

  rafId = requestAnimationFrame(frame);

  return {
    recorder: rec,
    stop() {
      if (stopped) return;
      stopped = true;
      cancelAnimationFrame(rafId);
      unlisten?.();
      unlisten = null;
      document.removeEventListener("visibilitychange", onVisible);
      rec.stop();
    },
  };
}
