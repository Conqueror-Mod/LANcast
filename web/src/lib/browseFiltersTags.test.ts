import { describe, it, expect } from "vitest";
import { activePills, FILTER_CATEGORIES, FILTER_PARAM_KEYS } from "./browseFilters";

/*
 * Tags in the filter bar (ADR 0062).
 *
 * These exist because the feature shipped without them once. Tagging worked,
 * the API filtered, and the bar offered no way to use it — found by opening the
 * app and looking at the row of chips, not by any test, because every test was
 * about the parts that were built.
 */

describe("tags in the browse filters", () => {
  // A category, so the bar draws a panel for it at all.
  it("is one of the filter categories", () => {
    const tag = FILTER_CATEGORIES.find((c) => c.key === "tag");
    expect(tag).toBeTruthy();
    expect(tag?.label).toBe("Tag");
  });

  /*
   * Both keys are owned by the bar, so "clear all" clears them.
   *
   * Missing from this list, a tag filter survives clearing and the grid stays
   * mysteriously narrowed — the quiet version of the bug this project has
   * shipped four times.
   */
  it("owns both parameters, so clearing the bar clears them", () => {
    expect(FILTER_PARAM_KEYS).toContain("tag");
    expect(FILTER_PARAM_KEYS).toContain("favourite");
  });

  /*
   * A tag pill shows the name, never the id.
   *
   * The value in the URL is an id because a name is unique only within one
   * account; the label has to be resolved, and an unresolved one is held back
   * rather than shown raw — "tag 4" is not a thing anybody recognises as their
   * own note.
   */
  it("labels a tag pill with its name, and holds it back until the name is known", () => {
    const params = new URLSearchParams("tag=4&tag=9");
    const named = activePills(params, {
      tagNames: new Map([["4", "needs a better copy"]]),
    });
    const tags = named.filter((p) => p.key === "tag");
    expect(tags).toHaveLength(1);
    expect(tags[0].label).toBe("needs a better copy");
    expect(tags[0].value).toBe("4");
  });

  // The favourites toggle gets a pill too, so there is a way back out of it.
  it("gives favourites a removable pill", () => {
    const pills = activePills(new URLSearchParams("favourite=1"), {});
    const fav = pills.find((p) => p.key === "favourite");
    expect(fav?.label).toBe("Favourites");
  });
});
