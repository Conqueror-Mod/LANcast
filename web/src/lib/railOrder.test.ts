import { describe, it, expect } from "vitest";
import { railOrder } from "./railOrder";

const lib = (name: string, kind: string) => ({ name, kind });
const labels = (entries: ReturnType<typeof railOrder<{ name: string; kind: string }>>) =>
  entries.map((e) => (e.type === "game-hub" ? "Game Hub" : e.lib.name));

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
  it("goes Movies, TV Shows, Music, Game Hub, Pictures", () => {
    expect(labels(railOrder(byName, true))).toEqual([
      "Classic Films",
      "Movies",
      "Anime",
      "TV Shows",
      "Music",
      "Game Hub",
      "Family Photos",
    ]);
  });

  it("lists retro libraries through the hub, not beside it", () => {
    expect(labels(railOrder(byName, true))).not.toContain("Retro games");
  });

  it("keeps a retro library in the games slot when there is no hub", () => {
    expect(labels(railOrder(byName, false))).toEqual([
      "Classic Films",
      "Movies",
      "Anime",
      "TV Shows",
      "Music",
      "Retro games",
      "Family Photos",
    ]);
  });

  it("keeps the hub in its place when kinds around it are missing", () => {
    expect(labels(railOrder([lib("Movies", "movie"), lib("Pics", "picture")], true))).toEqual([
      "Movies",
      "Game Hub",
      "Pics",
    ]);
    expect(labels(railOrder([lib("Movies", "movie")], true))).toEqual(["Movies", "Game Hub"]);
    expect(labels(railOrder([], true))).toEqual(["Game Hub"]);
  });

  it("puts a kind it does not know last rather than dropping it", () => {
    expect(labels(railOrder([lib("Odd", "audiobook"), lib("Movies", "movie")], true))).toEqual([
      "Movies",
      "Game Hub",
      "Odd",
    ]);
  });
});
