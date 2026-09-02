import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { PosterTile } from "./PosterTile";
import type { MenuAction } from "./Menu";
import type { Item } from "@/api/types";
import { edges, pageBy, type Edges } from "./shelfEdges";
import "./Shelf.css";

interface Props {
  title: string;
  items: Item[];
  seeAllTo?: string;
  /*
   * A right-click menu for this shelf's tiles, or nothing.
   *
   * Passed straight through rather than decided here: the two Continue shelves
   * want to offer removal from themselves, and Recently Added does not, and a
   * shelf is not the thing that knows which of those it is.
   */
  itemActions?: (item: Item) => MenuAction[];
  /*
   * What pressing a tile does, when the default is wrong for this shelf.
   *
   * Continue Watching needs it: its rows are shows, and opening a show's
   * detail page is not continuing it. Returning undefined for an item leaves
   * that tile with the ordinary behaviour, so one shelf can mix the two.
   */
  itemOpen?: (item: Item) => (() => void) | undefined;
}

// A horizontally scrolling hub row. The header pairs a wide-tracked label with a
// gold-to-transparent hairline trailing right, per design.md. Tiles reuse the
// same PosterTile as the grid, so focus, the gold rule, and progress bars are
// identical everywhere.
export function Shelf({
  title,
  items,
  seeAllTo,
  itemActions,
  itemOpen,
}: Props) {
  const track = useRef<HTMLDivElement>(null);
  const [reach, setReach] = useState<Edges>({ left: false, right: false });

  const measure = useCallback(() => {
    const el = track.current;
    if (!el) return;
    setReach(edges(el.scrollLeft, el.clientWidth, el.scrollWidth));
  }, []);

  /*
   * Measured on mount, on scroll, on resize, and whenever the items change.
   *
   * The last one is the easy omission: a shelf renders empty while its query
   * is in flight and fills in afterwards, so a measurement taken only on mount
   * says "nothing to scroll" for ever on every shelf in the app.
   *
   * ResizeObserver rather than a window listener, because the rail collapsing
   * and expanding changes the track's width without the window changing at
   * all — and that is the most common resize this element sees.
   */
  useEffect(() => {
    const el = track.current;
    if (!el) return;
    measure();
    /*
     * Feature-detected, because jsdom has no ResizeObserver and the test
     * environment is where this first ran.
     *
     * Guarding rather than stubbing it globally: the observer is an
     * enhancement — the measurements on mount, on scroll and on a change of
     * items all stand without it — so a missing one should cost the shelf a
     * remeasure on resize, not throw during render. A global stub would also
     * have hidden that this code has a hard dependency at all.
     */
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure, items]);

  const page = (dir: 1 | -1) => {
    const el = track.current;
    if (!el) return;
    el.scrollBy({ left: pageBy(el.clientWidth, dir), behavior: "smooth" });
  };

  if (items.length === 0) return null;
  return (
    <section className="shelf">
      <div className="shelf__head">
        <span className="section-label">{title}</span>
        <span className="shelf__rule" />
        {seeAllTo && (
          <Link className="shelf__all" to={seeAllTo}>
            All
          </Link>
        )}
      </div>
      <div className="shelf__viewport">
        {/*
          Mouse affordances, and deliberately invisible to the keyboard.

          `tabIndex={-1}` and `aria-hidden`: arrow keys already walk the tiles
          through the focus controller (ADR 0004), and the browser scrolls a
          focused tile into view, so a keyboard user needs nothing here. Two
          focusable buttons sitting between rows would interrupt that walk to
          offer something it already does — and in bigscreen, where there is no
          pointer at all, they would be two stops on the way to nowhere.
        */}
        <button
          className="shelf__page shelf__page--left"
          onClick={() => page(-1)}
          tabIndex={-1}
          aria-hidden="true"
          data-visible={reach.left || undefined}
        >
          <ChevronLeft />
        </button>
        <div className="shelf__track" ref={track} onScroll={measure}>
          {items.map((item) => (
            <div className="shelf__item" key={item.id}>
              <PosterTile
                item={item}
                actions={itemActions}
                onOpen={itemOpen?.(item)}
              />
            </div>
          ))}
        </div>
        <button
          className="shelf__page shelf__page--right"
          onClick={() => page(1)}
          tabIndex={-1}
          aria-hidden="true"
          data-visible={reach.right || undefined}
        >
          <ChevronRight />
        </button>
      </div>
    </section>
  );
}

/*
 * The glyphs, drawn rather than pulled from an icon font.
 *
 * The same reasoning the nav rail's icons were built on: twenty lines against a
 * dependency whose whole purpose is two arrows.
 */
function ChevronLeft() {
  return (
    <svg viewBox="0 0 24 24" width="28" height="28" aria-hidden="true">
      <path
        d="M15 5 8 12l7 7"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ChevronRight() {
  return (
    <svg viewBox="0 0 24 24" width="28" height="28" aria-hidden="true">
      <path
        d="m9 5 7 7-7 7"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
