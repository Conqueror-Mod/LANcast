import { describe, it, expect } from "vitest";
import { configForKind } from "./libraryConfig";

/*
 * Which libraries offer a duration sort.
 *
 * The rule this file already learned the hard way: a sort the data cannot
 * produce is worse than a missing one. The music library once offered Year
 * against a column that is NULL for every artist, so it returned the same list
 * as Title — two of three options doing the same thing, reported as sorting
 * being broken.
 *
 * Duration is the same shape of mistake waiting to happen, and photographs are
 * the trap: the probe writes 40ms for every still image, so the sort would not
 * tie visibly there — it would hand back a confident, arbitrary order.
 */
const hasDuration = (kind: string) =>
  configForKind(kind).sorts.some((s) => s.value === "longest");

describe("the duration sort", () => {
  it("is offered on a film library", () => {
    const sorts = configForKind("movie").sorts.map((s) => s.value);
    expect(sorts).toContain("longest");
    expect(sorts).toContain("shortest");
  });

  it("is not offered where every row would tie", () => {
    // Shows, artists and galleries are containers; none carries a duration.
    for (const kind of ["show", "music", "picture"]) {
      expect(hasDuration(kind)).toBe(false);
    }
  });

  // The labels are what the person reads; "longest" alone does not say of what.
  it("says what it sorts by", () => {
    const labels = configForKind("movie").sorts.map((s) => s.label);
    expect(labels).toContain("Duration (longest)");
    expect(labels).toContain("Duration (shortest)");
  });
});
