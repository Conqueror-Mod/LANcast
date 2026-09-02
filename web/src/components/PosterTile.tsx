import { useCallback, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
import { useFocusable } from "@/focus/FocusController";
import { acknowledge, useCanReveal, useObscured } from "@/lib/sensitiveAck";
import { containerCountLabel, isSquareArt } from "@/lib/kind";
import { episodeLabel, rating } from "@/lib/format";
import type { Item } from "@/api/types";
import "./PosterTile.css";

/*
 * The row that carries the progress is not always the row on the tile.
 *
 * On Continue Watching a show tile stands for its next episode: the show has
 * no position of its own and no duration either, so reading the show would
 * draw an empty bar under a half-watched series. Read whichever row actually
 * holds a position — the episode when there is one, the item itself
 * otherwise.
 */
function progressSource(item: Item): Item {
  return item.next_episode ?? item;
}

function progressPct(item: Item): number {
  const src = progressSource(item);
  const pos = src.progress?.position_ms ?? 0;
  const dur = src.duration_ms ?? 0;
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
  /*
   * A sensitive tile answers the press with a question instead of the thing
   * (ADR 0051).
   *
   * The first press acknowledges and reveals; the second does what the tile
   * would have done. That ordering is the feature — a cover you can click
   * straight through is a cover that has not stopped anything — and it is the
   * only interaction here, because the alternative is a modal in front of a
   * grid where a dozen tiles might be covered.
   */
  const obscured = useObscured(item);
  /*
   * And whether this surface is allowed to lift a cover at all (ADR 0051,
   * amended). The library grid and a gallery's page are; the home shelves, the
   * hero, search and collections are not, so a covered tile there does nothing
   * when pressed rather than uncovering itself.
   */
  const canReveal = useCanReveal();
  const go = onOpen ?? (() => navigate(`/item/${item.id}`));
  const open = () => {
    if (obscured) {
      /*
       * Pressing does nothing where a cover may not be lifted, and that is the
       * behaviour rather than an oversight: navigating instead would open the
       * thing the cover exists to not show, from the screen it most needs
       * covering on.
       */
      if (canReveal) acknowledge(item.id);
      return;
    }
    go();
  };
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

  /*
   * The URL is not built at all while the tile is covered.
   *
   * A CSS blur over the real image is a picture of a privacy feature: the bytes
   * arrive, the element is in the DOM, and anything that drops styles — a
   * stylesheet that has not loaded, a reader view, a devtools panel — shows the
   * photograph the mark exists to not show, on somebody else's screen, which is
   * the entire scenario. Not asking for it cannot fail that way.
   *
   * The server is not asked to withhold it instead, and deliberately: artwork
   * is addressed by content hash and served `immutable`, so a placeholder
   * returned under the real hash would be cached under it for a year.
   */
  const poster = obscured ? "" : artworkURL(item.artwork?.poster, "poster");
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
  /*
   * A show on Continue Watching labels itself with the episode it would play,
   * not with nothing. The show carries no season or episode number, so asking
   * it directly returns empty and the tile loses the one line that says where
   * you are in the series.
   */
  const episode = episodeLabel(item.next_episode ?? item);
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
      /*
       * The tooltip goes away while this tile's menu is open.
       *
       * `title` is a real tooltip drawn by the browser, and a browser draws it
       * above everything the page can produce -- there is no z-index that wins,
       * because it is not in the page. Right-clicking leaves the pointer
       * resting on the tile, so the tooltip appears a second later *on top of
       * the menu* and covers whichever item happens to be under the cursor.
       * Seen in the shipped v0.8.6 build: "Add to queue" was unreadable behind
       * the film's own name.
       *
       * jsdom could never have caught it. The attribute is present, the menu
       * item is present, every assertion passes, and the two are only in
       * conflict once something paints. Same class of miss as the menu that
       * opened half off the screen.
       *
       * The attribute earns its place the rest of the time -- these titles
       * truncate at one line -- so it is suppressed rather than removed.
       * aria-label stays put: it is not drawn, and a screen reader still needs
       * the tile to have a name while a menu hangs off it.
       */
      title={menuAt ? undefined : item.title}
      aria-label={
        obscured
          ? item.title +
            (canReveal
              ? " — sensitive, click to show"
              : " — sensitive, open it in its library to view")
          : item.title
      }
    >
      <div
        className={
          "poster-tile__art" +
          (isSquareArt(item) ? " poster-tile__art--square" : "")
        }
      >
        {obscured ? (
          /*
           * The name stays readable. Hiding it as well would make somebody
           * hunt through identical grey rectangles for the folder they marked,
           * and the mark is about the pictures — a person who marked a folder
           * knows what is in it and needs to be able to find it.
           */
          <div className="poster-tile__sensitive">
            <span className="poster-tile__sensitive-title">{item.title}</span>
            <span className="poster-tile__sensitive-note">
              {canReveal ? "Sensitive — click to show" : "Sensitive"}
            </span>
          </div>
        ) : poster ? (
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
