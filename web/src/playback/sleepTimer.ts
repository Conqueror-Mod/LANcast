/*
 * The book player's sleep timer (ADR 0116 step 3, answer 8): 15, 30, 45 or 60
 * minutes, or the end of the chapter, fading out rather than stopping dead.
 *
 * Pure, so the arithmetic is tested without an audio element.
 *
 * Minutes count only while the book plays. A timer that ran on through a pause
 * would be spent by the time somebody came back from making tea, which is the
 * moment they set it for.
 *
 * The end of a chapter is a place on the book's clock, so how long is left
 * depends on the speed: ten seconds of book at 2x is five seconds of night.
 */
import type { BookChapter } from "./bookTimeline";

export const SLEEP_MINUTES = [15, 30, 45, 60] as const;
export type SleepChoice = (typeof SLEEP_MINUTES)[number] | "chapter";

/** How long the fade takes, in real time. */
export const SLEEP_FADE_MS = 10_000;

export type SleepTimer =
  /** Real time left, counted down only while the book plays. */
  | { kind: "minutes"; leftMs: number }
  /** Stop when the book's clock reaches this place. */
  | { kind: "chapter"; endMs: number };

/**
 * Where the chapter playing at `ms` ends. With no chapters the book is one
 * long chapter, so the end is the book's end.
 */
export function chapterEndAt(chapters: BookChapter[], ms: number, durationMs: number): number {
  const c = chapters.find((ch) => ms >= ch.start_ms && ms < ch.end_ms);
  return c ? c.end_ms : durationMs;
}

/** A new timer for a choice, starting from `bookMs`. */
export function startSleep(
  choice: SleepChoice,
  chapters: BookChapter[],
  bookMs: number,
  durationMs: number,
): SleepTimer {
  if (choice === "chapter") {
    return { kind: "chapter", endMs: chapterEndAt(chapters, bookMs, durationMs) };
  }
  return { kind: "minutes", leftMs: choice * 60_000 };
}

/** Real time until the timer stops the book. */
export function sleepLeftMs(t: SleepTimer, bookMs: number, speed: number): number {
  if (t.kind === "minutes") return Math.max(0, t.leftMs);
  return Math.max(0, (t.endMs - bookMs) / (speed > 0 ? speed : 1));
}

/** The volume, as a share of the listener's own, with `leftMs` to go. */
export function fadeGain(leftMs: number): number {
  if (leftMs >= SLEEP_FADE_MS) return 1;
  if (leftMs <= 0) return 0;
  return leftMs / SLEEP_FADE_MS;
}

/** "14:32" for the countdown beside the menu. */
export function sleepClock(ms: number): string {
  const s = Math.ceil(ms / 1000);
  const m = Math.floor(s / 60);
  return `${m}:${String(s % 60).padStart(2, "0")}`;
}
