import { describe, it, expect } from "vitest";
import {
  bookTime,
  chapterAt,
  clock,
  locate,
  nextChapterStart,
  prevChapterStart,
  resumeFrom,
  speedOf,
  type BookChapter,
  type BookPart,
} from "./bookTimeline";

const MIN = 60_000;
// Two parts, ten and twenty minutes: the server's own layout.
const parts: BookPart[] = [
  { id: 11, title: "Part 1", start_ms: 0, duration_ms: 10 * MIN },
  { id: 12, title: "Part 2", start_ms: 10 * MIN, duration_ms: 20 * MIN },
];
const chapters: BookChapter[] = [
  { title: "Part 1", start_ms: 0, end_ms: 10 * MIN },
  { title: "Part 2", start_ms: 10 * MIN, end_ms: 30 * MIN },
];

describe("a book is one clock over its parts", () => {
  it("finds the part and the offset for a place", () => {
    expect(locate(parts, 5 * MIN)).toEqual({ index: 0, offsetMs: 5 * MIN });
    expect(locate(parts, 15 * MIN)).toEqual({ index: 1, offsetMs: 5 * MIN });
    // A part's first instant belongs to that part, not the one before.
    expect(locate(parts, 10 * MIN)).toEqual({ index: 1, offsetMs: 0 });
    // Past the end is the end of the last part, never a part that is not there.
    expect(locate(parts, 99 * MIN)).toEqual({ index: 1, offsetMs: 20 * MIN });
    expect(locate(parts, -5)).toEqual({ index: 0, offsetMs: 0 });
  });

  it("turns an offset in a part back into the book's clock", () => {
    expect(bookTime(parts, 1, 5 * MIN)).toBe(15 * MIN);
    expect(bookTime(parts, 0, 0)).toBe(0);
  });
});

describe("chapters", () => {
  it("knows which chapter a place is in", () => {
    expect(chapterAt(chapters, 0)).toBe(0);
    expect(chapterAt(chapters, 12 * MIN)).toBe(1);
    expect(chapterAt([], 12 * MIN)).toBe(-1);
  });

  it("skips to the next chapter, and has none after the last", () => {
    expect(nextChapterStart(chapters, 2 * MIN)).toBe(10 * MIN);
    expect(nextChapterStart(chapters, 12 * MIN)).toBeNull();
  });

  it("goes back to this chapter's start, or the one before near it", () => {
    expect(prevChapterStart(chapters, 15 * MIN)).toBe(10 * MIN);
    expect(prevChapterStart(chapters, 10 * MIN + 1_000)).toBe(0);
    expect(prevChapterStart(chapters, 1_000)).toBe(0);
  });
});

describe("where a book begins", () => {
  it("resumes a few seconds before the saved place", () => {
    expect(resumeFrom({ position_ms: 15 * MIN, watched: false, duration_ms: 30 * MIN })).toBe(15 * MIN - 5_000);
  });
  it("begins a finished or unstarted book at the beginning", () => {
    expect(resumeFrom({ position_ms: 29 * MIN, watched: true, duration_ms: 30 * MIN })).toBe(0);
    expect(resumeFrom({ position_ms: 0, watched: false, duration_ms: 30 * MIN })).toBe(0);
    expect(resumeFrom({ position_ms: 2_000, watched: false, duration_ms: 30 * MIN })).toBe(0);
  });
  it("plays a book never given a speed at 1×, and its own otherwise", () => {
    expect(speedOf({})).toBe(1);
    expect(speedOf({ speed: 1.5 })).toBe(1.5);
    expect(speedOf({ speed: 9 })).toBe(1);
  });
});

it("writes a clock the way a player does", () => {
  expect(clock(65_000)).toBe("1:05");
  expect(clock(3_723_000)).toBe("1:02:03");
});
