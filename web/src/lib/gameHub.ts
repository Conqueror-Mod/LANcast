/*
 * The Game Hub's two halves, decided from what this window and this server
 * have (docs/game-hub-plan.md, from Chris's notes on 2026-10-08).
 *
 * Pure, so every state the notes ask for is tested without a window: no
 * installed games, PC games switched off, a browser that can never start one,
 * no retro library, a retro library with no ROM database, several.
 */
import { sortGames, visibleGames, type GameRow, type GamesResult } from "./games";

export type PCHalf =
  /** Not the desktop app: a phone or a browser tab cannot start a PC game. */
  | { kind: "not-desktop" }
  /** The desktop app, with the PC Games list switched off in Settings. */
  | { kind: "off" }
  /** Still asking the launchers. */
  | { kind: "loading" }
  /** Asked, and there are none. */
  | { kind: "none" }
  /** The listing failed; the reason is the launcher's own words. */
  | { kind: "error"; reason: string }
  | { kind: "ready"; count: number };

export type RetroHalf<L> =
  | { kind: "loading" }
  /** No retro library on this server. */
  | { kind: "none" }
  /** One or more; `needsDatabase` only when an admin can see it is missing. */
  | { kind: "ready"; libraries: L[]; needsDatabase: boolean };

export function pcHalf(o: {
  supported: boolean;
  enabled: boolean;
  result: GamesResult | undefined;
}): PCHalf {
  if (!o.supported) return { kind: "not-desktop" };
  if (!o.enabled) return { kind: "off" };
  const r = o.result;
  if (!r) return { kind: "loading" };
  switch (r.status) {
    case "disabled":
      return { kind: "off" };
    case "error":
      return { kind: "error", reason: r.error ?? "The launchers could not be read." };
    case "not-installed":
      return { kind: "none" };
  }
  // Hidden games are still installed, and still in the PC Games screen
  // behind its filter; the hub counts what that screen shows by default.
  const count = (r.games ?? []).filter((g) => !g.hidden).length;
  return count > 0 ? { kind: "ready", count } : { kind: "none" };
}

export function retroHalf<L extends { kind: string }>(o: {
  libraries: L[] | undefined;
  /** undefined when not asked (not an admin) or not answered yet. */
  databaseInstalled: boolean | undefined;
}): RetroHalf<L> {
  if (!o.libraries) return { kind: "loading" };
  const retro = o.libraries.filter((l) => l.kind === "retro");
  if (retro.length === 0) return { kind: "none" };
  return { kind: "ready", libraries: retro, needsDatabase: o.databaseInstalled === false };
}

/*
 * Whether the rail offers the hub at all.
 *
 * Where there is something in it — a retro library — or something it can
 * explain: the desktop app, where PC games can be listed, and an admin, who
 * can add a retro library from the hub's own instructions. A member in a
 * browser on a server with no retro library would reach a page of two
 * apologies, so it is not offered there.
 */
export function hubOffered(o: { retroLibraries: number; desktop: boolean; admin: boolean }): boolean {
  return o.retroLibraries > 0 || o.desktop || o.admin;
}

/*
 * The previews: a glimpse of what each half holds, so the hub is not two
 * headings and a lot of field (Chris, looking at stage 1).
 *
 * PC games most recently played first — what somebody is likeliest to go
 * back to, and the preview doubles as the way back in. Hidden games stay
 * hidden, as on the PC Games screen.
 */
export const PREVIEW_COUNT = 12;

export function pcPreview(games: GameRow[] | undefined): GameRow[] {
  return sortGames(visibleGames(games ?? []), "played").slice(0, PREVIEW_COUNT);
}

/*
 * The retro half's selection is the server's random order with a seed that
 * changes once a day: stable while somebody comes and goes from the hub in an
 * evening, and a different handful tomorrow, so the hub shows off the
 * collection rather than the same first twelve by title for ever. Local
 * date, not UTC — a UTC day turns over in the evening in the Americas, which
 * is when this is used (the trap the global rules name).
 */
export function dailySeed(now: Date): number {
  return now.getFullYear() * 10000 + (now.getMonth() + 1) * 100 + now.getDate();
}

// The routes the hub stands for, so the rail can mark it as where you are
// while you are inside either half.
export const GAME_HUB_PATH = "/game-hub";

export function insideHub(pathname: string, retroLibraryIDs: number[]): boolean {
  if (pathname === GAME_HUB_PATH || pathname === "/games" || pathname.startsWith("/games/")) return true;
  const m = /^\/library\/(\d+)(\/|$)/.exec(pathname);
  return !!m && retroLibraryIDs.includes(Number(m[1]));
}
