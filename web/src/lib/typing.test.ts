/*
 * The player gives way to typing and to nothing else. A slider with focus
 * used to take the arrow keys from it, so seeking needed a click elsewhere
 * first (diagnosed 2026-10-17).
 */
import { describe, it, expect } from "vitest";
import { typingInto } from "./typing";

function input(type: string) {
  const el = document.createElement("input");
  el.type = type;
  return el;
}

describe("typingInto", () => {
  it("is true for things that take text", () => {
    for (const type of ["text", "search", "password", "number", "email"]) {
      expect(typingInto(input(type))).toBe(true);
    }
    expect(typingInto(document.createElement("textarea"))).toBe(true);
    // contenteditable is not asserted: jsdom does not implement
    // isContentEditable, so it would test jsdom rather than this.
  });

  it("is false for sliders and buttons, so the transport keys stay the player's", () => {
    for (const type of ["range", "checkbox", "button", "submit"]) {
      expect(typingInto(input(type))).toBe(false);
    }
    expect(typingInto(document.createElement("button"))).toBe(false);
    expect(typingInto(document.body)).toBe(false);
    expect(typingInto(null)).toBe(false);
  });
});
