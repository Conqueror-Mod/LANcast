import { useMemo, useState } from "react";
import { useEditShelf, useHomeShelves, useItems, useUnpinShelf } from "@/api/hooks";
import type { HomeShelf, Item, Library } from "@/api/types";
import { libraryItemQuery } from "@/lib/libraryQuery";
import { configForKind } from "@/screens/libraryConfig";
import { ButtonMenu, type MenuAction } from "./Menu";
import { Shelf } from "./Shelf";
import { ShelfTitleDialog } from "./ShelfTitleDialog";

/*
 * The shelves you pinned from a library grid (ADR 0119 §2), in your order.
 *
 * Each is fetched through the same query the grid builds (libraryItemQuery),
 * so a pinned shelf is that grid's first row and nothing else: its sort, its
 * filters, its level. "All" opens the grid with the query on.
 */
export function PinnedShelves({
  libraries,
  itemActions,
}: {
  libraries?: Library[];
  /** What a right-click on a tile offers. */
  itemActions?: (item: Item) => MenuAction[];
}) {
  const { data } = useHomeShelves();
  const shelves = data?.shelves ?? [];
  const byID = useMemo(() => new Map((libraries ?? []).map((l) => [l.id, l])), [libraries]);
  if (shelves.length === 0) return null;
  return (
    <>
      {shelves.map((shelf, i) => {
        const lib = byID.get(shelf.library_id);
        // The libraries list is the account's own view of them; a library
        // missing from it is one this account cannot see right now.
        if (!lib) return null;
        return (
          <PinnedShelf
            key={shelf.id}
            shelf={shelf}
            library={lib}
            first={i === 0}
            last={i === shelves.length - 1}
            itemActions={itemActions}
          />
        );
      })}
    </>
  );
}

function PinnedShelf({
  shelf,
  library,
  first,
  last,
  itemActions,
}: {
  shelf: HomeShelf;
  library: Library;
  first: boolean;
  last: boolean;
  itemActions?: (item: Item) => MenuAction[];
}) {
  const params = useMemo(() => new URLSearchParams(shelf.query), [shelf.query]);
  const query = libraryItemQuery(library.id, params, configForKind(library.kind));
  const { data, isSuccess } = useItems({ ...query, limit: 20 });
  const edit = useEditShelf();
  const unpin = useUnpinShelf();
  const [renaming, setRenaming] = useState(false);

  const actions: MenuAction[] = [
    { label: "Rename", onSelect: () => setRenaming(true) },
    {
      label: "Move up",
      disabled: first,
      onSelect: () => edit.mutate({ id: shelf.id, position: shelf.position - 1 }),
    },
    {
      label: "Move down",
      disabled: last,
      onSelect: () => edit.mutate({ id: shelf.id, position: shelf.position + 1 }),
    },
    { label: "Unpin", danger: true, onSelect: () => unpin.mutate(shelf.id) },
  ];
  const menu = (
    <ButtonMenu label={`Actions for the ${shelf.title} shelf`} className="shelf__menu" actions={actions} />
  );
  const seeAll = `/library/${library.id}${shelf.query ? `?${shelf.query}` : ""}`;
  const items = data?.items ?? [];

  return (
    <>
      {items.length > 0 ? (
        <Shelf
          title={shelf.title}
          items={items}
          seeAllTo={seeAll}
          headerActions={menu}
          itemActions={itemActions}
        />
      ) : (
        isSuccess && (
          /*
           * Nothing matches today — every film in it watched, say. Still drawn
           * as a heading, because a shelf that vanished would also take its
           * menu, and with it the only way to unpin it.
           */
          <section className="shelf shelf--empty">
            <div className="shelf__head">
              <span className="section-label">{shelf.title}</span>
              <span className="shelf__rule" />
              {menu}
            </div>
            <p className="shelf__none">Nothing here right now.</p>
          </section>
        )
      )}
      {renaming && (
        <ShelfTitleDialog
          heading="Rename shelf"
          action="Rename"
          initial={shelf.title}
          busy={edit.isPending}
          error={edit.error ? (edit.error as Error).message : undefined}
          onClose={() => {
            setRenaming(false);
            edit.reset();
          }}
          onSave={(title) =>
            edit.mutate({ id: shelf.id, title }, { onSuccess: () => setRenaming(false) })
          }
        />
      )}
    </>
  );
}
