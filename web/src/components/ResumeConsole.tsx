import { useCallback, useState } from "react";
import { Link, useNavigate } from "react-router";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
import { useFocusable } from "@/focus/FocusController";
import { acknowledge, useCanReveal, useObscured } from "@/lib/sensitiveAck";
import { resumeArt, resumeDetail, resumeFraction, resumeHref } from "@/lib/resume";
import type { Library, ResumeCard } from "@/api/types";
import { LibraryLinks } from "./HomeMasthead";
import "./ResumeConsole.css";

/*
 * The resume console (ADR 0118 §2): one list of everything in progress, in
 * place of Continue Watching, Next up and Continue Listening.
 *
 * Every card is one height and its art keeps its own shape -- poster, sleeve,
 * episode still, box -- inside a fixed column, so the text lines up down the
 * list. That is what lets a film, an album and a comic share a panel without
 * the ragged baseline that split the old shelves.
 */

type CardProps = {
  card: ResumeCard;
  actions?: (card: ResumeCard) => MenuAction[];
  /** Overrides what pressing the card does; otherwise it follows resumeHref. */
  open?: (card: ResumeCard) => (() => void) | undefined;
};

export function ResumeCardRow({ card, actions, open }: CardProps) {
  const navigate = useNavigate();
  const item = card.item;
  const obscured = useObscured(item);
  const canReveal = useCanReveal();
  const go = open?.(card) ?? (() => navigate(resumeHref(card)));
  const press = () => {
    if (obscured) {
      if (canReveal) acknowledge(item.id);
      return;
    }
    go();
  };

  const [menuAt, setMenuAt] = useState<MenuPoint | null>(null);
  const [byKey, setByKey] = useState(false);
  const list = actions?.(card) ?? [];
  const openMenu = useCallback(
    (el: HTMLElement) => {
      if (list.length === 0) return;
      setByKey(true);
      const at = el.getBoundingClientRect();
      setMenuAt({ x: at.left, y: at.bottom });
    },
    [list.length],
  );
  const focusable = useFocusable(press, list.length > 0 ? openMenu : undefined);

  const art = resumeArt(card);
  const detail = resumeDetail(card);
  const frac = resumeFraction(card);

  return (
    <>
      <button
        type="button"
        {...focusable}
        className="resume-card"
        onClick={press}
        onContextMenu={
          list.length > 0
            ? (e) => {
                e.preventDefault();
                setByKey(false);
                setMenuAt({ x: e.clientX, y: e.clientY });
              }
            : undefined
        }
        aria-label={obscured ? `${item.title} — sensitive` : `${item.title}, ${detail}`}
      >
        <span className={`resume-card__art resume-card__art--${art.shape}`}>
          {obscured || !art.src ? (
            <span className="resume-card__placeholder" aria-hidden="true" />
          ) : (
            <img src={art.src} alt="" loading="lazy" draggable={false} />
          )}
        </span>
        <span className="resume-card__text">
          <span className="resume-card__title">{item.title}</span>
          <span className="resume-card__detail">
            {obscured ? (canReveal ? "Sensitive — click to show" : "Sensitive") : detail}
          </span>
          {frac !== null && !obscured && (
            <span className="resume-card__bar" aria-hidden="true">
              <span style={{ width: `${Math.round(frac * 100)}%` }} />
            </span>
          )}
        </span>
      </button>
      {menuAt && (
        <PointMenu at={menuAt} actions={list} autoFocus={byKey} onClose={() => setMenuAt(null)} />
      )}
    </>
  );
}

type ConsoleProps = {
  cards: ResumeCard[];
  /** The server left something out (the limit or two-of-a-kind cap). */
  more: boolean;
  actions?: (card: ResumeCard) => MenuAction[];
  open?: (card: ResumeCard) => (() => void) | undefined;
  /** Listed in place of the cards when nothing is in progress. */
  libraries?: Library[];
};

export function ResumeConsole({ cards, more, actions, open, libraries }: ConsoleProps) {
  /*
   * Nothing in progress (ADR 0118 §3): the panel says what is here instead,
   * so the first screen is never an empty frame. With no libraries at all
   * there is nothing to list, and the page's welcome stands in for this.
   */
  if (cards.length === 0) {
    if (!libraries || libraries.length === 0) return null;
    return (
      <section className="resume panel panel--resume" aria-labelledby="resume-title">
        <header className="resume__head">
          <h2 id="resume-title" className="resume__title">
            Your libraries
          </h2>
        </header>
        <p className="resume__idle">Nothing in progress. Pick somewhere to start.</p>
        <LibraryLinks libraries={libraries} />
      </section>
    );
  }
  /*
   * See all when the server held something back, and also when there are more
   * cards than a phone shows: a narrow screen hides the fourth and fifth with
   * CSS, and a link that only appears when the server says so would leave
   * those unreachable there.
   */
  const seeAll = more || cards.length > 3;
  return (
    <section className="resume panel panel--resume" aria-labelledby="resume-title">
      <header className="resume__head">
        <h2 id="resume-title" className="resume__title">
          Continue
        </h2>
        {seeAll && (
          <Link className="resume__all" to="/continue">
            See all
          </Link>
        )}
      </header>
      <ol className="resume__list">
        {cards.map((c) => (
          <li key={`${c.kind}-${c.item.id}`} className="resume__item">
            <ResumeCardRow card={c} actions={actions} open={open} />
          </li>
        ))}
      </ol>
    </section>
  );
}
