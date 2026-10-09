import { describe, it, expect, beforeEach } from "vitest";
import { getDock, nearestCorner, nextCorner, nextSize, resetDock, setDock } from "./dock";

beforeEach(() => {
  localStorage.clear();
  resetDock();
});

describe("the docked player's place", () => {
  it("starts where it always has: bottom right, medium", () => {
    expect(getDock()).toEqual({ corner: "br", size: "m", dx: 0, dy: 0 });
  });

  it("settles a card let go anywhere into the nearest corner", () => {
    const w = 1920, h = 1080;
    expect(nearestCorner(100, 100, w, h)).toBe("tl");
    expect(nearestCorner(1800, 100, w, h)).toBe("tr");
    expect(nearestCorner(100, 1000, w, h)).toBe("bl");
    expect(nearestCorner(1800, 1000, w, h)).toBe("br");
    // Dead centre goes to the bottom right, where it started.
    expect(nearestCorner(960, 540, w, h)).toBe("br");
  });

  it("cycles corners and sizes round, for the keyboard and the pad", () => {
    expect([nextCorner("br"), nextCorner("bl"), nextCorner("tl"), nextCorner("tr")]).toEqual(["bl", "tl", "tr", "br"]);
    expect([nextSize("s"), nextSize("m"), nextSize("l")]).toEqual(["m", "l", "s"]);
  });

  it("remembers the corner and size, and never the drag", () => {
    setDock({ corner: "tl", size: "l", dx: 40, dy: -12 });
    resetDock(); // as a new page load would
    expect(getDock()).toEqual({ corner: "tl", size: "l", dx: 0, dy: 0 });
  });

  it("ignores a stored value it does not recognise", () => {
    localStorage.setItem("lancast:dock", JSON.stringify({ corner: "middle", size: "huge" }));
    resetDock();
    expect(getDock()).toEqual({ corner: "br", size: "m", dx: 0, dy: 0 });
    localStorage.setItem("lancast:dock", "{not json");
    resetDock();
    expect(getDock().corner).toBe("br");
  });
});
