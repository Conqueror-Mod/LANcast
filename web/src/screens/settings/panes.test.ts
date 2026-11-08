import { describe, expect, it } from "vitest";
import { PANE_GROUPS } from "../settingsIndex";
import { PANE_VIEWS } from "./panes";

/*
 * The nav and the views are two lists. A pane in the nav with no view opens an
 * empty column; a view with no pane is a screen nobody can reach. Either is the
 * kind of fault that passes every other test, because nothing renders wrong —
 * something just is not there.
 */
describe("settings panes", () => {
  const navIDs = PANE_GROUPS.flatMap((g) => g.panes.map((p) => p.id));

  it("gives every pane in the nav a view", () => {
    expect(navIDs.filter((id) => !PANE_VIEWS[id])).toEqual([]);
  });

  it("has no view that the nav cannot reach", () => {
    expect(Object.keys(PANE_VIEWS).filter((id) => !navIDs.includes(id))).toEqual([]);
  });
});
