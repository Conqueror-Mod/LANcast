import { describe, it, expect } from "vitest";
import { offersShuffle, playAllLabel } from "./playAll";

describe("Play all or Play", () => {
  it("says Play for exactly one", () => {
    expect(playAllLabel(1)).toBe("Play");
    expect(offersShuffle(1)).toBe(false);
  });
  it("says Play all for several, or when the count is not known", () => {
    for (const n of [2, 40, 0, undefined, null]) {
      expect(playAllLabel(n)).toBe("Play all");
      expect(offersShuffle(n)).toBe(true);
    }
  });
});
