/*
 * The rail's bucketing, shared by three pages and two implementations.
 *
 * The library grid asks the server for its letters (it pages in, so the S
 * titles may not be loaded); the collections and playlists pages hold
 * everything and filter in memory. Two implementations of the same rule, which
 * is exactly the arrangement that drifts — so the client's has to agree with
 * what the server does, and that is what this pins.
 */
import { describe, it, expect } from "vitest";
import { initialsOf, matchesInitial } from "./AlphabetRail";

describe("initialsOf", () => {
  it("offers only the letters present, # first", () => {
    expect(initialsOf(["Aliens", "Solaris", "300", "Alien"])).toEqual([
      "#",
      "A",
      "S",
    ]);
  });

  it("treats case as not a fact about a title", () => {
    expect(initialsOf(["the matrix", "The Thing"])).toEqual(["T"]);
  });

  // Everything that is not a Latin letter goes in one bucket, the same as the
  // server's. Transliterating would be a second normalizer with opinions about
  // scripts, and this project's rule is that the second one disagrees.
  it("puts numbers, symbols and other scripts under #", () => {
    expect(initialsOf(["300", "(500) Days", "Ran", "Русский"])).toEqual([
      "#",
      "R",
    ]);
  });

  it("says nothing about an empty list", () => {
    expect(initialsOf([])).toEqual([]);
  });
});

describe("matchesInitial", () => {
  it("matches its own bucket, case-insensitively", () => {
    expect(matchesInitial("the matrix", "T")).toBe(true);
    expect(matchesInitial("Solaris", "T")).toBe(false);
  });

  it("puts a numeric title under #", () => {
    expect(matchesInitial("300", "#")).toBe(true);
    expect(matchesInitial("300", "3")).toBe(false);
  });

  // No letter selected is not a filter — it is the whole list.
  it("matches everything when nothing is selected", () => {
    expect(matchesInitial("anything", "")).toBe(true);
  });
});
