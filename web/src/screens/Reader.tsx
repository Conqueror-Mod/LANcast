import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useComicPages, useItem, useSetComicPlace } from "@/api/hooks";
import "./Reader.css";

/*
 * The comic reader (ADR 0101).
 *
 * A page at a time, fitted to the screen. The arrow keys, Page Up and Down,
 * Space, a click or tap on either side, or a swipe turn it; Home and End jump
 * to the ends; Escape goes back. The place is kept as you go, a moment after
 * each turn rather than on every keypress, and once more on the way out.
 *
 * It opens where you left off. A comic you finished opens at the start again,
 * because the last page of something you have read is not where anybody wants
 * to be put.
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
  const saved = useRef<number | null>(null);
  const pending = useRef<number | null>(null);

  const pages = data?.pages ?? 0;

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
  const step = useCallback((d: number) => setPage((cur) => (cur === null ? cur : Math.min(Math.max(cur + d, 0), Math.max(pages - 1, 0)))), [pages]);

  const leave = useCallback(() => {
    flush();
    if (window.history.length > 1) navigate(-1);
    else navigate(`/item/${id}`);
  }, [flush, navigate, id]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.altKey || e.ctrlKey || e.metaKey) return;
      switch (e.key) {
        case "ArrowRight":
        case "ArrowDown":
        case "PageDown":
        case " ":
          step(1);
          break;
        case "ArrowLeft":
        case "ArrowUp":
        case "PageUp":
          step(-1);
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
  }, [step, go, leave, pages]);

  // The next page is fetched before it is asked for, so turning is instant.
  useEffect(() => {
    if (page === null || page + 1 >= pages) return;
    const img = new Image();
    img.src = pageURL(id, page + 1);
  }, [id, page, pages]);

  // A swipe turns the page; a tap on the left third goes back, anywhere else
  // forward, and a tap in the middle shows or hides the bar.
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
      step(dx < 0 ? 1 : -1);
      return;
    }
    const w = (e.currentTarget as HTMLElement).clientWidth || window.innerWidth;
    const x = e.clientX / w;
    if (x < 1 / 3) step(-1);
    else if (x > 2 / 3) step(1);
    else setChrome((c) => !c);
  }

  const title = item?.title ?? "";

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
    <div className={"reader" + (chrome ? "" : " reader--bare")}>
      <div
        className="reader__stage"
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
        aria-label={title ? `${title}, page ${(page ?? 0) + 1} of ${pages}` : undefined}
      >
        {page !== null && pages > 0 && (
          <img
            key={page}
            className="reader__page"
            src={pageURL(id, page)}
            alt={`Page ${page + 1}`}
            draggable={false}
          />
        )}
      </div>
      <div className="reader__bar">
        <button type="button" className="reader__btn" onClick={leave} aria-label="Back">
          ←
        </button>
        <span className="reader__title">{title}</span>
        <span className="reader__count" aria-live="polite">
          {page !== null && pages > 0 ? `${page + 1} / ${pages}` : ""}
        </span>
        <button
          type="button"
          className="reader__btn"
          disabled={page === null || page <= 0}
          onClick={() => step(-1)}
          aria-label="Previous page"
        >
          ‹
        </button>
        <button
          type="button"
          className="reader__btn"
          disabled={page === null || page >= pages - 1}
          onClick={() => step(1)}
          aria-label="Next page"
        >
          ›
        </button>
      </div>
      <div className="reader__progress" aria-hidden="true">
        <div
          className="reader__progress-fill"
          style={{ width: pages > 0 && page !== null ? `${((page + 1) / pages) * 100}%` : "0%" }}
        />
      </div>
    </div>
  );
}

export function pageURL(id: number, n: number) {
  return `/api/items/${id}/pages/${n}`;
}
