import { describe, it, expect } from "vitest";
import { frameAt, type PreviewSheets } from "./previews";

// Shrek's real shape: 5,409 s, six sheets of 10 × 10 at 240 × 136.
const shrek: PreviewSheets = { interval_ms: 10_000, columns: 10, rows: 10, width: 240, height: 136, sheets: 6 };

describe("the preview frame for a moment", () => {
  it("is the first frame at the start", () => {
    expect(frameAt(shrek, 0, 5409)).toEqual({ sheet: 0, x: 0, y: 0 });
  });

  it("walks left to right, then down, a frame per interval", () => {
    expect(frameAt(shrek, 10, 5409)).toEqual({ sheet: 0, x: 240, y: 0 });
    expect(frameAt(shrek, 99, 5409)).toEqual({ sheet: 0, x: 9 * 240, y: 0 });
    expect(frameAt(shrek, 100, 5409)).toEqual({ sheet: 0, x: 0, y: 136 });
  });

  it("moves to the next sheet after a hundred frames", () => {
    expect(frameAt(shrek, 1000, 5409)).toEqual({ sheet: 1, x: 0, y: 0 });
    expect(frameAt(shrek, 1999, 5409)).toEqual({ sheet: 1, x: 9 * 240, y: 9 * 136 });
  });

  it("never points past the last frame into the black the last sheet ends in", () => {
    // 5,409 s is frame 540: sheet 5, row 4, column 0.
    expect(frameAt(shrek, 5409, 5409)).toEqual({ sheet: 5, x: 0, y: 4 * 136 });
    expect(frameAt(shrek, 99_999, 5409)).toEqual({ sheet: 5, x: 0, y: 4 * 136 });
  });

  it("never asks for a sheet that does not exist", () => {
    const short = { ...shrek, sheets: 1 };
    expect(frameAt(short, 5000, 5409)?.sheet).toBe(0);
  });

  it("is nothing for a manifest that describes nothing", () => {
    expect(frameAt({ ...shrek, sheets: 0 }, 10, 5409)).toBeNull();
    expect(frameAt({ ...shrek, interval_ms: 0 }, 10, 5409)).toBeNull();
  });
});
