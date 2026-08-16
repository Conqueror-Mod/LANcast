import { describe, it, expect } from "vitest";
import { episodeLabel } from "./format";

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
      episodeLabel({ series: "Cowboy Bebop", season: 1, episode: 2 }),
    ).toBe("Cowboy Bebop · S01E02");
  });

  it("pads to two digits, so episodes sort and read alike", () => {
    expect(episodeLabel({ series: "Andor", season: 2, episode: 11 })).toBe(
      "Andor · S02E11",
    );
  });

  // A season-zero special is still an episode, and 0 is a real season number —
  // a falsy check here would drop it back to showing a year.
  it("handles season zero", () => {
    expect(episodeLabel({ series: "Doctor Who", season: 0, episode: 1 })).toBe(
      "Doctor Who · S00E01",
    );
  });

  it("falls back to the code alone when the series is unknown", () => {
    expect(episodeLabel({ series: null, season: 1, episode: 2 })).toBe("S01E02");
  });

  // Not an episode: the caller shows the year instead, and null is how it knows.
  it("is null for anything that is not an episode", () => {
    expect(episodeLabel({ series: "X", season: null, episode: null })).toBeNull();
    expect(episodeLabel({})).toBeNull();
  });
});
