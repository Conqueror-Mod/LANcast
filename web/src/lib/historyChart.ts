/*
 * The dashboard's history charts (ADR 0106): the arithmetic, kept apart from
 * the drawing so it can be tested without a layout engine.
 */

export interface Point {
  t: number; // unix seconds
  v: number;
}

/*
 * Lines broken where the server was not running. Samples come a minute apart;
 * a gap of more than three minutes is the server being off, and drawing a
 * straight line across it would invent the numbers in between.
 */
export function segments(points: Point[], gapSec = 180): Point[][] {
  const out: Point[][] = [];
  let cur: Point[] = [];
  for (const p of points) {
    if (cur.length > 0 && p.t - cur[cur.length - 1].t > gapSec) {
      out.push(cur);
      cur = [];
    }
    cur.push(p);
  }
  if (cur.length > 0) out.push(cur);
  return out;
}

/*
 * A y-axis top that reads as a number somebody would write: 1, 2, 5 times a
 * power of ten, at or above the largest value. Counts of people and
 * conversions never get a fractional top, and an empty chart still has one.
 */
export function niceMax(values: number[], fixed?: number): number {
  if (fixed !== undefined) return fixed;
  const max = Math.max(0, ...values);
  if (max <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(max)));
  for (const step of [1, 2, 5, 10]) {
    if (step * pow >= max) return step * pow;
  }
  return 10 * pow;
}

// The SVG path for one segment, in a box of width × height, time across.
export function pathFor(seg: Point[], from: number, to: number, yMax: number, width: number, height: number): string {
  const span = Math.max(1, to - from);
  return seg
    .map((p, i) => {
      const x = ((p.t - from) / span) * width;
      const y = height - (Math.min(p.v, yMax) / yMax) * height;
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join("");
}

// The point nearest a time, for the crosshair; undefined when there is none.
export function nearest(points: Point[], t: number): Point | undefined {
  let best: Point | undefined;
  for (const p of points) {
    if (!best || Math.abs(p.t - t) < Math.abs(best.t - t)) best = p;
  }
  return best;
}

// Hourly rows for the table view: the hour's average and its peak.
export function hourly(points: Point[]): { hour: number; avg: number; max: number }[] {
  const by = new Map<number, number[]>();
  for (const p of points) {
    const hour = Math.floor(p.t / 3600) * 3600;
    by.set(hour, [...(by.get(hour) ?? []), p.v]);
  }
  return [...by.entries()]
    .sort(([a], [b]) => a - b)
    .map(([hour, vs]) => ({ hour, avg: vs.reduce((a, b) => a + b, 0) / vs.length, max: Math.max(...vs) }));
}
