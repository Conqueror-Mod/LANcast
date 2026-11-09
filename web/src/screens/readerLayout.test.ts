import { describe, expect, it } from "vitest";
import { nextSpread, prevSpread, spreadPages, spreadStart } from "./readerLayout";

describe("two facing pages", () => {
  it("shows the cover alone, then pairs each even page with the odd one after it", () => {
    expect(spreadPages(0, 6)).toEqual([0]);
    expect(spreadPages(1, 6)).toEqual([1, 2]);
    expect(spreadPages(2, 6)).toEqual([1, 2]);
    expect(spreadPages(3, 6)).toEqual([3, 4]);
    // Six pages leave the last one over.
    expect(spreadPages(5, 6)).toEqual([5]);
  });

  it("pairs right to the end of an odd count", () => {
    expect(spreadPages(5, 7)).toEqual([5, 6]);
    expect(spreadPages(6, 7)).toEqual([5, 6]);
  });

  it("starts any page at its spread", () => {
    expect([0, 1, 2, 3, 4].map(spreadStart)).toEqual([0, 1, 1, 3, 3]);
  });

  it("turns a whole spread, and stops at the ends", () => {
    expect(nextSpread(0, 6)).toBe(1);
    expect(nextSpread(2, 6)).toBe(3);
    expect(nextSpread(5, 6)).toBe(5);
    expect(prevSpread(4)).toBe(1);
    expect(prevSpread(2)).toBe(0);
    expect(prevSpread(0)).toBe(0);
  });

  it("has nothing to show for an empty comic", () => {
    expect(spreadPages(0, 0)).toEqual([]);
  });

  it("puts a place beyond the end on the last spread", () => {
    expect(spreadPages(40, 6)).toEqual([5]);
  });
});
