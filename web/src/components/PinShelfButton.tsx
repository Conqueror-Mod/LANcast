import { useState } from "react";
import { usePinShelf } from "@/api/hooks";
import { activePills } from "@/lib/browseFilters";
import { shelfQuery } from "@/lib/libraryQuery";
import { ShelfTitleDialog } from "./ShelfTitleDialog";

/*
 * Pins the view on screen to Home (ADR 0119 §2): the library and the grid's
 * own query, so the shelf is the same listing the grid shows.
 *
 * The title starts as the filter pills joined, which is what the view already
 * calls itself; the library's name when nothing is filtered but the sort.
 */
export function defaultShelfTitle(
  libraryName: string,
  params: URLSearchParams,
  castNames?: Map<string, string>,
  faceNames?: Map<string, string>,
): string {
  const labels = activePills(params, { castNames, faceNames }).map((p) => p.label);
  const q = params.get("q");
  if (q) labels.unshift(`“${q}”`);
  const title = labels.join(" · ");
  if (!title) return libraryName;
  return title.length > 80 ? title.slice(0, 79) + "…" : title;
}

export function PinShelfButton({
  libraryID,
  libraryName,
  params,
  castNames,
  faceNames,
}: {
  libraryID: number;
  libraryName: string;
  params: URLSearchParams;
  castNames?: Map<string, string>;
  faceNames?: Map<string, string>;
}) {
  const pin = usePinShelf();
  const [open, setOpen] = useState(false);
  const [done, setDone] = useState(false);

  const close = () => {
    setOpen(false);
    pin.reset();
  };

  return (
    <>
      <button
        type="button"
        className="browse__clear"
        onClick={() => {
          setDone(false);
          setOpen(true);
        }}
      >
        {done ? "Pinned to Home" : "Pin to Home"}
      </button>
      {open && (
        <ShelfTitleDialog
          heading="Pin to Home"
          action="Pin to Home"
          initial={defaultShelfTitle(libraryName, params, castNames, faceNames)}
          busy={pin.isPending}
          error={pin.error ? (pin.error as Error).message : undefined}
          onClose={close}
          onSave={(title) =>
            pin.mutate(
              { libraryID, title, query: shelfQuery(params) },
              {
                onSuccess: () => {
                  setDone(true);
                  close();
                },
              },
            )
          }
        />
      )}
    </>
  );
}
