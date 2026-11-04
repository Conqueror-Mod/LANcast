/*
 * A book's timeline (ADR 0116 step 2), as pure functions.
 *
 * A book is one item however many files it is. The server lays its parts end
 * to end (`GET /api/items/{id}/book`); everything here turns a place on that
 * one clock into a part and an offset, and back, so the player never thinks in
 * parts and a book never becomes a playlist of its chapters.
 */
import type { components } from "@/api/schema";

export type BookInfo = components["schemas"]["BookInfo"];
export type BookPart = components["schemas"]["BookPart"];
export type BookChapter = components["schemas"]["Chapter"];

/** The speeds a book offers, 0.75× to 2× in quarters (ADR 0116). */
export const BOOK_SPEEDS = [0.75, 1, 1.25, 1.5, 1.75, 2] as const;

/** The skips every book player shares: back 15 seconds, forward 30. */
export const SKIP_BACK_MS = 15_000;
export const SKIP_FORWARD_MS = 30_000;

/**
 * How far before the saved place a book resumes, so the last sentence heard
 * is heard again (ADR 0116).
 */
export const RESUME_REWIND_MS = 5_000;

/** Where in which part a place on the book's clock falls. */
export function locate(parts: BookPart[], ms: number): { index: number; offsetMs: number } {
  if (parts.length === 0) return { index: 0, offsetMs: 0 };
  const at = Math.max(0, ms);
  for (let i = parts.length - 1; i >= 0; i--) {
    if (at >= parts[i].start_ms) {
      const p = parts[i];
      // Past the end of the last part is the end of it, not beyond.
      return { index: i, offsetMs: Math.min(at - p.start_ms, Math.max(0, p.duration_ms)) };
    }
  }
  return { index: 0, offsetMs: 0 };
}

/** The place on the book's clock of an offset into one part. */
export function bookTime(parts: BookPart[], index: number, offsetMs: number): number {
  const p = parts[index];
  return p ? p.start_ms + Math.max(0, offsetMs) : 0;
}

/** The chapter a place falls in, by index, or -1 when there are none. */
export function chapterAt(chapters: BookChapter[], ms: number): number {
  let found = -1;
  for (let i = 0; i < chapters.length; i++) {
    if (ms >= chapters[i].start_ms) found = i;
    else break;
  }
  return chapters.length > 0 && found < 0 ? 0 : found;
}

/** The start of the next chapter, or null at the last. */
export function nextChapterStart(chapters: BookChapter[], ms: number): number | null {
  const i = chapterAt(chapters, ms);
  return i >= 0 && i + 1 < chapters.length ? chapters[i + 1].start_ms : null;
}

/**
 * Where "previous chapter" goes: the start of this chapter when more than a
 * few seconds into it, else the start of the one before — the way every
 * player's back button treats a track.
 */
export function prevChapterStart(chapters: BookChapter[], ms: number, graceMs = 3_000): number {
  const i = chapterAt(chapters, ms);
  if (i < 0) return 0;
  const start = chapters[i].start_ms;
  if (ms - start > graceMs || i === 0) return start;
  return chapters[i - 1].start_ms;
}

/**
 * Where a book begins when Play is pressed. Resume is the default, not an
 * offer (ADR 0116): the saved place, a few seconds back. A finished book, or
 * one never started, begins at the beginning.
 */
export function resumeFrom(info: Pick<BookInfo, "position_ms" | "watched" | "duration_ms">): number {
  if (info.watched || info.position_ms <= 0) return 0;
  if (info.duration_ms > 0 && info.position_ms >= info.duration_ms) return 0;
  return Math.max(0, info.position_ms - RESUME_REWIND_MS);
}

/** The speed a book plays at: its own, else 1× (ADR 0116, answer 9). */
export function speedOf(info: Pick<BookInfo, "speed">): number {
  const s = info.speed;
  return s !== undefined && s >= BOOK_SPEEDS[0] && s <= BOOK_SPEEDS[BOOK_SPEEDS.length - 1] ? s : 1;
}

/** "1:02:03" or "4:05", for a place on a clock in milliseconds. */
export function clock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}
