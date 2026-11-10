import { Shelf } from "./Shelf";
import { addedThisWeek, rowLabel } from "@/lib/newThisWeek";
import type { Item } from "@/api/types";
import type { MenuAction } from "./Menu";
import "./NewThisWeek.css";

/*
 * New this week (ADR 0118), in place of Recently Added, New Music and Recently
 * Added Photos.
 *
 * One section under one heading, with a row for each kind of thing that came
 * in -- still a window each, fetched separately, because a shared window is
 * how a music import once emptied the films shelf, and how a comics library
 * did it again. A row with nothing from the last seven days is left out, and
 * so is the whole section when every row is, rather than a heading over
 * nothing.
 */
export interface NewThisWeekRows {
  video?: Item[];
  music?: Item[];
  books?: Item[];
  comics?: Item[];
  games?: Item[];
  photos?: Item[];
}

const ROWS: { key: keyof NewThisWeekRows; title: string }[] = [
  { key: "video", title: "Films and shows" },
  { key: "music", title: "Music" },
  { key: "books", title: "Audiobooks" },
  { key: "comics", title: "Comics" },
  { key: "games", title: "Games" },
  { key: "photos", title: "Photos" },
];

export function NewThisWeek({
  rows,
  now,
  itemActions,
}: {
  rows: NewThisWeekRows;
  /** What a right-click on a tile offers. */
  itemActions?: (item: Item) => MenuAction[];
  /** Unix seconds; a test passes a fixed one. */
  now?: number;
}) {
  const shown = ROWS.map((r) => ({ ...r, items: addedThisWeek(rows[r.key], now) })).filter(
    (r) => r.items.length > 0,
  );
  if (shown.length === 0) return null;

  return (
    <section className="new-week" aria-labelledby="new-week-title">
      <h2 id="new-week-title" className="new-week__title">
        New this week
      </h2>
      {shown.map((r) => (
        <Shelf
          key={r.key}
          // The video row is named by what is in it ("Films", "Shows", or
          // both), since a week may bring only one; the others by their kind.
          title={r.key === "video" ? rowLabel(r.items, r.title) : r.title}
          items={r.items}
          itemActions={itemActions}
        />
      ))}
    </section>
  );
}
