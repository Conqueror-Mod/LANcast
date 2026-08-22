import { Link } from "react-router-dom";
import { PosterTile } from "./PosterTile";
import type { MenuAction } from "./Menu";
import type { Item } from "@/api/types";
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
}

// A horizontally scrolling hub row. The header pairs a wide-tracked label with a
// gold-to-transparent hairline trailing right, per design.md. Tiles reuse the
// same PosterTile as the grid, so focus, the gold rule, and progress bars are
// identical everywhere.
export function Shelf({ title, items, seeAllTo, itemActions }: Props) {
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
      <div className="shelf__track">
        {items.map((item) => (
          <div className="shelf__item" key={item.id}>
            <PosterTile item={item} actions={itemActions} />
          </div>
        ))}
      </div>
    </section>
  );
}
