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
   * A work removed outright, which the first version of this rule refused.
   *
   * It required a surviving copy, as a proxy for "the drive is still there" —
   * and the first thing it was asked to do was a split-cut film whose halves
   * had both been deleted on purpose, where the proxy said no and the person
   * was right.
   *
   * The guarantee moved rather than went: the server refuses `mode=forget`
   * unless the title's location reads at that moment, so an offline drive is
   * answered with evidence instead of inference. That is stronger than this
   * rule ever was, and it cannot be bypassed by a client.
   */
  it("offers it even when every copy has gone", () => {
    const ms = [member(1, true), member(2, true)];
    expect(forgettable(ms, ms[0])).toBe(true);
    expect(forgettable(ms, ms[1])).toBe(true);
    expect(resolvableByForgetting(ms)).toBe(true);
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
