import { describe, it, expect } from "vitest";
import { EDGE_SLACK, edges, pageBy } from "./shelfEdges";

/*
 * The rule behind the chevrons.
 *
 * Separated from the component because jsdom performs no layout: every width is
 * zero there, so a test of the wiring would pass whatever the arithmetic said.
 * This is the part that can be wrong.
 */
describe("which way a shelf can go", () => {
  it("offers nothing when everything already fits", () => {
    // The common case on a wide window with a short row. Two dead chevrons
    // would be worse than none, because absence is honest.
    expect(edges(0, 1200, 900)).toEqual({ left: false, right: false });
    expect(edges(0, 1200, 1200)).toEqual({ left: false, right: false });
  });

  it("offers only right at the start of a long row", () => {
    expect(edges(0, 1000, 4000)).toEqual({ left: false, right: true });
  });

  it("offers both in the middle", () => {
    expect(edges(1500, 1000, 4000)).toEqual({ left: true, right: true });
  });

  it("offers only left at the end", () => {
    expect(edges(3000, 1000, 4000)).toEqual({ left: true, right: false });
  });

  /*
   * Fractional scroll positions are ordinary — a 1.25 device pixel ratio, a
   * smooth scroll landing at 1487.5 — so the true end lands a fraction short.
   * Without slack the right chevron stays lit at the end of every shelf,
   * pointing at nothing.
   */
  it("treats a fraction short of the end as the end", () => {
    expect(edges(2999.5, 1000, 4000).right).toBe(false);
    expect(edges(EDGE_SLACK - 0.5, 1000, 4000).left).toBe(false);
  });
});

describe("how far one press moves", () => {
  it("leaves an overlap rather than replacing the whole row", () => {
    // A full page leaves no landmark from the previous view, so the row simply
    // becomes different tiles with nothing to say which way you went.
    const step = pageBy(1000, 1);
    expect(step).toBeLessThan(1000);
    expect(step).toBeGreaterThan(800);
  });

  it("goes the way it is asked", () => {
    expect(pageBy(1000, -1)).toBe(-pageBy(1000, 1));
  });
});
