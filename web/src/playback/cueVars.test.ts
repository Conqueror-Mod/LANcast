import { describe, it, expect } from "vitest";
import { applyCueVars, CUE_VARS } from "./cueVars";
import { DEFAULTS } from "./prefs";

/*
 * Subtitle appearance has to reach every document a subtitle is shown in.
 *
 * The preferences are written as custom properties because `::cue` cannot be
 * styled any other way — the cue box is in a shadow tree with no reachable
 * element. That works, and it worked only in the tab: the properties were set
 * as inline style on the *page's* `documentElement`, while the pop-out player
 * (ADR 0029) is a second document with its own root. `copyStyles` carries the
 * stylesheets across, and an inline style on an element is not a stylesheet, so
 * every `var(--cue-…, fallback)` in the copied rules resolved to its fallback.
 *
 * The result was subtitles at the shipped defaults in the pop-out window
 * whatever anyone had chosen, with nothing failing anywhere: the rule was
 * right, the properties were right, and no one asked which root they were on.
 */

const prefs = {
  ...DEFAULTS,
  subColor: "#ffe08a",
  subSize: 1.5,
  subPosition: 20,
};

describe("subtitle appearance across documents", () => {
  it("writes every property the stylesheet reads", () => {
    const root = document.createElement("div");
    applyCueVars(root, prefs);

    // The list is shared with the stylesheet's expectations rather than
    // retyped, so adding a property without applying it is a failing test
    // rather than a preference that quietly does nothing.
    for (const name of CUE_VARS) {
      expect(root.style.getPropertyValue(name), name).not.toBe("");
    }
  });

  it("carries the chosen values, not the defaults", () => {
    const root = document.createElement("div");
    applyCueVars(root, prefs);

    expect(root.style.getPropertyValue("--cue-color")).toBe("#ffe08a");
    expect(root.style.getPropertyValue("--cue-scale")).toBe("1.5");
    expect(root.style.getPropertyValue("--cue-bottom")).toBe("20%");
  });

  /*
   * The one that was broken. A second document gets the same treatment as the
   * first, because a pop-out is a place subtitles are read and not a preview.
   */
  it("applies to a second document's root as well as the page's", () => {
    const other = document.implementation.createHTMLDocument("popout");
    applyCueVars(other.documentElement, prefs);

    expect(
      other.documentElement.style.getPropertyValue("--cue-color"),
      "the pop-out window renders subtitles at the shipped defaults whatever " +
        "was chosen, because the properties were only ever set on the page",
    ).toBe("#ffe08a");
  });

  // A typeface is a stack, not one name: a machine without the first face has
  // to land somewhere deliberate rather than on the engine's serif default.
  it("writes a font stack that ends somewhere sensible", () => {
    const root = document.createElement("div");
    applyCueVars(root, { ...prefs, subFont: "sans" });
    const stack = root.style.getPropertyValue("--cue-font");

    expect(stack).toContain("sans-serif");
  });
});
