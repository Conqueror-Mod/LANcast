/*
 * The book player (ADR 0116 step 2).
 *
 * Its own small player beside PlaybackProvider rather than a mode of it. The
 * film and music player is built around one file per item: its position, its
 * resume, its progress writes and its queue all assume the element's clock is
 * the item's clock. A book of seven parts breaks that assumption everywhere at
 * once, and bending all of it would put every film and song at risk to play
 * books. So a book gets one audio element, one clock over all its parts, and
 * progress written against the book and never a part — the ADR's trap, "a
 * book must never become a playlist of its chapters", answered by
 * construction: there is no queue here to put chapters in.
 *
 * The two players never sound at once. Starting a book stops whatever the
 * main player holds; the main player starting pauses the book.
 */
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { apiGet, apiSend } from "@/api/client";
import { invalidateWatchState, useItem } from "@/api/hooks";
import type { Item } from "@/api/types";
import { usePlaybackIfAny } from "./PlaybackProvider";
import { defaultBookSpeed, defaultSleep, useReadingPrefsNow } from "@/lib/readingPrefs";
import { startedFloorMs } from "./resumePoint";
import {
  bookTime,
  chapterAt,
  locate,
  nextChapterStart,
  prevChapterStart,
  resumeFrom,
  SKIP_BACK_MS,
  SKIP_FORWARD_MS,
  speedOf,
  type BookChapter,
  type BookInfo,
} from "./bookTimeline";
import {
  chapterEndAt,
  fadeGain,
  sleepLeftMs,
  startSleep,
  type SleepChoice,
  type SleepTimer,
} from "./sleepTimer";

export interface BookPlayerState {
  /** The book loaded, or 0. */
  bookID: number;
  book: Item | undefined;
  info: BookInfo | null;
  playing: boolean;
  /** The place on the book's clock, in milliseconds. */
  timeMs: number;
  speed: number;
  chapter: BookChapter | null;
  /** Something to say instead of playing: a book still being measured. */
  note: string;
  /** Play a book: from `fromMs` when given, else where this account left it. */
  play: (bookID: number, fromMs?: number) => Promise<void>;
  toggle: () => void;
  seekTo: (ms: number) => void;
  skip: (deltaMs: number) => void;
  skipBack: () => void;
  skipForward: () => void;
  nextChapter: () => void;
  prevChapter: () => void;
  setSpeed: (rate: number) => void;
  stop: () => void;
  /** The sleep timer's choice, or null when none is set. */
  sleep: SleepChoice | null;
  /** Real time until the sleep timer stops the book, or null. */
  sleepLeftMs: number | null;
  /** Set the sleep timer, or clear it with null. */
  setSleep: (choice: SleepChoice | null) => void;
}

/** How often the sleep timer counts down and steps its fade. */
const SLEEP_TICK_MS = 250;

const Ctx = createContext<BookPlayerState | null>(null);

export function useBookPlayer(): BookPlayerState | null {
  return useContext(Ctx);
}

/** How often the place is written while a book plays. */
const SAVE_EVERY_MS = 5_000;

// One element for the life of the page, made on first use. jsdom has Audio.
let sharedAudio: HTMLAudioElement | null = null;
function bookAudio(): HTMLAudioElement {
  if (!sharedAudio) {
    sharedAudio = new Audio();
    sharedAudio.preload = "auto";
  }
  return sharedAudio;
}

/** For tests: a fresh element per test. */
export function resetBookAudioForTests(a?: HTMLAudioElement): void {
  sharedAudio = a ?? null;
}

export function BookProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const main = usePlaybackIfAny();
  const readingPrefs = useReadingPrefsNow();
  // setSleep is declared below play; play reaches it through this.
  const applySleep = useRef<(choice: SleepChoice) => void>(() => {});
  const [bookID, setBookID] = useState(0);
  const [info, setInfo] = useState<BookInfo | null>(null);
  const [playing, setPlaying] = useState(false);
  const [timeMs, setTimeMs] = useState(0);
  const [speed, setSpeedState] = useState(1);
  const [note, setNote] = useState("");
  const { data: book } = useItem(bookID);

  // Read by the element's listeners, which are attached once.
  const infoRef = useRef<BookInfo | null>(null);
  const bookRef = useRef(0);
  const partRef = useRef(-1);
  const speedRef = useRef(1);
  const timeRef = useRef(0);
  const lastSaved = useRef(-1);
  // True while a new part's source is being swapped in: the element reports
  // the old one stopping, which is not the listener pausing.
  const swapping = useRef(false);

  // The sleep timer. The ref is what the element's listeners and the tick
  // read; the state is what the bar shows.
  const [sleep, setSleepChoice] = useState<SleepChoice | null>(null);
  const [sleepLeft, setSleepLeft] = useState<number | null>(null);
  const sleepRef = useRef<SleepTimer | null>(null);
  // The listener's volume before a fade began, put back after it.
  const sleepVolume = useRef(1);

  /** Clear the timer and put the volume back, whether it fired or not. */
  const endSleep = useCallback(() => {
    if (sleepRef.current) bookAudio().volume = sleepVolume.current;
    sleepRef.current = null;
    setSleepChoice(null);
    setSleepLeft(null);
  }, []);

  /** The timer has run out: pause, which saves the place, then tidy up. */
  const fireSleep = useCallback(() => {
    const a = bookAudio();
    if (!a.paused) a.pause();
    endSleep();
  }, [endSleep]);
  // For the element's listeners, which are attached once.
  const fireSleepRef = useRef(fireSleep);
  fireSleepRef.current = fireSleep;

  const save = useCallback(
    (opts: { finished?: boolean; settle?: boolean } = {}) => {
      const id = bookRef.current;
      const i = infoRef.current;
      if (!id || !i) return;
      const at = opts.finished ? i.duration_ms : Math.floor(timeRef.current);
      // A place this early is not a bookmark (resumePoint.ts): opening a book
      // to look at it must not put it on the Continue shelf.
      if (!opts.finished && at < startedFloorMs(i.duration_ms)) return;
      if (!opts.finished && Math.abs(at - lastSaved.current) < 500 && !opts.settle) return;
      lastSaved.current = at;
      void apiSend(`/api/items/${id}/progress`, "PUT", {
        position_ms: at,
        watched: !!opts.finished,
      })
        .then(() => {
          // What a page shows changed — the place, the shelf, a finish — but
          // only worth redrawing when the listening stops, not every tick.
          if (opts.settle || opts.finished) {
            qc.invalidateQueries({ queryKey: ["book", id] });
            invalidateWatchState(qc);
          }
        })
        .catch(() => {});
    },
    [qc],
  );

  // Load the part a place falls in, and seek within it.
  const loadAt = useCallback((ms: number, autoplay: boolean) => {
    const i = infoRef.current;
    if (!i || i.parts.length === 0) return;
    const a = bookAudio();
    const { index, offsetMs } = locate(i.parts, ms);
    timeRef.current = bookTime(i.parts, index, offsetMs);
    setTimeMs(timeRef.current);
    if (index === partRef.current && a.src) {
      a.currentTime = offsetMs / 1000;
      if (autoplay) void a.play().catch(() => {});
      return;
    }
    partRef.current = index;
    swapping.current = true;
    a.src = `/api/stream/${i.parts[index].id}`;
    const onMeta = () => {
      a.removeEventListener("loadedmetadata", onMeta);
      swapping.current = false;
      a.currentTime = offsetMs / 1000;
      // A new source resets the rate; the book's own is put back.
      a.playbackRate = speedRef.current;
      if (autoplay) void a.play().catch(() => {});
    };
    a.addEventListener("loadedmetadata", onMeta);
    a.load();
  }, []);

  // The element's events, attached once.
  useEffect(() => {
    const a = bookAudio();
    const onTime = () => {
      const i = infoRef.current;
      if (!i || partRef.current < 0) return;
      timeRef.current = bookTime(i.parts, partRef.current, a.currentTime * 1000);
      setTimeMs(timeRef.current);
      // The end of the chapter is a place, so it is checked against every
      // step of the clock rather than the coarser tick, which at 2x could
      // carry the book a second into the next chapter.
      const t = sleepRef.current;
      if (t?.kind === "chapter" && timeRef.current >= t.endMs) fireSleepRef.current();
    };
    const onPlay = () => setPlaying(true);
    const onPause = () => {
      if (swapping.current) return;
      setPlaying(false);
      save({ settle: true });
    };
    const onEnded = () => {
      const i = infoRef.current;
      if (!i) return;
      const next = partRef.current + 1;
      if (next < i.parts.length) {
        // The next part, from its start: one book, not the next item.
        loadAt(i.parts[next].start_ms, true);
        return;
      }
      setPlaying(false);
      timeRef.current = i.duration_ms;
      setTimeMs(i.duration_ms);
      save({ finished: true });
      // The book ended before the timer did; nothing is left for it to stop.
      if (sleepRef.current) fireSleepRef.current();
    };
    a.addEventListener("timeupdate", onTime);
    a.addEventListener("play", onPlay);
    a.addEventListener("pause", onPause);
    a.addEventListener("ended", onEnded);
    return () => {
      a.removeEventListener("timeupdate", onTime);
      a.removeEventListener("play", onPlay);
      a.removeEventListener("pause", onPause);
      a.removeEventListener("ended", onEnded);
    };
  }, [loadAt, save]);

  // The place, every few seconds while a book plays.
  useEffect(() => {
    if (!playing) return;
    const t = window.setInterval(() => save(), SAVE_EVERY_MS);
    return () => window.clearInterval(t);
  }, [playing, save]);

  /*
   * The sleep timer's countdown and fade, while the book plays.
   *
   * Paused, nothing counts down: the effect ends and the next one starts its
   * clock afresh. The countdown on screen moves in whole seconds, so the bar
   * redraws once a second rather than four times.
   */
  const sleepOn = sleep !== null;
  useEffect(() => {
    if (!sleepOn || !playing) return;
    let last = performance.now();
    const tick = () => {
      const t = sleepRef.current;
      if (!t) return;
      const now = performance.now();
      if (t.kind === "minutes") t.leftMs -= now - last;
      last = now;
      const left = sleepLeftMs(t, timeRef.current, speedRef.current);
      if (left <= 0) {
        fireSleep();
        return;
      }
      bookAudio().volume = sleepVolume.current * fadeGain(left);
      setSleepLeft((prev) =>
        prev !== null && Math.ceil(prev / 1000) === Math.ceil(left / 1000) ? prev : left,
      );
    };
    const id = window.setInterval(tick, SLEEP_TICK_MS);
    return () => window.clearInterval(id);
  }, [sleepOn, playing, fireSleep]);

  // And once more if the page goes away mid-sentence.
  useEffect(() => {
    const onHide = () => {
      const id = bookRef.current;
      const i = infoRef.current;
      if (!id || !i || timeRef.current < startedFloorMs(i.duration_ms)) return;
      void fetch(`/api/items/${id}/progress`, {
        method: "PUT",
        keepalive: true,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ position_ms: Math.floor(timeRef.current), watched: false }),
      }).catch(() => {});
    };
    window.addEventListener("pagehide", onHide);
    return () => window.removeEventListener("pagehide", onHide);
  }, []);

  // Never two things sounding at once: the main player starting pauses the book.
  const mainPlaying = main?.playing ?? false;
  useEffect(() => {
    if (mainPlaying && !bookAudio().paused) bookAudio().pause();
  }, [mainPlaying]);

  const play = useCallback(
    async (id: number, fromMs?: number) => {
      setNote("");
      // Asked alongside the book rather than after it, so a slow answer never
      // holds the book up.
      const prefsAsked = readingPrefs();
      let i: BookInfo;
      try {
        i = await apiGet<BookInfo>(`/api/items/${id}/book`);
      } catch {
        setNote("This book could not be opened.");
        return;
      }
      if (!i.ready || i.parts.length === 0) {
        setNote("This book is still being measured. Try again in a minute.");
        return;
      }
      // Leaving one book for another keeps the first one's place.
      if (bookRef.current && bookRef.current !== id) save({ settle: true });
      // A timer belongs to the book it was set on.
      if (bookRef.current !== id) endSleep();
      if (main?.itemID) main.stop();
      infoRef.current = i;
      bookRef.current = id;
      partRef.current = -1;
      lastSaved.current = -1;
      const prefs = await prefsAsked;
      /*
       * The book's own speed, else the account's default (settings-3-plan
       * step 4), else 1x. The default is not written to the book: until the
       * listener chooses, the book has no speed of its own, so changing the
       * default changes every book nobody has set.
       */
      const rate = i.speed != null ? speedOf(i) : defaultBookSpeed(prefs);
      speedRef.current = rate;
      setSpeedState(rate);
      setInfo(i);
      setBookID(id);
      loadAt(fromMs ?? resumeFrom(i), true);
      // A sleep timer from the account's default, when none is running: one
      // set by hand, or one still counting on this book, is left alone.
      const sleepDefault = defaultSleep(prefs);
      if (sleepDefault && !sleepRef.current) applySleep.current(sleepDefault);
    },
    [loadAt, main, save, endSleep, readingPrefs],
  );

  const toggle = useCallback(() => {
    const a = bookAudio();
    if (!bookRef.current) return;
    if (a.paused) {
      if (main?.playing) main.togglePlay();
      void a.play().catch(() => {});
    } else a.pause();
  }, [main]);

  const seekTo = useCallback(
    (ms: number) => {
      const i = infoRef.current;
      if (!i) return;
      loadAt(Math.min(Math.max(0, ms), i.duration_ms), !bookAudio().paused);
      // "End of chapter" means the chapter you are in, so a skip into another
      // one moves the stop to that chapter's end.
      const t = sleepRef.current;
      if (t?.kind === "chapter") {
        t.endMs = chapterEndAt(i.chapters, timeRef.current, i.duration_ms);
        setSleepLeft(sleepLeftMs(t, timeRef.current, speedRef.current));
      }
    },
    [loadAt],
  );

  const setSleep = useCallback(
    (choice: SleepChoice | null) => {
      const i = infoRef.current;
      if (choice === null || !i) {
        endSleep();
        return;
      }
      // A second choice replaces the first, from the volume the listener set.
      if (!sleepRef.current) sleepVolume.current = bookAudio().volume;
      else bookAudio().volume = sleepVolume.current;
      const t = startSleep(choice, i.chapters, timeRef.current, i.duration_ms);
      sleepRef.current = t;
      setSleepChoice(choice);
      setSleepLeft(sleepLeftMs(t, timeRef.current, speedRef.current));
    },
    [endSleep],
  );
  applySleep.current = setSleep;
  const skip = useCallback((d: number) => seekTo(timeRef.current + d), [seekTo]);
  const skipBack = useCallback(() => skip(-SKIP_BACK_MS), [skip]);
  const skipForward = useCallback(() => skip(SKIP_FORWARD_MS), [skip]);
  const nextChapter = useCallback(() => {
    const i = infoRef.current;
    if (!i) return;
    const n = nextChapterStart(i.chapters, timeRef.current);
    if (n !== null) seekTo(n);
  }, [seekTo]);
  const prevChapter = useCallback(() => {
    const i = infoRef.current;
    if (!i) return;
    seekTo(prevChapterStart(i.chapters, timeRef.current));
  }, [seekTo]);

  const setSpeed = useCallback((rate: number) => {
    speedRef.current = rate;
    setSpeedState(rate);
    bookAudio().playbackRate = rate;
    const id = bookRef.current;
    if (id) {
      void apiSend(`/api/items/${id}/speed`, "PUT", { speed: rate }).catch(() => {});
      if (infoRef.current) infoRef.current = { ...infoRef.current, speed: rate };
    }
  }, []);

  const stop = useCallback(() => {
    const a = bookAudio();
    // Saved before the refs are cleared: the pause event below arrives after
    // them and would find no book to save.
    save({ settle: true });
    endSleep();
    if (!a.paused) a.pause();
    a.removeAttribute("src");
    a.load();
    bookRef.current = 0;
    infoRef.current = null;
    partRef.current = -1;
    setBookID(0);
    setInfo(null);
    setPlaying(false);
    setNote("");
  }, [save, endSleep]);

  const chapterIndex = info ? chapterAt(info.chapters, timeMs) : -1;
  const chapter = info && chapterIndex >= 0 ? info.chapters[chapterIndex] : null;

  const value = useMemo<BookPlayerState>(
    () => ({
      bookID,
      book,
      info,
      playing,
      timeMs,
      speed,
      chapter,
      note,
      play,
      toggle,
      seekTo,
      skip,
      skipBack,
      skipForward,
      nextChapter,
      prevChapter,
      setSpeed,
      stop,
      sleep,
      sleepLeftMs: sleepLeft,
      setSleep,
    }),
    [bookID, book, info, playing, timeMs, speed, chapter, note, play, toggle, seekTo, skip,
      skipBack, skipForward, nextChapter, prevChapter, setSpeed, stop, sleep, sleepLeft, setSleep],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
