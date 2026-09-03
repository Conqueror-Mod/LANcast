import { describe, it, expect } from "vitest";
import { collapsePeople } from "./collapsePeople";
import type { FacePerson } from "@/api/hooks";

/*
 * One person, one tile.
 *
 * Reported from a real library: "Georgia Bowles" appeared three times, with
 * counts of 80, 1 and 1 — the result of accepting near-miss suggestions, which
 * names a group rather than merging it. The page read as three different
 * people who happened to share a name.
 */

function person(id: number, name: string | null, count: number, cover?: number): FacePerson {
  return { id, name, name_locked: false, count, cover_face_id: cover } as FacePerson;
}

describe("collapsing people by name", () => {
  it("shows one row for a person spread across several groups", () => {
    const got = collapsePeople([
      person(1, "Georgia Bowles", 80, 11),
      person(2, "Georgia Bowles", 1, 22),
      person(3, "Georgia Bowles", 1, 33),
    ]);
    expect(got).toHaveLength(1);
    expect(got[0].name).toBe("Georgia Bowles");
    expect(got[0].count).toBe(82);
  });

  it("keeps every group so renaming can reach them all", () => {
    // Renaming one of three would split her back into two people, which is
    // this function's own bug arriving by a different route.
    const got = collapsePeople([
      person(1, "Georgia Bowles", 80),
      person(2, "Georgia Bowles", 1),
      person(3, "Georgia Bowles", 1),
    ]);
    expect(got[0].clusterIDs).toEqual([1, 2, 3]);
  });

  it("takes the cover face from the largest group", () => {
    // The one with the most evidence behind it, not whichever arrived first.
    const got = collapsePeople([
      person(2, "Georgia Bowles", 1, 22),
      person(1, "Georgia Bowles", 80, 11),
    ]);
    expect(got[0].coverFaceID).toBe(11);
    expect(got[0].clusterIDs[0]).toBe(1);
  });

  it("falls back to a smaller group's face when the largest has none", () => {
    const got = collapsePeople([
      person(1, "Georgia Bowles", 80),
      person(2, "Georgia Bowles", 1, 22),
    ]);
    expect(got[0].coverFaceID).toBe(22);
  });

  /*
   * The subtlety worth a test of its own: unnamed groups all share the same
   * name — none — and merging on that would put every unidentified face in the
   * library into one tile, which is the opposite of what the page is for.
   */
  it("never merges the unnamed together", () => {
    const got = collapsePeople([
      person(1, null, 3),
      person(2, null, 2),
      person(3, "", 1),
    ]);
    expect(got).toHaveLength(3);
    expect(got.every((p) => p.name === null)).toBe(true);
  });

  it("treats a name that differs only by spacing as the same person", () => {
    const got = collapsePeople([
      person(1, "Georgia Bowles", 5),
      person(2, "  Georgia Bowles  ", 3),
    ]);
    expect(got).toHaveLength(1);
    expect(got[0].count).toBe(8);
  });

  it("keeps different people apart", () => {
    const got = collapsePeople([
      person(1, "Georgia Bowles", 5),
      person(2, "Carl Bowles", 3),
      person(3, null, 1),
    ]);
    expect(got).toHaveLength(3);
  });

  it("handles an empty library", () => {
    expect(collapsePeople([])).toEqual([]);
  });
});
