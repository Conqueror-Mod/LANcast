/*
 * Chapter arithmetic for the scrub bar and the chapter keys.
 *
 * Pure functions of a sorted chapter list and a time in seconds, so the rules
 * are tested without a player. Times are seconds because the player's are;
 * the server's are milliseconds because everything on the wire is.
 */

export interface ChapterMark {
  start_ms: number;
  end_ms: number;
  title: string;
}

// chapterAt is the chapter playing at `seconds`, or undefined before the first.
export function chapterAt(chapters: ChapterMark[], seconds: number): ChapterMark | undefined {
  const ms = seconds * 1000;
  let found: ChapterMark | undefined;
  for (const c of chapters) {
    if (c.start_ms <= ms) found = c;
    else break;
  }
  return found;
}

// nextChapterStart is where "next chapter" goes, in seconds, or undefined in
// the last chapter. A chapter starting within half a second of now is the one
// just jumped to, not the next one, or two presses would land in one place.
export function nextChapterStart(chapters: ChapterMark[], seconds: number): number | undefined {
  const ms = seconds * 1000 + 500;
  const next = chapters.find((c) => c.start_ms > ms);
  return next ? next.start_ms / 1000 : undefined;
}

/*
 * previousChapterStart is where "previous chapter" goes, in seconds.
 *
 * The rule every player uses: more than three seconds into a chapter, back to
 * its start; within three seconds, to the one before. Without the grace a
 * person could never get past the chapter they had just jumped to.
 */
export function previousChapterStart(chapters: ChapterMark[], seconds: number): number | undefined {
  const ms = seconds * 1000;
  const current = chapterAt(chapters, seconds);
  if (!current) return undefined;
  if (ms - current.start_ms > 3000) return current.start_ms / 1000;
  const i = chapters.indexOf(current);
  return i > 0 ? chapters[i - 1].start_ms / 1000 : current.start_ms / 1000;
}
