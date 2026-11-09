import { describe, expect, it } from "vitest";
import { clamp, initialCrop, MAX_ZOOM, pan, sourceRect, zoomTo } from "./pictureCrop";

describe("picture crop", () => {
  it("starts centred, with the short side filling the frame", () => {
    const c = initialCrop(400, 200, 100);
    expect(sourceRect(c)).toEqual({ sx: 100, sy: 0, side: 200 });
  });

  it("never leaves an edge of the frame empty", () => {
    let c = initialCrop(400, 200, 100);
    c = pan(c, 1000, 1000);
    expect(sourceRect(c).sx).toBe(0);
    expect(sourceRect(c).sy).toBe(0);
    c = pan(c, -5000, -5000);
    const r = sourceRect(c);
    expect(r.sx + r.side).toBeCloseTo(400);
    expect(r.sy + r.side).toBeCloseTo(200);
  });

  it("keeps zoom between 1 and the maximum", () => {
    const c = initialCrop(300, 300, 100);
    expect(zoomTo(c, 0.2).zoom).toBe(1);
    expect(zoomTo(c, 99).zoom).toBe(MAX_ZOOM);
  });

  it("zooms about the centre of the frame", () => {
    const c = initialCrop(300, 300, 100);
    const r = sourceRect(zoomTo(c, 2));
    expect(r.side).toBeCloseTo(150);
    expect(r.sx + r.side / 2).toBeCloseTo(150);
    expect(r.sy + r.side / 2).toBeCloseTo(150);
  });

  it("pulls an out-of-range position back when zooming out", () => {
    let c = zoomTo(initialCrop(300, 300, 100), 3);
    c = pan(c, -1000, -1000);
    c = zoomTo(c, 1);
    expect(clamp(c)).toEqual(c);
    expect(sourceRect(c)).toEqual({ sx: 0, sy: 0, side: 300 });
  });
});
