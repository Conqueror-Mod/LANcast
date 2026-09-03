import type { FacePerson } from "@/api/hooks";

/*
 * One person, one tile — however many groups carry their name.
 *
 * Naming does not merge groups, deliberately: a re-cluster seeds named groups
 * as anchors and never renames or dissolves one, which is the locked-fields
 * rule applied to identity. The consequence is that accepting three near-miss
 * suggestions leaves four groups all called Georgia Bowles, and the page
 * listed her four times — with counts of 80, 1, 1 and 1, as though the machine
 * had found four different people who happened to share a name.
 *
 * So the collapsing belongs here, on the way to the screen, and *not* in the
 * data. Merging the groups themselves would throw away which faces the machine
 * grouped together and which a person put there, and that distinction is what
 * makes a re-cluster safe to run.
 */

/** A row on the people page: one person, and the groups making them up. */
export type CollapsedPerson = {
  /** Stable across renders: the name, or the single group's id. */
  key: string;
  name: string | null;
  /** Every face under this person, across all their groups. */
  count: number;
  coverFaceID?: number;
  /**
   * The groups collapsed into this row, largest first.
   *
   * Plural because renaming has to reach all of them. Renaming one of four
   * groups called Georgia Bowles would split her back into two people, which
   * is the bug this function exists to prevent, arriving by a different route.
   */
  clusterIDs: number[];
};

/*
 * Unnamed groups are never collapsed, and that is the whole subtlety.
 *
 * They all share the same name — none — and merging on that would put every
 * unidentified face in the library into a single tile, which is the opposite
 * of a page whose job is to get them told apart.
 */
export function collapsePeople(people: FacePerson[]): CollapsedPerson[] {
  const byName = new Map<string, FacePerson[]>();
  const out: CollapsedPerson[] = [];

  for (const p of people) {
    const name = p.name?.trim() ?? "";
    if (name === "") {
      out.push({
        key: `c${p.id}`,
        name: null,
        count: p.count,
        coverFaceID: p.cover_face_id,
        clusterIDs: [p.id],
      });
      continue;
    }
    const group = byName.get(name);
    if (group) group.push(p);
    else byName.set(name, [p]);
  }

  for (const [name, group] of byName) {
    // Largest first, so the cover face comes from the group with the most
    // evidence behind it rather than from whichever row arrived first — and
    // so renaming targets the biggest group, which is the one a re-cluster
    // will draw faces toward.
    const sorted = [...group].sort((a, b) => b.count - a.count);
    const cover = sorted.find((p) => p.cover_face_id != null);
    out.push({
      key: `n${name}`,
      name,
      count: sorted.reduce((n, p) => n + p.count, 0),
      coverFaceID: cover?.cover_face_id,
      clusterIDs: sorted.map((p) => p.id),
    });
  }

  return out;
}
