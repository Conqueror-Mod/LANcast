/*
 * The phone's on-screen controls without a screen (touchPad.ts): which
 * buttons each console gets, which key each sends, and how a thumb on the
 * d-pad becomes directions.
 */
import { describe, it, expect } from "vitest";
import { DPAD_DEAD_ZONE, dpadDirections, padLayout, PAD_KEYS, sendPadKey } from "./touchPad";

const labels = (cs: { label: string }[]) => cs.map((c) => c.label);
const sorted = (s: Set<string>) => [...s].sort();

describe("padLayout", () => {
  it("gives the NES and the Game Boys B, A, Select and Start, and no shoulders", () => {
    for (const p of ["nes", "gb", "gbc"]) {
      const l = padLayout(p);
      expect(labels(l.face)).toEqual(["B", "A"]);
      expect(l.face.map((c) => c.button)).toEqual(["b", "a"]);
      expect(labels(l.system)).toEqual(["Select", "Start"]);
      expect(l.shoulders).toEqual([]);
    }
  });

  it("gives the GBA its shoulders", () => {
    expect(padLayout("gba").shoulders.map((c) => c.button)).toEqual(["l", "r"]);
  });

  // Gearsystem reads button 1 from the RetroPad's B and 2 from its A.
  it("names the Master System's buttons 1 and 2, sent as B and A", () => {
    const l = padLayout("sms");
    expect(l.face).toEqual([
      { label: "1", button: "b" },
      { label: "2", button: "a" },
    ]);
    expect(labels(l.system)).toEqual(["Start"]);
  });

  // The libretro convention for the Mega Drive: A from Y, B from B, C from A.
  it("gives the Mega Drive A, B and C, sent as Y, B and A", () => {
    const l = padLayout("genesis");
    expect(l.face).toEqual([
      { label: "A", button: "y" },
      { label: "B", button: "b" },
      { label: "C", button: "a" },
    ]);
    expect(labels(l.system)).toEqual(["Start"]);
  });

  it("falls back to the NES pad for a console it does not know", () => {
    expect(labels(padLayout("something-new").face)).toEqual(["B", "A"]);
    expect(labels(padLayout(null).face)).toEqual(["B", "A"]);
  });
});

describe("dpadDirections", () => {
  it("holds nothing in the dead zone at the centre", () => {
    expect(dpadDirections(0, 0).size).toBe(0);
    expect(dpadDirections(DPAD_DEAD_ZONE * 0.9, 0).size).toBe(0);
  });

  it("holds one direction straight along an axis (y is down)", () => {
    expect(sorted(dpadDirections(1, 0))).toEqual(["right"]);
    expect(sorted(dpadDirections(-1, 0))).toEqual(["left"]);
    expect(sorted(dpadDirections(0, -1))).toEqual(["up"]);
    expect(sorted(dpadDirections(0, 1))).toEqual(["down"]);
  });

  it("holds two on a diagonal", () => {
    expect(sorted(dpadDirections(0.7, -0.7))).toEqual(["right", "up"]);
    expect(sorted(dpadDirections(-0.7, 0.7))).toEqual(["down", "left"]);
  });

  // Each direction owns a 135° wedge: 20° off the axis is still only that
  // axis, 30° is already a diagonal.
  it("splits eight ways, a diagonal's wedge 45° wide", () => {
    const at = (deg: number) => dpadDirections(Math.cos((deg * Math.PI) / 180), -Math.sin((deg * Math.PI) / 180));
    expect(sorted(at(20))).toEqual(["right"]);
    expect(sorted(at(30))).toEqual(["right", "up"]);
    expect(sorted(at(60))).toEqual(["right", "up"]);
    expect(sorted(at(70))).toEqual(["up"]);
  });

  // A thumb drifts outward while playing; past the rim still steers.
  it("keeps steering past the rim", () => {
    expect(sorted(dpadDirections(3, 0))).toEqual(["right"]);
  });
});

describe("sendPadKey", () => {
  it("sends RetroArch's default key for the button, down then up, bubbling", () => {
    const el = document.createElement("div");
    const parent = document.createElement("div");
    parent.append(el);
    const seen: KeyboardEvent[] = [];
    parent.addEventListener("keydown", (e) => seen.push(e));
    parent.addEventListener("keyup", (e) => seen.push(e));
    sendPadKey(el, "a", true);
    sendPadKey(el, "a", false);
    expect(seen.map((e) => [e.type, e.key, e.code, e.keyCode])).toEqual([
      ["keydown", "x", "KeyX", 88],
      ["keyup", "x", "KeyX", 88],
    ]);
  });

  it("sends Select as the right Shift", () => {
    expect(PAD_KEYS.select).toMatchObject({ code: "ShiftRight", location: 2 });
    const el = document.createElement("div");
    let got: KeyboardEvent | undefined;
    el.addEventListener("keydown", (e) => (got = e));
    sendPadKey(el, "select", true);
    expect(got?.location).toBe(2);
  });
});
