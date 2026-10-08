import { describe, it, expect } from "vitest";
import { railOrder } from "./railOrder";

const lib = (name: string, kind: string) => ({ name, kind });
const labels = (entries: ReturnType<typeof railOrder<{ name: string; kind: string }>>) =>
  entries.map((e) => (e.type === "pc-games" ? "PC Games" : e.lib.name));

// The server's order is by name; the rail's is by kind.
const byName = [
  lib("Anime", "show"),
  lib("Classic Films", "movie"),
  lib("Family Photos", "picture"),
  lib("Movies", "movie"),
  lib("Music", "music"),
  lib("Retro games", "retro"),
  lib("TV Shows", "show"),
];

describe("railOrder", () => {
  it("goes Movies, TV Shows, Music, Retro Games, PC Games, Pictures", () => {
    expect(labels(railOrder(byName, true))).toEqual([
      "Classic Films",
      "Movies",
      "Anime",
      "TV Shows",
      "Music",
      "Retro games",
      "PC Games",
      "Family Photos",
    ]);
  });

  it("leaves PC Games out when it is not shown", () => {
    expect(labels(railOrder(byName, false))).not.toContain("PC Games");
  });

  it("keeps PC Games in its place when kinds around it are missing", () => {
    expect(labels(railOrder([lib("Movies", "movie"), lib("Pics", "picture")], true))).toEqual([
      "Movies",
      "PC Games",
      "Pics",
    ]);
    expect(labels(railOrder([lib("Movies", "movie")], true))).toEqual(["Movies", "PC Games"]);
    expect(labels(railOrder([], true))).toEqual(["PC Games"]);
  });

  it("puts a kind it does not know last rather than dropping it", () => {
    expect(labels(railOrder([lib("Odd", "audiobook"), lib("Movies", "movie")], true))).toEqual([
      "Movies",
      "PC Games",
      "Odd",
    ]);
  });
});
