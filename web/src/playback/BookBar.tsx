/*
 * The book player's controls (ADR 0116 step 2): a bar along the bottom of the
 * window while a book is loaded, so listening carries on while somebody
 * browses. What a listener expects and nothing a film needs: back 15 seconds,
 * forward 30, chapter skips, speed, and the place on the whole book's clock.
 */
import { useEffect } from "react";
import { Link } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { useBookPlayer } from "./BookPlayer";
import { BOOK_SPEEDS, clock } from "./bookTimeline";
import "./BookBar.css";

function speedLabel(r: number): string {
  return `${r}×`;
}

export function BookBar() {
  const b = useBookPlayer();
  const open = !!b && b.bookID > 0;

  // Room at the foot of the page for the bar, so the last row of a grid is
  // never underneath it.
  useEffect(() => {
    document.documentElement.classList.toggle("book-bar-open", open);
    return () => document.documentElement.classList.remove("book-bar-open");
  }, [open]);

  if (!b) return null;
  if (!open) {
    return b.note ? (
      <div className="book-bar book-bar--note" role="status">
        {b.note}
      </div>
    ) : null;
  }

  const total = b.info?.duration_ms ?? 0;
  const cover = artworkURL(b.book?.artwork?.poster, "poster");
  const title = b.book?.title ?? "";
  const chapterTitle = b.chapter && b.chapter.title !== title ? b.chapter.title : "";

  return (
    <div className="book-bar" role="region" aria-label="Book player">
      <Link className="book-bar__book" to={`/item/${b.bookID}`}>
        {cover ? (
          <img className="book-bar__cover" src={cover} alt="" draggable={false} />
        ) : (
          <span className="book-bar__cover book-bar__cover--empty" aria-hidden="true" />
        )}
        <span className="book-bar__titles">
          <span className="book-bar__title">{title}</span>
          {chapterTitle && <span className="book-bar__chapter">{chapterTitle}</span>}
        </span>
      </Link>

      <div className="book-bar__controls">
        <button type="button" className="book-bar__btn" onClick={b.prevChapter} aria-label="Previous chapter">
          <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 4v12M16 4l-8 6 8 6z" /></svg>
        </button>
        <button type="button" className="book-bar__btn book-bar__btn--skip" onClick={b.skipBack} aria-label="Back 15 seconds">
          <span>−15</span>
        </button>
        <button
          type="button"
          className="book-bar__btn book-bar__btn--play"
          onClick={b.toggle}
          aria-label={b.playing ? "Pause" : "Play"}
        >
          {b.playing ? (
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 4h3v12H6zM11 4h3v12h-3z" /></svg>
          ) : (
            <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M6 4l10 6-10 6z" /></svg>
          )}
        </button>
        <button type="button" className="book-bar__btn book-bar__btn--skip" onClick={b.skipForward} aria-label="Forward 30 seconds">
          <span>+30</span>
        </button>
        <button type="button" className="book-bar__btn" onClick={b.nextChapter} aria-label="Next chapter">
          <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M15 4v12M4 4l8 6-8 6z" /></svg>
        </button>
      </div>

      <div className="book-bar__scrub">
        <span className="book-bar__time">{clock(b.timeMs)}</span>
        <input
          type="range"
          className="book-bar__range"
          min={0}
          max={Math.max(1, total)}
          step={1000}
          value={Math.min(b.timeMs, total)}
          onChange={(e) => b.seekTo(Number(e.target.value))}
          aria-label="Place in the book"
          aria-valuetext={`${clock(b.timeMs)} of ${clock(total)}`}
        />
        <span className="book-bar__time">−{clock(Math.max(0, total - b.timeMs))}</span>
      </div>

      <label className="book-bar__speed">
        <span className="visually-hidden">Speed</span>
        <select value={String(b.speed)} onChange={(e) => b.setSpeed(Number(e.target.value))} aria-label="Speed">
          {BOOK_SPEEDS.map((r) => (
            <option key={r} value={String(r)}>
              {speedLabel(r)}
            </option>
          ))}
        </select>
      </label>

      <button type="button" className="book-bar__btn book-bar__close" onClick={b.stop} aria-label="Close the book">
        <svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5l10 10M15 5L5 15" /></svg>
      </button>
    </div>
  );
}
