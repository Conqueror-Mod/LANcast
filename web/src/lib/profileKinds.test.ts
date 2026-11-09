import { describe, expect, it } from "vitest";
import { kindCards } from "./profileKinds";

const k = (kind: string, started: number, finished: number, ms: number) => ({
  kind,
  started,
  finished,
  viewings: finished,
  watched_ms: ms,
});

describe("profile totals by kind", () => {
  it("puts films and episodes together, songs and books apart, most time first", () => {
    const cards = kindCards([
      k("track", 208, 126, 49_000_000),
      k("movie", 426, 266, 2_520_000_000),
      k("episode", 79, 72, 202_000_000),
      k("book_part", 12, 10, 60_000_000),
    ]);
    expect(cards.map((c) => c.id)).toEqual(["watched", "books", "listened"]);
    expect(cards[0].ms).toBe(2_722_000_000);
    expect(cards[0].note).toBe("266 films · 72 episodes finished");
    expect(cards[2].note).toBe("208 songs played");
    expect(cards[1].note).toBe("10 chapters finished");
  });

  it("counts a comic without a time, after the timed cards", () => {
    const cards = kindCards([k("comic", 1, 1, 0), k("movie", 1, 0, 600_000)]);
    expect(cards.map((c) => c.id)).toEqual(["watched", "read"]);
    expect(cards[1].ms).toBeNull();
    expect(cards[0].note).toBe("nothing finished yet");
  });

  it("keeps a reset's banked time as its own card, last", () => {
    const cards = kindCards([k("track", 1, 1, 200_000)], k("", 14, 7, 98_400_000));
    expect(cards.map((c) => c.id)).toEqual(["listened", "earlier"]);
    expect(cards[1].ms).toBe(98_400_000);
  });

  it("counts whole books and older chapter rows in their own words", () => {
    const cards = kindCards([k("book", 3, 2, 9_000_000), k("book_part", 5, 4, 3_000_000)]);
    expect(cards).toHaveLength(1);
    expect(cards[0].ms).toBe(12_000_000);
    expect(cards[0].note).toBe("2 books · 4 chapters finished");
  });

  it("has nothing to show for nothing played", () => {
    expect(kindCards([], undefined)).toEqual([]);
  });
});
