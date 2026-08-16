import { describe, it, expect } from "vitest";
import { episodeLabel, episodeCode } from "./format";

/*
 * An episode has to say which episode it is.
 *
 * On Continue Watching a tile read "Stray Dog Strut · 1998", which looks like an
 * obscure film and is Cowboy Bebop S01E02. The tile had the series and the
 * numbers in hand and showed the year instead.
 */
describe("episodeLabel", () => {
  it("names the series and the episode", () => {
    expect(
      episodeLabel({ kind: "episode", series: "Cowboy Bebop", season: 1, episode: 2 }),
    ).toBe("Cowboy Bebop · S01E02");
  });

  it("pads to two digits, so episodes sort and read alike", () => {
    expect(
      episodeLabel({ kind: "episode", series: "Andor", season: 2, episode: 11 }),
    ).toBe("Andor · S02E11");
  });

  // A season-zero special is still an episode, and 0 is a real season number —
  // a falsy check here would drop it back to showing a year.
  it("handles season zero", () => {
    expect(
      episodeLabel({ kind: "episode", series: "Doctor Who", season: 0, episode: 1 }),
    ).toBe("Doctor Who · S00E01");
  });

  it("falls back to the code alone when the series is unknown", () => {
    expect(
      episodeLabel({ kind: "episode", series: null, season: 1, episode: 2 }),
    ).toBe("S01E02");
  });

  // Not an episode: the caller shows the year instead, and null is how it knows.
  it("is null for anything that is not an episode", () => {
    expect(
      episodeLabel({ kind: "episode", series: "X", season: null, episode: null }),
    ).toBeNull();
    expect(episodeLabel({})).toBeNull();
  });

  /*
   * The one that shipped wrong.
   *
   * A music track carries its album in `series`, its disc in `season` and its
   * track number in `episode` (ADR 0024), so "has a season and an episode" is
   * true of every tagged song. Pearl Jam's *Black* was labelled S00E33 and
   * Garbage's *#1 Crush* S00E14 — disc zero, track thirty-three — on the
   * profile page and on any tile that showed a track.
   */
  it("refuses to label a music track as an episode", () => {
    const track = {
      kind: "track",
      series: "The Very Best Of Pearl Jam",
      season: 0,
      episode: 33,
    };
    expect(episodeLabel(track)).toBeNull();
    expect(episodeCode(track)).toBeNull();
  });

  // A film has no numbers at all, but the kind gate is what makes that certain
  // rather than incidental.
  it("refuses anything that is not an episode, whatever its columns say", () => {
    for (const kind of ["movie", "track", "photo", "show", "season"]) {
      expect(episodeCode({ kind, season: 1, episode: 2 })).toBeNull();
    }
    expect(episodeCode({ kind: "episode", season: 1, episode: 2 })).toBe("S01E02");
  });
});
