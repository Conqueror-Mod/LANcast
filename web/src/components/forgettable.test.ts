import { describe, expect, it } from "vitest";

import { forgettable, resolvableByForgetting } from "./forgettable";
import type { CollisionMember } from "@/api/types";

function member(id: number, missing: boolean): CollisionMember {
  return {
    id,
    title: `t${id}`,
    path: `/x/${id}.mkv`,
    size_bytes: 100,
    library_id: 1,
    missing,
  };
}

/*
 * The rule that decides whether a collision can be resolved at all.
 *
 * ADR 0042 says LANcast never merges, ranks or deletes, and that still holds:
 * nothing here decides between two files. What it allows is forgetting a row
 * whose file is *gone*, which is not a choice between two things — it is the
 * removal of a leftover.
 */
describe("forgettable", () => {
  it("offers nothing for a file that is still on disk", () => {
    const ms = [member(1, false), member(2, false)];
    expect(forgettable(ms, ms[0])).toBe(false);
  });

  it("offers to forget the row whose file has gone", () => {
    const ms = [member(1, true), member(2, false)];
    expect(forgettable(ms, ms[0])).toBe(true);
    expect(forgettable(ms, ms[1])).toBe(false);
  });

  /*
   * The assertion that keeps this from becoming the hole in "scanning marks
   * missing, never deletes".
   *
   * An unmounted drive takes every member of a collision missing at once. If
   * the offer appeared then, a person tidying up would delete both halves of a
   * work that is entirely intact and merely offline.
   */
  it("offers nothing when every copy has gone", () => {
    const ms = [member(1, true), member(2, true)];
    expect(forgettable(ms, ms[0])).toBe(false);
    expect(forgettable(ms, ms[1])).toBe(false);
    expect(resolvableByForgetting(ms)).toBe(false);
  });

  it("still offers when one of three has gone", () => {
    const ms = [member(1, true), member(2, false), member(3, false)];
    expect(forgettable(ms, ms[0])).toBe(true);
    expect(resolvableByForgetting(ms)).toBe(true);
  });

  it("says a collision of present files is not resolvable this way", () => {
    // Two real files both on disk is the case ADR 0042 was written for, and
    // nothing here may resolve it.
    expect(resolvableByForgetting([member(1, false), member(2, false)])).toBe(
      false,
    );
  });
});
