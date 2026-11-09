import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  useComicPages,
  useIsAdmin,
  useItem,
  useSetComicPlace,
  useSetSeriesDirection,
} from "@/api/hooks";
import { comicFitOf, comicLayoutOf, useReadingPrefs } from "@/lib/readingPrefs";
import { nextSpread, prevSpread, spreadPages } from "./readerLayout";
import "./Reader.css";

/*
 * Two facing pages need a screen wider than it is tall. A phone held upright
 * shows one page whatever the layout says, and turns to two when it is turned
 * on its side.
 */
const LANDSCAPE = "(orientation: landscape)";
function useLandscape(): boolean {
  return useSyncExternalStore(
    (fn) => {
      if (typeof window.matchMedia !== "function") return () => {};
      const m = window.matchMedia(LANDSCAPE);
      m.addEventListener?.("change", fn);
      return () => m.removeEventListener?.("change", fn);
    },
    () => (typeof window.matchMedia === "function" ? window.matchMedia(LANDSCAPE).matches : true),
    () => true,
  );
}

/*
 * The comic reader (ADR 0101).
 *
 * A page at a time or two facing pages, fitted to the screen or to its
 * width (Your account › Reading & listening). The arrow keys, Page Up and Down,
 * Space, a click or tap on either side, or a swipe turn it; Home and End jump
 * to the ends; Escape goes back. The place is kept as you go, a moment after
 * each turn rather than on every keypress, and once more on the way out.
 *
 * It opens where you left off. A comic you finished opens at the start again,
 * because the last page of something you have read is not where anybody wants
 * to be put.
 *
 * Right to left (ADR 0123), when the server says so: the next page is to the
 * left, so the left arrow, a tap on the left, a swipe to the right and the
 * left-hand button go forward, and a spread puts its earlier page on the
 * right. Space and Page Down have no side and still go forward.
 */
export function Reader() {
  const id = Number(useParams().id);
  const navigate = useNavigate();
  // Start over, from the detail page: the start, whatever the place says.
  const [params] = useSearchParams();
  const fromStart = params.get("start") === "1";
  const { data: item } = useItem(id);
  const { data, isError } = useComicPages(id);
  const setPlace = useSetComicPlace(id);
  const [page, setPage] = useState<number | null>(null);
  const [chrome, setChrome] = useState(true);
  // The page whose image failed to load, shown as a sentence instead.
  const [failed, setFailed] = useState<number | null>(null);
  const saved = useRef<number | null>(null);
  const pending = useRef<number | null>(null);

  const pages = data?.pages ?? 0;
  const rtl = data?.direction === "rtl";
  // Which way "left" goes: back when reading left to right, on when not.
  const leftward = rtl ? 1 : -1;

  const { data: prefs } = useReadingPrefs();
  const landscape = useLandscape();
  const spread = comicLayoutOf(prefs) === "spread" && landscape;
  const fitWidth = comicFitOf(prefs) === "width";
  // What is on screen, left to right. The place kept is the first of them.
  const shownPages = page === null ? [] : spread ? spreadPages(page, pages) : [page];
  const lastShown = shownPages.length > 0 ? shownPages[shownPages.length - 1] : (page ?? 0);
  const stage = useRef<HTMLDivElement>(null);

  // Where to open, once the count and the place are known.
  useEffect(() => {
    if (!data || page !== null) return;
    const at = data.place?.page ?? 0;
    const finished = data.place && at >= data.pages - 1;
    const start = finished || fromStart ? 0 : Math.min(Math.max(at, 0), Math.max(data.pages - 1, 0));
    // Opening a comic for the first time is not a place worth keeping; the
    // first turn is.
    saved.current = data.place ? at : 0;
    setPage(start);
  }, [data, page, fromStart]);

  /*
   * Keep the place a moment after the page settles, and on the way out.
   *
   * The mutation is reached through a ref so that flush never changes. It
   * did, once: useMutation's result is a new object every render, flush was
   * rebuilt with it, and the unmount effect below — whose cleanup is flush —
   * ran on every render, keeping every page the moment it was turned.
   */
  const mutate = useRef(setPlace.mutate);
  mutate.current = setPlace.mutate;
  const flush = useCallback(() => {
    const p = pending.current;
    if (p === null || p === saved.current) return;
    saved.current = p;
    pending.current = null;
    mutate.current(p);
  }, []);

  useEffect(() => {
    if (page === null) return;
    pending.current = page;
    const t = window.setTimeout(flush, 600);
    return () => window.clearTimeout(t);
  }, [page, flush]);

  useEffect(() => () => flush(), [flush]);

  const go = useCallback(
    (to: number) => {
      if (!pages) return;
      setPage((cur) => {
        const next = Math.min(Math.max(to, 0), pages - 1);
        return cur === next ? cur : next;
      });
    },
    [pages],
  );
  const step = useCallback(
    (d: number) =>
      setPage((cur) => {
        if (cur === null) return cur;
        // A spread turns as a whole.
        if (spread) return d > 0 ? nextSpread(cur, pages) : prevSpread(cur);
        return Math.min(Math.max(cur + d, 0), Math.max(pages - 1, 0));
      }),
    [pages, spread],
  );

  // A new page starts at its top when it is taller than the screen.
  useEffect(() => {
    if (fitWidth) stage.current?.scrollTo?.({ top: 0 });
  }, [page, fitWidth]);

  /*
   * Fitted to the width, a page is taller than the screen, so Space and Page
   * Down read down it first and turn only at its foot, as a long web page
   * does. Page Up does the same upwards.
   */
  const scrollOrStep = useCallback(
    (d: number) => {
      const el = stage.current;
      if (!fitWidth || !el) return step(d);
      const atEnd = d > 0 ? el.scrollTop + el.clientHeight >= el.scrollHeight - 2 : el.scrollTop <= 2;
      if (atEnd) step(d);
      else el.scrollBy?.({ top: d * el.clientHeight * 0.9 });
    },
    [fitWidth, step],
  );

  const leave = useCallback(() => {
    flush();
    if (window.history.length > 1) navigate(-1);
    else navigate(`/item/${id}`);
  }, [flush, navigate, id]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      // The series' direction is a dropdown in the bar; its arrows are its own.
      if (e.target instanceof HTMLSelectElement) return;
      switch (e.key) {
        case "ArrowDown":
        case "ArrowUp":
          // Fitted to the width, up and down scroll the page.
          if (fitWidth) return;
          step(e.key === "ArrowDown" ? 1 : -1);
          break;
        case "ArrowRight":
          step(-leftward);
          break;
        case "ArrowLeft":
          step(leftward);
          break;
        case "PageDown":
        case " ":
          scrollOrStep(1);
          break;
        case "PageUp":
          scrollOrStep(-1);
          break;
        case "Home":
          go(0);
          break;
        case "End":
          go(pages - 1);
          break;
        case "Escape":
        case "Backspace":
          leave();
          break;
        default:
          return;
      }
      e.preventDefault();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [step, scrollOrStep, go, leave, pages, fitWidth, leftward]);

  // The next page is fetched before it is asked for, so turning is instant.
  useEffect(() => {
    if (page === null) return;
    for (let n = lastShown + 1; n <= lastShown + (spread ? 2 : 1) && n < pages; n++) {
      const img = new Image();
      img.src = pageURL(id, n);
    }
  }, [id, page, pages, lastShown, spread]);

  // A swipe turns the page; a tap on the left third goes left, on the right
  // third right, and a tap in the middle shows or hides the bar. Which of
  // left and right is forward depends on the direction.
  const down = useRef<{ x: number; y: number } | null>(null);
  function onPointerDown(e: React.PointerEvent) {
    down.current = { x: e.clientX, y: e.clientY };
  }
  function onPointerUp(e: React.PointerEvent) {
    const start = down.current;
    down.current = null;
    if (!start) return;
    const dx = e.clientX - start.x;
    const dy = e.clientY - start.y;
    if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy)) {
      // Swiping left drags the page on the right into view.
      step(dx < 0 ? -leftward : leftward);
      return;
    }
    const w = (e.currentTarget as HTMLElement).clientWidth || window.innerWidth;
    const x = e.clientX / w;
    if (x < 1 / 3) step(leftward);
    else if (x > 2 / 3) step(-leftward);
    else setChrome((c) => !c);
  }

  const title = item?.title ?? "";
  const atStart = page === null || page <= 0;
  const atEnd = page === null || lastShown >= pages - 1;
  const isAdmin = useIsAdmin();

  if (isError) {
    return (
      <div className="reader reader--message" role="alert">
        <p>This comic cannot be opened. The file may be missing, or it is not a comic archive.</p>
        <button type="button" className="reader__btn" onClick={leave}>
          Back
        </button>
      </div>
    );
  }

  return (
    <div
      className={
        "reader" +
        (chrome ? "" : " reader--bare") +
        (spread ? " reader--spread" : "") +
        (fitWidth ? " reader--fit-width" : "") +
        (rtl ? " reader--rtl" : "")
      }
    >
      <div
        ref={stage}
        className="reader__stage"
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        aria-label={title ? `${title}, ${pagesLabel(shownPages, pages)}` : undefined}
      >
        {pages > 0 &&
          shownPages.map((n) =>
            failed === n ? (
              <p key={n} className="reader__missing" role="status">
                Page {n + 1} cannot be shown. A PDF page that is text or drawing, rather than a
                picture, is not read — the pages either side of it are.
              </p>
            ) : (
              <img
                key={n}
                className="reader__page"
                src={pageURL(id, n)}
                alt={`Page ${n + 1}`}
                draggable={false}
                onError={() => setFailed(n)}
              />
            ),
          )}
      </div>
      <div className="reader__bar">
        <button type="button" className="reader__btn" onClick={leave} aria-label="Back">
          ←
        </button>
        <span className="reader__title">{title}</span>
        <span className="reader__count" aria-live="polite">
          {page !== null && pages > 0 ? `${countLabel(shownPages)} / ${pages}` : ""}
        </span>
        {isAdmin && data?.series && <SeriesDirection id={id} pages={data} />}
        <button
          type="button"
          className="reader__btn"
          disabled={rtl ? atEnd : atStart}
          onClick={() => step(leftward)}
          aria-label={rtl ? "Next page" : "Previous page"}
        >
          ‹
        </button>
        <button
          type="button"
          className="reader__btn"
          disabled={rtl ? atStart : atEnd}
          onClick={() => step(-leftward)}
          aria-label={rtl ? "Previous page" : "Next page"}
        >
          ›
        </button>
      </div>
      <div className="reader__progress" aria-hidden="true">
        <div
          className="reader__progress-fill"
          style={{ width: pages > 0 && page !== null ? `${((lastShown + 1) / pages) * 100}%` : "0%" }}
        />
      </div>
    </div>
  );
}

/*
 * Which way this comic's series reads, for an admin (ADR 0123). In the bar
 * because the reader is where a wrong direction is noticed. "The library's
 * way" removes the override; the option says which way that is.
 */
function SeriesDirection({
  id,
  pages,
}: {
  id: number;
  pages: { direction: string; direction_from: string; series?: string | null };
}) {
  const set = useSetSeriesDirection(id);
  const value = pages.direction_from === "series" ? pages.direction : "";
  // What the library would give, which is what "" means here. Only known when
  // the library is what decided it; otherwise it is left to right unless the
  // library says otherwise, and the series' own value hides which.
  const libraryWay =
    pages.direction_from === "series" ? "" : pages.direction === "rtl" ? " (right to left)" : " (left to right)";
  return (
    <select
      className="reader__direction"
      aria-label={`Reading direction for ${pages.series}`}
      value={value}
      disabled={set.isPending}
      onChange={(e) => set.mutate(e.target.value as "" | "ltr" | "rtl")}
      // The bar's own clicks are not page turns.
      onPointerUp={(e) => e.stopPropagation()}
    >
      <option value="">{`Library's way${libraryWay}`}</option>
      <option value="ltr">Left to right</option>
      <option value="rtl">Right to left</option>
    </select>
  );
}

// "3–4" for a spread, "3" for one page; counted from 1.
function countLabel(shown: number[]): string {
  if (shown.length === 0) return "";
  return shown.length > 1 ? `${shown[0] + 1}–${shown[shown.length - 1] + 1}` : `${shown[0] + 1}`;
}

function pagesLabel(shown: number[], total: number): string {
  return shown.length > 1
    ? `pages ${shown[0] + 1} and ${shown[1] + 1} of ${total}`
    : `page ${(shown[0] ?? 0) + 1} of ${total}`;
}

export function pageURL(id: number, n: number) {
  return `/api/items/${id}/pages/${n}`;
}
