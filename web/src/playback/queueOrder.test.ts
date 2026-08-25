import { describe, it, expect } from "vitest";
import {
  shuffled,
  shuffledStartingWith,
  startOf,
  shuffleForEntry,
  queueAfterEntry,
  resolvePos,
  nextPos,
  prevPos,
} from "./queueOrder";

const seq = (n: number) => Array.from({ length: n }, (_, i) => i + 1);

describe("shuffled", () => {
  it("keeps every item exactly once", () => {
    const out = shuffled(seq(50));
    expect([...out].sort((a, b) => a - b)).toEqual(seq(50));
  });

  it("does not modify the input", () => {
    const q = seq(10);
    shuffled(q);
    expect(q).toEqual(seq(10));
  });

  /*
   * The report that prompted this file: a six-episode queue advancing
   * 6, 2, 3, 4, 5 with shuffle engaged. A correct shuffle *can* produce a run
   * that looks sequential, so the question is not "did it look odd once" but
   * "is the distribution right".
   *
   * Ten thousand shuffles of six items. If the algorithm were biased towards
   * the identity — the classic off-by-one, picking j from 0..i-1 or from the
   * whole range each time — this is where it shows.
   */
  it("is not biased towards leaving items where they were", () => {
    const N = 6;
    const runs = 10_000;
    let identical = 0;
    // How often each item lands in each position.
    const grid = Array.from({ length: N }, () => new Array(N).fill(0));

    for (let r = 0; r < runs; r++) {
      const out = shuffled(seq(N));
      if (out.every((v, i) => v === i + 1)) identical++;
      out.forEach((v, i) => grid[v - 1][i]++);
    }

    // 1/720 of 10,000 is ~14. Allow a wide margin; the point is to catch a
    // shuffle that returns the input most of the time, not to be a strict
    // statistical test.
    expect(identical).toBeLessThan(200);

    // Every item should reach every position roughly 1/6 of the time (~1667).
    // A biased shuffle leaves the first or last element pinned, which shows up
    // as a near-zero or near-total cell here.
    for (let item = 0; item < N; item++) {
      for (let pos = 0; pos < N; pos++) {
        expect(grid[item][pos]).toBeGreaterThan(900);
        expect(grid[item][pos]).toBeLessThan(2600);
      }
    }
  });

  it("is deterministic given a deterministic source", () => {
    const fixed = () => 0; // always swap with index 0
    expect(shuffled([1, 2, 3, 4], fixed)).toEqual(shuffled([1, 2, 3, 4], fixed));
  });

  it("handles empty and single-item queues", () => {
    expect(shuffled([])).toEqual([]);
    expect(shuffled([7])).toEqual([7]);
  });
});



describe("shuffledStartingWith", () => {
  it("puts the playing track first and keeps everything else", () => {
    const out = shuffledStartingWith(seq(20), 7);
    expect(out[0]).toBe(7);
    expect([...out].sort((a, b) => a - b)).toEqual(seq(20));
  });

  // The fault this exists for: with the current track dropped mid-order,
  // everything before it is unreachable and the queue count is a lie.
  it("leaves nothing stranded ahead of the playing track", () => {
    for (let run = 0; run < 200; run++) {
      const out = shuffledStartingWith(seq(50), 33);
      expect(out.indexOf(33)).toBe(0);
      expect(out.length).toBe(50);
    }
  });

  it("still shuffles the rest", () => {
    let differed = 0;
    for (let run = 0; run < 100; run++) {
      const out = shuffledStartingWith(seq(10), 1);
      if (out.slice(1).some((v, i) => v !== i + 2)) differed++;
    }
    expect(differed).toBeGreaterThan(90);
  });

  it("handles a current item that is not in the queue", () => {
    const out = shuffledStartingWith(seq(5), 99);
    expect(out.length).toBe(5);
    expect(out).not.toContain(99);
  });
});

describe("queueAfterEntry", () => {
  const album = [10, 11, 12, 13];

  // The mini-player round trip: navigate to /watch/11 with no queue at all.
  it("keeps the queue when re-entering with no queue information", () => {
    expect(queueAfterEntry(album, [11], 11)).toEqual(album);
  });

  it("takes a real queue when one is supplied", () => {
    expect(queueAfterEntry(album, [20, 21], 20)).toEqual([20, 21]);
  });

  // Playing a single track that is not part of what is playing replaces the
  // queue — otherwise picking one song would silently inherit the last album.
  it("replaces the queue for a single item from outside it", () => {
    expect(queueAfterEntry(album, [99], 99)).toEqual([99]);
  });

  it("replaces an empty queue", () => {
    expect(queueAfterEntry([], [7], 7)).toEqual([7]);
  });

  // A playlist may contain the same track twice; re-entry must still find it.
  it("keeps a queue that holds the item more than once", () => {
    const withRepeat = [10, 11, 10];
    expect(queueAfterEntry(withRepeat, [10], 10)).toEqual(withRepeat);
  });
});

/*
 * The repeat case these exist for. A playlist holding the same track twice —
 * [10, 11, 10] — was unplayable past position 1 with id-based navigation:
 * indexOf(10) is always 0, so the second copy resumed from the first.
 */
describe("position-based navigation", () => {
  const withRepeat = [10, 11, 10, 12];

  it("tells two copies of the same track apart", () => {
    // Playing the copy at position 2, not the one at position 0.
    expect(nextPos(withRepeat, 2, "off")).toBe(3);
    // The id-based answer would have been position 1 — going backwards.
  });

  it("advances through a repeat instead of looping on it", () => {
    const seen: number[] = [];
    let pos: number | null = 0;
    while (pos !== null && seen.length < 10) {
      seen.push(withRepeat[pos]);
      pos = nextPos(withRepeat, pos, "off");
    }
    expect(seen).toEqual([10, 11, 10, 12]);
  });

  it("stops at the end with repeat off", () => {
    expect(nextPos(withRepeat, 3, "off")).toBeNull();
  });

  it("wraps with repeat all", () => {
    expect(nextPos(withRepeat, 3, "all")).toBe(0);
    expect(prevPos(withRepeat, 0, "all")).toBe(3);
  });

  it("does nothing before the first track without repeat", () => {
    expect(prevPos(withRepeat, 0, "off")).toBeNull();
  });

  describe("resolvePos", () => {
    it("keeps a position that still holds the playing item", () => {
      expect(resolvePos(withRepeat, 2, 10)).toBe(2);
    });

    it("falls back when the order changed under it", () => {
      // Shuffled: the remembered index now holds something else.
      expect(resolvePos([12, 10, 11, 10], 2, 12)).toBe(0);
    });

    it("falls back for an unset position", () => {
      expect(resolvePos(withRepeat, -1, 11)).toBe(1);
    });

    it("returns -1 when the item is not in the order at all", () => {
      expect(resolvePos(withRepeat, 5, 99)).toBe(-1);
    });
  });
});

describe("startOf", () => {
  const seq = (n: number) => Array.from({ length: n }, (_, i) => i + 1);

  it("takes the front when the queue is ordered", () => {
    expect(startOf(seq(20), false)).toBe(1);
  });

  /*
   * The bug this exists for: "Randomize all" always started with the same film.
   * Every caller passed ids[0] and left the randomising to shuffle, but
   * shuffledStartingWith *pins* the id it is given to the front — so the
   * shuffle was real and was a shuffle of positions 2..n.
   */
  it("does not always pick the same id when shuffling", () => {
    const seen = new Set<number>();
    for (let i = 0; i < 200; i++) seen.add(startOf(seq(50), true)!);
    expect(seen.size).toBeGreaterThan(1);
  });

  it("can pick the last id, which a fixed front never could", () => {
    // rand() just under 1 lands on the final index.
    expect(startOf(seq(10), true, () => 0.999)).toBe(10);
  });

  it("picks from the queue and nowhere else", () => {
    const ids = seq(8);
    for (let i = 0; i < 100; i++) expect(ids).toContain(startOf(ids, true)!);
  });

  // An empty queue has no start. Callers already refuse to navigate on one;
  // returning 0 here would have them navigate to /watch/0.
  it("has no answer for an empty queue", () => {
    expect(startOf([], true)).toBeUndefined();
    expect(startOf([], false)).toBeUndefined();
  });
});

/*
 * Reported as "Futurama is not playing in order", and nothing about Futurama
 * was wrong: the episode query and the client's own walk both return S01E01
 * first, and every screen displayed them correctly. Randomize all had turned
 * shuffle on for the session, and Continue handed the player a correctly
 * ordered queue that it then shuffled.
 */
describe("shuffleForEntry", () => {
  it("obeys an explicit request either way", () => {
    expect(shuffleForEntry(true, true, false)).toBe(true);
    expect(shuffleForEntry(false, false, true)).toBe(false);
  });

  // The bug. A caller that supplies a queue is stating an order.
  it("plays a supplied queue in order when shuffle was left on", () => {
    expect(shuffleForEntry(undefined, true, true)).toBe(false);
  });

  /*
   * And the rule this must not break: returning from the mini-player navigates
   * to /watch/{id} with no queue at all, and clearing shuffle there would turn
   * "go back to what is playing" into "stop shuffling", which is a different
   * button.
   */
  it("leaves the session alone when no queue is supplied", () => {
    expect(shuffleForEntry(undefined, false, true)).toBe(true);
    expect(shuffleForEntry(undefined, false, false)).toBe(false);
  });
});
