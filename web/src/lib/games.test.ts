import { describe, expect, it } from "vitest";
import {
  hiddenCount,
  sortGames,
  visibleGames,
  type GameRow,
} from "@/lib/games";

// Invented titles and app ids throughout: a test fixture has no business
// naming anybody's real library.
function game(p: Partial<GameRow> & { id: string; name: string }): GameRow {
  return {
    size_bytes: 0,
    last_played: 0,
    install_path: "",
    has_poster: false,
    has_header: false,
    hidden: false,
    favourite: false,
    // Unanswered, which is the state these sorting and filtering rules should
    // be indifferent to — a game nobody has chosen a screen for still sorts and
    // filters like any other.
    display: "",
    ...p,
  };
}

const list: GameRow[] = [
  game({ id: "1", name: "Zephyr Drift", size_bytes: 900, last_played: 1000 }),
  game({ id: "2", name: "alpha Protocol", size_bytes: 300, last_played: 0 }),
  game({ id: "3", name: "Meridian", size_bytes: 700, last_played: 5000 }),
];

describe("sortGames", () => {
  it("sorts by name regardless of case", () => {
    // Lower-cased titles would otherwise land after every capital letter,
    // which reads as a broken sort rather than as a convention.
    expect(sortGames(list, "name").map((g) => g.name)).toEqual([
      "alpha Protocol",
      "Meridian",
      "Zephyr Drift",
    ]);
  });

  it("puts the most recently played first and the never-played last", () => {
    // 0 is "no answer", not "a very long time ago". A wall of untouched games
    // above the one played this morning is not a recency list.
    expect(sortGames(list, "played").map((g) => g.id)).toEqual(["3", "1", "2"]);
  });

  it("sorts by size, largest first", () => {
    expect(sortGames(list, "size").map((g) => g.id)).toEqual(["1", "3", "2"]);
  });

  it("does not sort the caller's array in place", () => {
    // It belongs to the query cache: reordering it where it lies would change
    // what other components render without telling React anything happened.
    const before = list.map((g) => g.id);
    sortGames(list, "size");
    expect(list.map((g) => g.id)).toEqual(before);
  });
});

describe("visibleGames", () => {
  const withHidden = [...list, game({ id: "4", name: "Hidden Thing", hidden: true })];

  it("leaves hidden games out by default", () => {
    expect(visibleGames(withHidden).map((g) => g.id)).not.toContain("4");
  });

  it("includes them when they are asked for", () => {
    expect(
      visibleGames(withHidden, { showHidden: true }).map((g) => g.id),
    ).toContain("4");
  });

  it("filters on part of a name, case-insensitively", () => {
    expect(visibleGames(withHidden, { filter: "ERID" }).map((g) => g.id)).toEqual([
      "3",
    ]);
  });

  it("treats a blank filter as no filter", () => {
    expect(visibleGames(list, { filter: "   " })).toHaveLength(3);
  });

  it("counts what hiding would reveal", () => {
    expect(hiddenCount(withHidden)).toBe(1);
    expect(hiddenCount(list)).toBe(0);
  });
});
