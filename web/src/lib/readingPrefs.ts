import { useCallback } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/api/client";
import type { ReadingPreferences, ReadingPreferencesUpdate } from "@/api/types";
import { BOOK_SPEEDS } from "@/playback/bookTimeline";
import { SLEEP_MINUTES, type SleepChoice } from "@/playback/sleepTimer";

/*
 * Your reading and listening defaults, kept on your account (ADR 0120,
 * settings-3-plan step 4).
 *
 * Defaults, not settings. A book with a speed of its own keeps it; a sleep
 * timer set from a default can be changed or cleared in the book player like
 * any other; a comic still turns the way it always did. These say only what
 * happens when nothing more specific has been chosen.
 */

export const READING_PREFS_KEY = ["reading-prefs"] as const;

const EMPTY: ReadingPreferences = {
  account: false,
  book_speed: null,
  book_sleep: null,
  comic_layout: null,
  comic_fit: null,
};

function fetchReadingPrefs(signal?: AbortSignal): Promise<ReadingPreferences> {
  return apiGet<ReadingPreferences>("/api/profile/reading", signal).then((r) => ({
    account: r?.account === true,
    book_speed: r?.book_speed ?? null,
    book_sleep: r?.book_sleep ?? null,
    comic_layout: r?.comic_layout ?? null,
    comic_fit: r?.comic_fit ?? null,
  }));
}

export const readingPrefsQuery = {
  queryKey: READING_PREFS_KEY,
  queryFn: ({ signal }: { signal?: AbortSignal }) => fetchReadingPrefs(signal),
  staleTime: 60_000,
};

export function useReadingPrefs() {
  return useQuery(readingPrefsQuery);
}

/**
 * The defaults as they stand, for code that runs once rather than renders:
 * the book player when a book starts. A failure is "nothing chosen", so a
 * book never refuses to play over a preference.
 */
export function useReadingPrefsNow(): () => Promise<ReadingPreferences> {
  const qc = useQueryClient();
  // Stable, because the book player's callbacks depend on it.
  return useCallback(() => qc.fetchQuery(readingPrefsQuery).catch(() => EMPTY), [qc]);
}

export function useSetReadingPrefs() {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: READING_PREFS_KEY,
    mutationFn: (u: ReadingPreferencesUpdate) =>
      apiPost<ReadingPreferences>("/api/profile/reading", u, "PUT"),
    // Shown at once, so a select does not snap back while the save is out.
    onMutate: async (u) => {
      await qc.cancelQueries({ queryKey: READING_PREFS_KEY });
      const before = qc.getQueryData<ReadingPreferences>(READING_PREFS_KEY);
      if (before) qc.setQueryData<ReadingPreferences>(READING_PREFS_KEY, { ...before, ...u });
      return { before };
    },
    onError: (_e, _u, ctx) => {
      if (ctx?.before) qc.setQueryData(READING_PREFS_KEY, ctx.before);
    },
    // An earlier answer while a later save is still out would put the old
    // value back on screen until the later one lands.
    onSuccess: (r) => {
      if (qc.isMutating({ mutationKey: READING_PREFS_KEY }) > 1) return;
      qc.setQueryData(READING_PREFS_KEY, r);
    },
  });
}

/** The account's default book speed, or 1x where none is chosen or it is not one the player offers. */
export function defaultBookSpeed(p: Pick<ReadingPreferences, "book_speed"> | null | undefined): number {
  const s = p?.book_speed;
  return typeof s === "number" && (BOOK_SPEEDS as readonly number[]).includes(s) ? s : 1;
}

/** The sleep timer a book starts with, or null for none. */
export function defaultSleep(p: Pick<ReadingPreferences, "book_sleep"> | null | undefined): SleepChoice | null {
  const v = p?.book_sleep;
  if (!v || v === "off") return null;
  if (v === "chapter") return "chapter";
  const n = Number(v);
  return (SLEEP_MINUTES as readonly number[]).includes(n) ? (n as SleepChoice) : null;
}

export type ComicLayout = "single" | "spread";
export type ComicFit = "page" | "width";

export function comicLayoutOf(p: Pick<ReadingPreferences, "comic_layout"> | null | undefined): ComicLayout {
  return p?.comic_layout === "spread" ? "spread" : "single";
}

export function comicFitOf(p: Pick<ReadingPreferences, "comic_fit"> | null | undefined): ComicFit {
  return p?.comic_fit === "width" ? "width" : "page";
}
