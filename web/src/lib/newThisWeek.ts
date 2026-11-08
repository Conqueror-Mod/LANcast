import type { Item } from "@/api/types";

/*
 * "New this week" (ADR 0118): what reached this server in the last seven days,
 * and nothing older.
 *
 * A strict window, chosen over "the newest twenty, whenever they came": a
 * heading that says "this week" over last spring's films is untrue, and the
 * library pages already sort by date added for anybody who wants the rest. The
 * cost is that a quiet week has no section at all -- which is the honest
 * answer, and Home draws no empty frames.
 *
 * Seven days as a duration, not as calendar dates: nothing here is turned into
 * a day, so the UTC-is-tomorrow trap has nowhere to happen. `added_at` is unix
 * seconds, when the scan first saw the file.
 */
export const NEW_WINDOW_SECONDS = 7 * 24 * 60 * 60;

/** The items added within the window, in the order they came. */
export function addedThisWeek(items: Item[] | undefined, nowSeconds = Date.now() / 1000): Item[] {
  const since = nowSeconds - NEW_WINDOW_SECONDS;
  return (items ?? []).filter((i) => !i.missing && (i.added_at ?? 0) >= since);
}

const KIND_NAMES: Record<string, string> = {
  movie: "Films",
  collection: "Films",
  show: "Shows",
  episode: "Shows",
  book: "Audiobooks",
  comic: "Comics",
  rom: "Games",
  game: "Games",
};

/*
 * The row's name, from what is in it: "Films · Shows", "Comics".
 *
 * Named by its contents rather than fixed, because the video window holds
 * every kind that is not music or a picture -- a fixed "Films and shows" would
 * be wrong the week the only new thing is a comic.
 */
export function rowLabel(items: Item[], fallback: string): string {
  const names: string[] = [];
  for (const it of items) {
    const name = KIND_NAMES[it.kind];
    if (name && !names.includes(name)) names.push(name);
  }
  return names.length > 0 ? names.join(" · ") : fallback;
}
