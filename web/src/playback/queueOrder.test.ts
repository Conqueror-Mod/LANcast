import { describe, it, expect } from "vitest";
import {
  shuffled,
  shuffledStartingWith,
  nextAfter,
  prevBefore,
  queueAfterEntry,
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

describe("nextAfter", () => {
  it("follows the order it is given, not the natural order", () => {
    const order = [5, 1, 4, 2, 3];
    expect(nextAfter(order, 5, "off")).toBe(1);
    expect(nextAfter(order, 1, "off")).toBe(4);
    expect(nextAfter(order, 2, "off")).toBe(3);
  });

  it("stops at the end when repeat is off", () => {
    expect(nextAfter([1, 2, 3], 3, "off")).toBeNull();
  });

  it("wraps to the front when repeat is all", () => {
    expect(nextAfter([5, 1, 4], 4, "all")).toBe(5);
  });

  // An item that is not in the queue at all — which happens if the queue is
  // replaced while something from the old one is still playing.
  it("returns null for an item outside the order", () => {
    expect(nextAfter([1, 2, 3], 99, "all")).toBeNull();
  });
});

describe("prevBefore", () => {
  it("steps back through the given order", () => {
    expect(prevBefore([5, 1, 4], 4)).toBe(1);
  });

  it("does nothing at the start", () => {
    expect(prevBefore([5, 1, 4], 5)).toBeNull();
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
