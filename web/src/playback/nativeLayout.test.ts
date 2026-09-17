import { describe, it, expect } from "vitest";
import { HIDDEN, nativeLayout, sameLayout } from "./nativeLayout";

const box = { left: 1600, top: 780, width: 300, height: 168.75 };

describe("nativeLayout", () => {
  it("hides the picture when nothing plays natively, whatever the surface", () => {
    expect(nativeLayout("full", false, box, 1)).toEqual(HIDDEN);
    expect(nativeLayout("mini", false, box, 1)).toEqual(HIDDEN);
  });

  it("hides it when the surface is idle", () => {
    expect(nativeLayout("idle", true, box, 1)).toEqual(HIDDEN);
  });

  it("full needs no rectangle; the client fills the window", () => {
    expect(nativeLayout("full", true, null, 1.5).layout).toBe("full");
  });

  it("docks the picture on the surface's box, in device pixels", () => {
    const l = nativeLayout("mini", true, box, 1.5);
    expect(l).toEqual({ layout: "mini", x: 2400, y: 1170, width: 450, height: 254 });
  });

  it("rounds outward so no black sliver of the box shows", () => {
    const l = nativeLayout("mini", true, { left: 10.4, top: 10.6, width: 100.2, height: 50.2 }, 1);
    expect(l.x).toBe(10);
    expect(l.y).toBe(10);
    expect(l.x + l.width).toBe(111);
    expect(l.y + l.height).toBe(61);
  });

  it("hides rather than docking a box that has no size yet", () => {
    expect(nativeLayout("mini", true, { left: 0, top: 0, width: 0, height: 0 }, 1)).toEqual(HIDDEN);
  });

  it("compares layouts by value, so an unchanged box sends nothing", () => {
    expect(sameLayout(nativeLayout("mini", true, box, 1), nativeLayout("mini", true, box, 1))).toBe(true);
    expect(sameLayout(HIDDEN, nativeLayout("full", true, null, 1))).toBe(false);
  });
});
