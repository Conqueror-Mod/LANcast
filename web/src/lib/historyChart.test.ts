import { describe, it, expect } from "vitest";
import { hourly, nearest, niceMax, pathFor, segments } from "./historyChart";

const at = (min: number, v: number) => ({ t: 1_800_000_000 + min * 60, v });

describe("history charts", () => {
  it("breaks the line where the server was off, and nowhere else", () => {
    const pts = [at(0, 1), at(1, 2), at(2, 3), at(10, 4), at(11, 5)];
    expect(segments(pts).map((s) => s.length)).toEqual([3, 2]);
    expect(segments([])).toEqual([]);
  });

  it("tops the axis at a round number above the peak", () => {
    expect(niceMax([0.3, 0.7])).toBe(1);
    expect(niceMax([3])).toBe(5);
    expect(niceMax([7, 12])).toBe(20);
    expect(niceMax([180])).toBe(200);
    expect(niceMax([])).toBe(1);
    expect(niceMax([0, 0])).toBe(1);
    expect(niceMax([37], 100)).toBe(100);
  });

  it("draws time across and value up, clamped to the top", () => {
    const from = at(0, 0).t;
    const to = at(10, 0).t;
    expect(pathFor([at(0, 0), at(5, 5), at(10, 20)], from, to, 10, 100, 50)).toBe("M0.0,50.0L50.0,25.0L100.0,0.0");
  });

  it("finds the sample under the pointer", () => {
    const pts = [at(0, 1), at(1, 2), at(2, 3)];
    expect(nearest(pts, at(1, 0).t + 20)?.v).toBe(2);
    expect(nearest([], 5)).toBeUndefined();
  });

  it("sums an hour into its average and its peak", () => {
    const rows = hourly([at(0, 2), at(30, 4), at(60, 10)]);
    expect(rows).toHaveLength(2);
    expect(rows[0].avg).toBe(3);
    expect(rows[0].max).toBe(4);
    expect(rows[1].max).toBe(10);
  });
});
