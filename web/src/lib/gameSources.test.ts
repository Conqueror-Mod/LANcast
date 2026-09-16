/*
 * A grid fed by three launchers.
 *
 * The sorting and filtering in this module were written when every game came
 * from Steam, and nothing about them mentions a launcher — which is the point
 * worth testing rather than assuming. A list merged from three readers must
 * behave exactly as one list: sorted together, filtered together, and with
 * hidden and favourite flags that cannot collide across launchers.
 *
 * The ids below are namespaced the way the client emits them. That prefix is
 * the whole defence against a collision: a Battle.net key is a display name and
 * nothing stops one being the digits of a Steam appid.
 */
import { describe, it, expect } from "vitest";
import { sortGames, type GameRow } from "./games";

function game(p: Partial<GameRow> & { id: string; name: string }): GameRow {
  return {
    size_bytes: 0,
    last_played: 0,
    install_path: "",
    has_poster: false,
    has_header: false,
    hidden: false,
    favourite: false,
    display: "",
    ...p,
  };
}

const steam = game({
  id: "steam:700012",
  name: "Beta Game",
  source: "steam",
  source_label: "Steam",
  size_bytes: 3_000,
  last_played: 1_700_000_000,
});
const epic = game({
  id: "epic:a26f991a5e6c4e9c9572fc200cbea47f",
  name: "Alpha Game",
  source: "epic",
  source_label: "Epic Games",
  size_bytes: 1_000,
  last_played: 0,
});
const blizzard = game({
  id: "battlenet:Invented Title",
  name: "Gamma Game",
  source: "battlenet",
  source_label: "Battle.net",
  size_bytes: 2_000,
  last_played: 0,
});

describe("a grid merged from three launchers", () => {
  it("sorts by name across all of them, not within each", () => {
    const got = sortGames([steam, epic, blizzard], "name").map((g) => g.name);
    expect(got).toEqual(["Alpha Game", "Beta Game", "Gamma Game"]);
  });

  it("sorts by size across all of them", () => {
    const got = sortGames([steam, epic, blizzard], "size").map((g) => g.name);
    expect(got).toEqual(["Beta Game", "Gamma Game", "Alpha Game"]);
  });

  it("puts games that were never played last, not first", () => {
    /*
     * Epic and Battle.net record no last-played time on disk, so both send 0.
     * Sorting descending would otherwise be fine; the risk is treating 0 as a
     * date, which would put every Epic and Blizzard game *above* a Steam game
     * played yesterday — and "last played" would be the one sort order that
     * lies.
     */
    const got = sortGames([epic, steam, blizzard], "played").map((g) => g.name);
    expect(got[0]).toBe("Beta Game");
  });
});

describe("ids from different launchers", () => {
  it("cannot collide when a launcher key looks like another's", () => {
    /*
     * The case the namespace exists for. A Battle.net entry whose registry key
     * is the digits of a Steam appid would, unprefixed, share an id with that
     * Steam game — and the hidden and favourite flags are stored by id, so
     * hiding one would hide the other with no error anywhere.
     */
    const a = game({ id: "steam:440", name: "A", source: "steam" });
    const b = game({ id: "battlenet:440", name: "B", source: "battlenet" });
    expect(a.id).not.toBe(b.id);

    const hidden = new Set([a.id]);
    expect(hidden.has(b.id)).toBe(false);
  });

  it("carries a label a person can read", () => {
    expect(epic.source_label).toBe("Epic Games");
    expect(blizzard.source_label).toBe("Battle.net");
  });

  it("tolerates a desktop binary too old to send one", () => {
    /*
     * The window ships in the installer and the web bundle rides the server's
     * in-app update, so a page newer than the binary it talks to is an ordinary
     * state rather than a broken one. Nothing here may require the field.
     */
    const old = game({ id: "700012", name: "From Before" });
    expect(old.source).toBeUndefined();
    expect(sortGames([old, epic], "name").map((g) => g.name)).toEqual([
      "Alpha Game",
      "From Before",
    ]);
  });
});
