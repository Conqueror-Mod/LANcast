import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
import { useFocusable } from "@/focus/FocusController";
import { containerCountLabel, isSquareArt } from "@/lib/kind";
import { episodeLabel, rating } from "@/lib/format";
import type { Item } from "@/api/types";
import "./PosterTile.css";

function progressPct(item: Item): number {
  const pos = item.progress?.position_ms ?? 0;
  const dur = item.duration_ms ?? 0;
  if (!pos || !dur) return 0;
  return Math.min(100, (pos / dur) * 100);
}

// onOpen overrides what pressing the tile does. A photo in a gallery selects
// itself into the banner above rather than navigating: a photograph has no
// detail page worth visiting — no synopsis, no cast, no year — and sending
// someone to one would be a worse answer than showing them the picture.
//
/*
 * `actions` gives this tile a right-click menu, and is opt-in per surface.
 *
 * A tile appears in the library grid, in every shelf, and in search results,
 * and what you would want to do to one is not the same in each — "Remove from
 * Continue Watching" is meaningless on a grid poster that was never started.
 * So the tile knows how to *show* a menu and nothing about what is in it, and
 * a surface that passes nothing gets no menu at all rather than a wrong one.
 *
 * A function of the item rather than a list, because the sensible actions
 * depend on the item: an unwatched film and a half-watched one differ.
 *
 * **This is reachable by pointer only.** There is no keyboard route to opening
 * it, which means it does not exist in bigscreen mode — the ten-foot layout is
 * driven by arrow keys for a remote that has no right button, and ADR 0004
 * built that model precisely so the television client would be a restyle
 * rather than a rewrite. Adding a binding is additive from here: the focus
 * controller already carries an onSelect per element and a menu would sit
 * beside it. It is not done yet, and until it is, anything that becomes *only*
 * available in here is unavailable on a television.
 */
export function PosterTile({
  item,
  onOpen,
  actions,
}: {
  item: Item;
  onOpen?: () => void;
  actions?: (item: Item) => MenuAction[];
}) {
  const navigate = useNavigate();
  const open = onOpen ?? (() => navigate(`/item/${item.id}`));
  const [menuAt, setMenuAt] = useState<MenuPoint | null>(null);
  // Set when the menu was summoned by the actions key, so it takes focus.
  const [byKey, setByKey] = useState(false);

  /*
   * The same menu, without a pointer.
   *
   * Anchored under the tile's own bottom-left rather than at a pointer that
   * does not exist — PointMenu keeps it on screen from there. Registered
   * through the focus controller like everything else, so the key that opens it
   * is the one in the keyboard settings rather than one this component invented
   * (ADR 0004).
   */
  const openedFrom = useRef<HTMLElement | null>(null);
  const openMenu = useCallback(
    (el: HTMLElement) => {
      if (!actions || actions(item).length === 0) return;
      openedFrom.current = el;
      setByKey(true);
      const at = el.getBoundingClientRect();
      setMenuAt({ x: at.left, y: at.bottom });
    },
    [actions, item],
  );
  const focusable = useFocusable(open, actions ? openMenu : undefined);

  const poster = artworkURL(item.artwork?.poster, "poster");
  const pct = progressPct(item);
  // A container shows how much it holds ("3 seasons"); a leaf shows its year.
  const count = containerCountLabel(item);
  /*
   * An episode says which episode it is, in place of the year.
   *
   * Everywhere a tile appears outside its own show — Continue Watching, search,
   * a shelf — the episode title alone is not an identification. "Stray Dog
   * Strut · 1998" reads as an obscure film; it is Cowboy Bebop S01E02.
   */
  const episode = episodeLabel(item);
  const score = rating(item.rating);

  return (
    /*
     * The menu is a sibling of the tile, not a child of it.
     *
     * The tile is a <button>, and a button may not contain buttons — the markup
     * is invalid and, worse, every click on a menu item would bubble straight
     * into the tile's own onClick and open the thing you were trying to act on.
     * PointMenu is fixed to the viewport, so sitting outside costs it nothing.
     */
    <>
    <button
      {...focusable}
      className="poster-tile"
      onClick={open}
      // Only where a surface supplied actions. Everywhere else the browser's
      // own menu is left alone, because suppressing it to show nothing is a
      // worse answer than not intervening.
      /*
       * Only where this item has actions — which is not the same as the surface
       * having supplied a function.
       *
       * A library grid holds shows, albums and photographs beside films, and
       * "Play" or "Mark as watched" means nothing on a folder or a picture. So
       * the surface answers per item and an item with nothing to offer opens no
       * menu at all, leaving the browser's own alone. Suppressing that to show
       * an empty box would be a worse answer than not intervening.
       */
      onContextMenu={
        actions
          ? (e) => {
              if (actions(item).length === 0) return;
              e.preventDefault();
              setByKey(false);
              setMenuAt({ x: e.clientX, y: e.clientY });
            }
          : undefined
      }
      title={item.title}
      aria-label={item.title}
    >
      <div
        className={
          "poster-tile__art" +
          (isSquareArt(item) ? " poster-tile__art--square" : "")
        }
      >
        {poster ? (
          <img src={poster} alt="" loading="lazy" draggable={false} />
        ) : (
          <div className="poster-tile__placeholder">
            <span>{item.title}</span>
          </div>
        )}
        {item.content_rating && (
          <span className="poster-tile__cert">{item.content_rating}</span>
        )}
        {score && (
          <span className="poster-tile__rating">
            <span className="poster-tile__star" aria-hidden="true">
              ★
            </span>
            {score}
          </span>
        )}
        {pct > 0 && (
          <div className="poster-tile__progress" style={{ width: `${pct}%` }} />
        )}
      </div>
      {/*
        A photo's caption is its filename, and a photo library's filenames are
        UUIDs and camera serials — the reason the scanner stores them verbatim
        rather than tidying them. Printing 2,600 of those under a grid is noise
        that makes the pictures harder to look at, so the tile carries the title
        for assistive technology (aria-label, above) and shows nothing.
      */}
      {item.kind !== "photo" && (
        <div className="poster-tile__meta">
          <span className="poster-tile__title">{item.title}</span>
          {count ? (
            <span className="poster-tile__year">{count}</span>
          ) : episode ? (
            <span className="poster-tile__year">{episode}</span>
          ) : (
            item.year && <span className="poster-tile__year">{item.year}</span>
          )}
        </div>
      )}
    </button>
    {menuAt && actions && (
      <PointMenu
        at={menuAt}
        actions={actions(item)}
        autoFocus={byKey}
        onClose={() => {
          setMenuAt(null);
          // Focus goes back to the tile it belonged to, or a keyboard is left
          // with nothing focused and the grid has to be walked from the top.
          if (byKey) openedFrom.current?.focus();
        }}
      />
    )}
    </>
  );
}
