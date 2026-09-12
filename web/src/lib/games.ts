import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

/*
 * The Games tab's client side (ADR 0066).
 *
 * Everything here goes through the window's bindings rather than the API,
 * because there is no API: a game is installed on this machine, the server has
 * no games table, and a phone could not launch one if it had it. So this file
 * has no fetch in it anywhere.
 *
 * The sorting and filtering below are exported as plain functions on purpose.
 * They are the part with rules in it, and rules are worth testing without a
 * DOM — jsdom performs no layout and proves nothing about a grid, but it can
 * prove that "last played" put the right game first.
 */

export interface GameRow {
  id: string;
  name: string;
  size_bytes: number;
  /** Unix seconds, 0 when never played. */
  last_played: number;
  install_path: string;
  has_poster: boolean;
  has_header: boolean;
  hidden: boolean;
  favourite: boolean;
}

export type GamesStatus = "ok" | "not-installed" | "error" | "disabled";

export interface GamesResult {
  status: GamesStatus;
  games?: GameRow[];
  error?: string;
}

interface Ack {
  ok: boolean;
  error?: string;
}

declare global {
  interface Window {
    lancastGames?: () => Promise<GamesResult>;
    lancastGameArt?: (
      id: string,
      kind: "poster" | "header",
    ) => Promise<{ ok: boolean; uri?: string; error?: string }>;
    lancastLaunchGame?: (id: string) => Promise<Ack>;
    lancastOpenGameFolder?: (id: string) => Promise<Ack>;
    lancastSetGameFlags?: (
      id: string,
      hidden: boolean,
      favourite: boolean,
    ) => Promise<Ack>;
  }
}

/**
 * Whether this window can answer questions about games at all.
 *
 * Feature detection rather than a flag from the server, for the reason the
 * desktop settings section gives: the same server serves this window, a browser
 * tab on the same machine, and a phone in the kitchen, and only one of those is
 * sitting in front of the Steam library.
 */
export function gamesSupported(): boolean {
  return typeof window.lancastGames === "function";
}

/*
 * One key for the list: ["games"].
 *
 * Deliberately without siblings. The most-repeated bug in this project is a
 * write that changes what a list holds and does not invalidate that list, and
 * its nastiest form is a key that *looks* invalidated: ["items", "infinite"] is
 * reached by ["items"] and ["items-infinite"] is not, and the difference is
 * invisible at the call site. So the art cache below is keyed "game-art" rather
 * than ["games", "art"], which would be swept away by every launch.
 */
const GAMES_KEY = ["games"] as const;

/*
 * Whether the Games tab is switched on, which is a different question from
 * whether this window could show one.
 *
 * Its own key rather than a corner of ["games"]: the list is invalidated by
 * every launch and every hide, and sweeping the setting away with it would
 * re-read the preferences file for no reason each time. It is also not a
 * sibling — ["games-enabled"] is not reached by ["games"] — which is the
 * distinction that has bitten this project four times.
 */
export const GAMES_ENABLED_KEY = ["games-enabled"] as const;

export function useGamesTab(): boolean {
  const { data } = useQuery({
    queryKey: GAMES_ENABLED_KEY,
    queryFn: async () => {
      const state = window.lancastDesktopState;
      if (!state) return false;
      const s = (await state()) as { games?: boolean };
      return !!s.games;
    },
    enabled: gamesSupported(),
  });
  return !!data;
}

export function useGames() {
  return useQuery<GamesResult>({
    queryKey: GAMES_KEY,
    queryFn: async () => window.lancastGames!(),
    enabled: gamesSupported(),
    // A scan reads a few dozen small files, so it is cheap to repeat — but it
    // is disk, not memory, and nothing about an installed library changes while
    // somebody is looking at it. Refetched on demand instead.
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  });
}

/**
 * One image, fetched only for a game that has one.
 *
 * Separate from the list because a two-hundred-game library would otherwise be
 * twenty megabytes of base64 in a single binding call, most of it for tiles
 * nobody scrolled to.
 */
export function useGameArt(
  id: string,
  kind: "poster" | "header",
  has: boolean,
) {
  return useQuery({
    queryKey: ["game-art", id, kind],
    queryFn: async () => (await window.lancastGameArt!(id, kind)).uri ?? "",
    enabled: gamesSupported() && has && !!id,
    // The bytes on disk do not change while the app is open, and re-reading
    // them per tile per scroll would be the expensive mistake this split exists
    // to avoid.
    staleTime: Infinity,
    gcTime: 10 * 60_000,
  });
}

export function useLaunchGame() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await window.lancastLaunchGame!(id);
      if (!res.ok) throw new Error(res.error ?? "it could not be started");
      return res;
    },
    /*
     * Launching changes the list.
     *
     * Not obviously — nothing is installed or removed — but Steam writes the
     * last-played time, and that is a column somebody may be sorting by. Ask
     * what a person could be *looking at* that this changes, not what it
     * writes.
     */
    onSuccess: () => qc.invalidateQueries({ queryKey: GAMES_KEY }),
  });
}

export function useOpenGameFolder() {
  return useMutation({
    mutationFn: async (id: string) => {
      const res = await window.lancastOpenGameFolder!(id);
      if (!res.ok) throw new Error(res.error ?? "it could not be opened");
      return res;
    },
  });
}

export function useSetGameFlags() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (v: {
      id: string;
      hidden: boolean;
      favourite: boolean;
    }) => {
      const res = await window.lancastSetGameFlags!(v.id, v.hidden, v.favourite);
      if (!res.ok) throw new Error(res.error ?? "it could not be saved");
      return res;
    },
    // Hiding and favouriting both change which tiles the grid draws and where.
    onSuccess: () => qc.invalidateQueries({ queryKey: GAMES_KEY }),
  });
}

/** Rescan is the same invalidation the mutations do, with a button on it. */
export function useRescanGames() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: GAMES_KEY });
}

export type GameSort = "name" | "played" | "size";

/**
 * Sorts a copy, never in place: the array belongs to the query cache, and
 * sorting it where it lies would reorder what other components are rendering
 * without telling React anything changed.
 */
export function sortGames(list: GameRow[], by: GameSort): GameRow[] {
  const out = [...list];
  const byName = (a: GameRow, b: GameRow) =>
    a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  switch (by) {
    case "played":
      // Never played sorts last rather than first: 0 is "no answer", not "a
      // very long time ago", and a wall of untouched games above the one
      // somebody played this morning is not a recency list.
      return out.sort((a, b) => {
        if (!a.last_played && !b.last_played) return byName(a, b);
        if (!a.last_played) return 1;
        if (!b.last_played) return -1;
        return b.last_played - a.last_played;
      });
    case "size":
      return out.sort((a, b) => b.size_bytes - a.size_bytes || byName(a, b));
    default:
      return out.sort(byName);
  }
}

/**
 * What the grid should draw: the name filter applied, and hidden games left out
 * unless they were asked for.
 */
export function visibleGames(
  list: GameRow[],
  opts: { filter?: string; showHidden?: boolean } = {},
): GameRow[] {
  const needle = (opts.filter ?? "").trim().toLowerCase();
  return list.filter((g) => {
    if (g.hidden && !opts.showHidden) return false;
    return !needle || g.name.toLowerCase().includes(needle);
  });
}

/** How many games are hidden, so the toggle can say what it would reveal. */
export function hiddenCount(list: GameRow[]): number {
  return list.filter((g) => g.hidden).length;
}
