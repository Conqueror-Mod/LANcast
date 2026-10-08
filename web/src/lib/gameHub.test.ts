import { describe, it, expect } from "vitest";
import { PREVIEW_COUNT, dailySeed, hubOffered, insideHub, pcHalf, pcPreview, retroHalf } from "./gameHub";
import type { GameRow } from "./games";

const game = (over: Partial<GameRow> = {}): GameRow =>
  ({ id: "1", name: "x", size_bytes: 1, last_played: 0, install_path: "", has_poster: false,
    has_header: false, hidden: false, favourite: false, display: "", ...over }) as GameRow;

describe("the PC half", () => {
  it("explains itself outside the desktop app, before anything else", () => {
    expect(pcHalf({ supported: false, enabled: true, result: undefined })).toEqual({ kind: "not-desktop" });
  });
  it("offers Settings when the list is switched off", () => {
    expect(pcHalf({ supported: true, enabled: false, result: undefined })).toEqual({ kind: "off" });
    expect(pcHalf({ supported: true, enabled: true, result: { status: "disabled" } }).kind).toBe("off");
  });
  it("says when no games are installed", () => {
    expect(pcHalf({ supported: true, enabled: true, result: { status: "not-installed" } }).kind).toBe("none");
    expect(pcHalf({ supported: true, enabled: true, result: { status: "ok", games: [] } }).kind).toBe("none");
  });
  it("counts the games the PC Games screen shows, not hidden ones", () => {
    const r = { status: "ok" as const, games: [game(), game({ id: "2" }), game({ id: "3", hidden: true })] };
    expect(pcHalf({ supported: true, enabled: true, result: r })).toEqual({ kind: "ready", count: 2 });
    const allHidden = { status: "ok" as const, games: [game({ hidden: true })] };
    expect(pcHalf({ supported: true, enabled: true, result: allHidden }).kind).toBe("none");
  });
  it("passes a launcher's error on", () => {
    expect(pcHalf({ supported: true, enabled: true, result: { status: "error", error: "Steam unreadable" } })).toEqual({
      kind: "error",
      reason: "Steam unreadable",
    });
  });
});

describe("the retro half", () => {
  const libs = [
    { id: 1, kind: "movie" },
    { id: 4, kind: "retro" },
    { id: 9, kind: "retro" },
  ];
  it("tells somebody with no retro library how to add one", () => {
    expect(retroHalf({ libraries: [{ id: 1, kind: "movie" }], databaseInstalled: true }).kind).toBe("none");
  });
  it("lists every retro library, and only those", () => {
    const h = retroHalf({ libraries: libs, databaseInstalled: true });
    expect(h.kind === "ready" && h.libraries.map((l) => l.id)).toEqual([4, 9]);
  });
  it("nudges for the ROM database only when it is known to be missing", () => {
    const missing = retroHalf({ libraries: libs, databaseInstalled: false });
    const unknown = retroHalf({ libraries: libs, databaseInstalled: undefined });
    expect(missing.kind === "ready" && missing.needsDatabase).toBe(true);
    expect(unknown.kind === "ready" && unknown.needsDatabase).toBe(false);
  });
});

describe("the previews", () => {
  it("shows PC games most recently played first, never-played last, hidden ones not at all", () => {
    const list = [
      game({ id: "old", name: "Old", last_played: 100 }),
      game({ id: "never", name: "Never", last_played: 0 }),
      game({ id: "new", name: "New", last_played: 900 }),
      game({ id: "hid", name: "Hidden", last_played: 999, hidden: true }),
    ];
    expect(pcPreview(list).map((g) => g.id)).toEqual(["new", "old", "never"]);
    expect(pcPreview(undefined)).toEqual([]);
  });

  it("shows at most a preview's worth", () => {
    const many = Array.from({ length: 40 }, (_, i) => game({ id: String(i), name: `G${i}`, last_played: i + 1 }));
    expect(pcPreview(many)).toHaveLength(PREVIEW_COUNT);
  });

  it("changes the retro selection by local day, not by the hour", () => {
    expect(dailySeed(new Date(2026, 9, 8, 0, 1))).toBe(dailySeed(new Date(2026, 9, 8, 23, 59)));
    expect(dailySeed(new Date(2026, 9, 8, 23, 59))).not.toBe(dailySeed(new Date(2026, 9, 9, 0, 1)));
    // Local, so an evening in the Americas is still today.
    expect(dailySeed(new Date(2026, 9, 8, 21, 0))).toBe(20261008);
  });
});

describe("where the hub is offered", () => {
  it("is offered with a retro library, in the desktop app, or to an admin", () => {
    expect(hubOffered({ retroLibraries: 1, desktop: false, admin: false })).toBe(true);
    expect(hubOffered({ retroLibraries: 0, desktop: true, admin: false })).toBe(true);
    expect(hubOffered({ retroLibraries: 0, desktop: false, admin: true })).toBe(true);
    expect(hubOffered({ retroLibraries: 0, desktop: false, admin: false })).toBe(false);
  });
  it("is where you are inside either half", () => {
    expect(insideHub("/game-hub", [4])).toBe(true);
    expect(insideHub("/games", [4])).toBe(true);
    expect(insideHub("/games/steam-123", [4])).toBe(true);
    expect(insideHub("/library/4", [4])).toBe(true);
    expect(insideHub("/library/4/collections", [4])).toBe(true);
    expect(insideHub("/library/1", [4])).toBe(false);
    expect(insideHub("/library/41", [4])).toBe(false);
    expect(insideHub("/", [4])).toBe(false);
  });
});
