/*
 * The MilkDrop stage's rules outside the canvas (ADR 0091, step d): a preset
 * that fails is dropped and the stage moves on, reduced motion holds a
 * remembered stage on the cover, and butterchurn hears the newest audio.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { dropPreset, milkdropHeld, nextAfterBroken, resetMilkdropPickedForTests } from "./milkdropStage";
import { getPrefs, setPrefs } from "./prefs";
import { newestSamples } from "./MilkdropCanvas";

beforeEach(() => resetMilkdropPickedForTests());

describe("a preset that fails", () => {
  const ids = ["a", "b", "c"];

  it("gives way to the next stored preset", () => {
    expect(nextAfterBroken("a", ids, new Set(["a"]))).toEqual({ stage: "milkdrop", milkdrop: "b" });
  });

  it("wraps round, and skips presets that have failed already", () => {
    expect(nextAfterBroken("c", ids, new Set(["c", "a"]))).toEqual({ stage: "milkdrop", milkdrop: "b" });
  });

  it("gives way to the cover when none is left", () => {
    expect(nextAfterBroken("a", ids, new Set(ids))).toEqual({ stage: "cover" });
    expect(nextAfterBroken("a", ["a"], new Set(["a"]))).toEqual({ stage: "cover" });
  });

  it("moves the stage only when it was the one playing", () => {
    setPrefs({ stage: "milkdrop", milkdrop: "b" });
    dropPreset("a", ids);
    expect(getPrefs()).toMatchObject({ stage: "milkdrop", milkdrop: "b" });
    dropPreset("b", ids);
    expect(getPrefs()).toMatchObject({ stage: "milkdrop", milkdrop: "c" });
  });
});

describe("reduced motion", () => {
  it("holds a remembered MilkDrop stage until somebody picks it", () => {
    expect(milkdropHeld("milkdrop", true, false)).toBe(true);
    expect(milkdropHeld("milkdrop", true, true)).toBe(false);
  });

  it("holds nothing else, and nothing without reduced motion", () => {
    expect(milkdropHeld("tunnel", true, false)).toBe(false);
    expect(milkdropHeld("milkdrop", false, false)).toBe(false);
  });
});

describe("butterchurn's audio window", () => {
  it("is the newest 1024 samples of the tap's 2048", () => {
    const td = Uint8Array.from({ length: 2048 }, (_, i) => i % 256);
    const out = newestSamples(td, new Uint8Array(1024));
    expect(out[0]).toBe(td[1024]);
    expect(out[1023]).toBe(td[2047]);
  });

  it("pads a shorter buffer with silence before it", () => {
    const out = newestSamples(new Uint8Array([200, 201]), new Uint8Array(4));
    expect(Array.from(out)).toEqual([128, 128, 200, 201]);
  });
});
