import { describe, it, expect } from "vitest";
import { chapterEndAt, fadeGain, sleepClock, sleepLeftMs, startSleep, SLEEP_FADE_MS } from "./sleepTimer";

const MIN = 60_000;
const chapters = [
  { title: "One", start_ms: 0, end_ms: 10 * MIN },
  { title: "Two", start_ms: 10 * MIN, end_ms: 30 * MIN },
];

describe("sleep timer arithmetic", () => {
  it("finds the end of the chapter a place is in", () => {
    expect(chapterEndAt(chapters, 5 * MIN, 30 * MIN)).toBe(10 * MIN);
    // A chapter's end is the next one's start, which belongs to the next one.
    expect(chapterEndAt(chapters, 10 * MIN, 30 * MIN)).toBe(30 * MIN);
    // No chapters: the book is one chapter.
    expect(chapterEndAt([], 5 * MIN, 30 * MIN)).toBe(30 * MIN);
  });

  it("starts a timer of minutes or of a chapter", () => {
    expect(startSleep(15, chapters, 0, 30 * MIN)).toEqual({ kind: "minutes", leftMs: 15 * MIN });
    expect(startSleep("chapter", chapters, 2 * MIN, 30 * MIN)).toEqual({ kind: "chapter", endMs: 10 * MIN });
  });

  it("counts a chapter's remainder in real time, so speed shortens it", () => {
    const t = { kind: "chapter" as const, endMs: 10 * MIN };
    expect(sleepLeftMs(t, 9 * MIN, 1)).toBe(MIN);
    expect(sleepLeftMs(t, 9 * MIN, 2)).toBe(30_000);
    expect(sleepLeftMs(t, 11 * MIN, 1)).toBe(0);
  });

  it("fades only in its last seconds", () => {
    expect(fadeGain(SLEEP_FADE_MS + 1)).toBe(1);
    expect(fadeGain(SLEEP_FADE_MS / 2)).toBe(0.5);
    expect(fadeGain(0)).toBe(0);
  });

  it("shows the time left in minutes and seconds, rounding up", () => {
    expect(sleepClock(15 * MIN)).toBe("15:00");
    expect(sleepClock(61_500)).toBe("1:02");
    expect(sleepClock(400)).toBe("0:01");
  });
});
