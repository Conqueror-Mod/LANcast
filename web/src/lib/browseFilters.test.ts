/*
 * The URL is the filter state, and this is the translation between it and what
 * the user can read.
 *
 * Worth testing on its own because every failure here is silent: a pill that
 * does not render leaves a grid filtered by something invisible, and a "Clear
 * all" that misses a key leaves it filtered with the controls all showing off.
 * Both look like the grid is simply wrong.
 */
import { describe, it, expect } from "vitest";
import {
  FILTER_CATEGORIES,
  FILTER_PARAM_KEYS,
  RATING_THRESHOLDS,
  activeCount,
  activePills,
  matchCollections,
  matchYears,
  ratingSteps,
} from "./browseFilters";
import type { Facets } from "@/api/types";

const facets: Facets = {
  genres: ["Drama"],
  decades: [1990],
  content_ratings: ["R"],
  has_watched: true,
  years: [1994, 1999, 2003, 2019],
  resolutions: [
    { key: "uhd", label: "4K", min_width: 3000, max_width: 0 },
    { key: "sd", label: "SD", min_width: 1, max_width: 1099 },
  ],
  has_in_progress: true,
  has_unmatched: false,
  collections: [
    { id: 7, name: "A Franchise", members: 4 },
    { id: 8, name: "A Pairing", members: 2 },
  ],
  max_rating: 8.4,
};

describe("active filter pills", () => {
  it("names every kind of filter in the URL", () => {
    const p = new URLSearchParams(
      "genre=Drama&decade=1990&year=1994&content_rating=R&resolution=uhd&status=in_progress&watched=false",
    );
    const labels = activePills(p, { facets }).map((x) => x.label);
    expect(labels).toEqual([
      "Drama",
      "1990s",
      "1994",
      "R",
      "4K",
      "In progress",
      "Unwatched",
    ]);
  });

  /*
   * The resolution label comes from the server's bucket table, never from a
   * copy here — otherwise a tier is "4K" in the panel and "uhd" in the pill.
   */
  it("labels a resolution from the facets rather than its key", () => {
    const p = new URLSearchParams("resolution=uhd");
    expect(activePills(p, { facets })[0].label).toBe("4K");
  });

  // Falls back to the raw key rather than dropping the pill: an unlabelled
  // filter is still applied, and hiding it would be the failure this row exists
  // to prevent.
  it("still shows a resolution whose label has not arrived", () => {
    const p = new URLSearchParams("resolution=hd720");
    expect(activePills(p, {})[0].label).toBe("hd720");
  });

  /*
   * A person is the one case that is held back. An id is not a name, and a pill
   * reading "person 12" that changes to "Ada Vance" under the cursor is worse
   * than one that appears a moment late.
   */
  it("waits for a name before showing a cast pill", () => {
    const p = new URLSearchParams("person=12");
    expect(activePills(p, { facets })).toHaveLength(0);

    const named = activePills(p, {
      facets,
      castNames: new Map([["12", "Ada Vance"]]),
    });
    expect(named).toEqual([{ key: "person", value: "12", label: "Ada Vance" }]);
  });

  /*
   * Acting and directing are separate filters, so their pills must be separate
   * words. Two pills reading "Ada Vance" would be indistinguishable, and
   * removing one would look like removing the other.
   */
  it("tells an acting credit from a directing one", () => {
    const p = new URLSearchParams("actor=12&director=12");
    const names = new Map([["12", "Ada Vance"]]);
    expect(activePills(p, { facets, castNames: names }).map((x) => x.label)).toEqual([
      "Ada Vance",
      "Ada Vance (director)",
    ]);
  });

  it("counts the two credit categories separately", () => {
    const p = new URLSearchParams("actor=12&actor=13&director=12");
    expect(activeCount(p, "actor")).toBe(2);
    expect(activeCount(p, "director")).toBe(1);
  });

  it("ignores a status value it cannot name", () => {
    const p = new URLSearchParams("status=banana");
    expect(activePills(p, { facets })).toHaveLength(0);
  });

  it("has no pills for an unfiltered library", () => {
    expect(activePills(new URLSearchParams("sort=year&q=alien"), { facets })).toEqual(
      [],
    );
  });
});

describe("the count on a category button", () => {
  it("counts each value of a repeatable filter", () => {
    const p = new URLSearchParams("genre=Drama&genre=Comedy&genre=Horror");
    expect(activeCount(p, "genre")).toBe(3);
  });

  // Status is single-valued, so it is one or none however it is spelled.
  it("counts status as at most one", () => {
    expect(activeCount(new URLSearchParams("status=unmatched"), "status")).toBe(1);
    expect(activeCount(new URLSearchParams(), "status")).toBe(0);
  });
});

describe("clearing", () => {
  /*
   * The list that "Clear all" iterates must cover every category the bar can
   * set. A key the bar writes and the clear misses leaves the grid filtered
   * with every control reading off — indistinguishable from a broken grid.
   */
  it("covers every category the bar can set", () => {
    for (const c of FILTER_CATEGORIES) {
      expect(FILTER_PARAM_KEYS).toContain(c.key);
    }
    // Plus the watched toggle, which is a filter without being a category.
    expect(FILTER_PARAM_KEYS).toContain("watched");
  });
});

describe("searching years", () => {
  /*
   * Prefix rather than substring, and this is the case that decides it: a
   * substring match on "99" also returns 1994, because the digits are in there.
   * Typing more would then widen the list, which is not what typing means.
   */
  it("narrows by prefix, so 199 is the nineties and 99 is nothing", () => {
    expect(matchYears(facets.years, "199")).toEqual([1994, 1999]);
    expect(matchYears(facets.years, "1994")).toEqual([1994]);
    expect(matchYears(facets.years, "99")).toEqual([]);
  });

  it("returns everything for an empty query", () => {
    expect(matchYears(facets.years, "  ")).toEqual(facets.years);
  });

  it("returns nothing rather than everything for a year not present", () => {
    expect(matchYears(facets.years, "1975")).toEqual([]);
  });
});

describe("rating thresholds", () => {
  /*
   * The case this exists for. A library topping out at 8.4 must not offer 9+,
   * which is a control guaranteed to return an empty grid — the same "lies
   * about what it does" failure the empty facets already avoid.
   */
  it("drops steps the library cannot reach, and keeps the one below", () => {
    const steps = ratingSteps(RATING_THRESHOLDS, 8.4);
    expect(steps).not.toContain(9);
    expect(steps[0]).toBe(8);
  });

  it("offers nothing at all when nothing is rated", () => {
    expect(ratingSteps(RATING_THRESHOLDS, 0)).toEqual([]);
  });

  it("shows a rating pill as a floor", () => {
    const p = new URLSearchParams("min_rating=8");
    expect(activePills(p, { facets })[0].label).toBe("8+");
  });

  // Single-valued, like status: one threshold or none.
  it("counts a rating as at most one", () => {
    expect(activeCount(new URLSearchParams("min_rating=8"), "min_rating")).toBe(1);
  });
});

describe("collections", () => {
  it("names a collection pill from the facets, with no second request", () => {
    const p = new URLSearchParams("collection=7");
    expect(activePills(p, { facets })[0].label).toBe("A Franchise");
  });

  // Matched anywhere in the name: a franchise is as often remembered by its
  // second word as its first.
  it("matches a word inside the name", () => {
    expect(matchCollections(facets.collections, "franch")).toHaveLength(1);
    expect(matchCollections(facets.collections, "a ")).toHaveLength(2);
  });
});
