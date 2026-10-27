import { describe, it, expect } from "vitest";
import { chapterAt, nextChapterStart, previousChapterStart, type ChapterMark } from "./chapters";

const film: ChapterMark[] = [
  { start_ms: 0, end_ms: 300_000, title: "Opening" },
  { start_ms: 300_000, end_ms: 900_000, title: "Middle" },
  { start_ms: 900_000, end_ms: 1_200_000, title: "End" },
];

describe("chapterAt", () => {
  it("names the chapter playing now", () => {
    expect(chapterAt(film, 0)?.title).toBe("Opening");
    expect(chapterAt(film, 300)?.title).toBe("Middle");
    expect(chapterAt(film, 1_000)?.title).toBe("End");
  });
  it("is undefined before the first chapter and with no chapters", () => {
    expect(chapterAt([{ start_ms: 5_000, end_ms: 10_000, title: "x" }], 1)).toBeUndefined();
    expect(chapterAt([], 100)).toBeUndefined();
  });
});

describe("next chapter", () => {
  it("goes to the next start", () => {
    expect(nextChapterStart(film, 10)).toBe(300);
    expect(nextChapterStart(film, 300.2)).toBe(900);
  });
  it("does not land on the chapter just jumped to", () => {
    // Just after a jump to 300 the playhead reads a hair past it.
    expect(nextChapterStart(film, 300.1)).toBe(900);
  });
  it("has nowhere to go in the last chapter", () => {
    expect(nextChapterStart(film, 950)).toBeUndefined();
  });
});

describe("previous chapter", () => {
  it("restarts the current chapter when well into it", () => {
    expect(previousChapterStart(film, 400)).toBe(300);
  });
  it("goes to the one before within three seconds of a start", () => {
    expect(previousChapterStart(film, 301)).toBe(0);
  });
  it("stays at the start of the first chapter", () => {
    expect(previousChapterStart(film, 1)).toBe(0);
  });
  it("does nothing with no chapters", () => {
    expect(previousChapterStart([], 100)).toBeUndefined();
  });
});
