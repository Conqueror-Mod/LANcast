import { useRefreshLibrary, useRefreshPreview } from "@/api/hooks";
import type { Library } from "@/api/types";

/*
 * One library's share of a language refresh (ADR 0122): how many titles it
 * would fetch again, and the button that does it.
 *
 * A button per library rather than one for everything, so the cost is read
 * where it is spent — a film library of 1,480 titles is 1,480 lookups — and so
 * a household can refresh the shows the children watch and leave the rest.
 */
export function LanguageRefreshRow({ library }: { library: Library }) {
  const preview = useRefreshPreview(library.id, "language", true);
  const refresh = useRefreshLibrary();
  const n = preview.data?.count;
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{library.name}</div>
        <div className="set-row__sub">
          {refresh.isSuccess
            ? `Fetching ${refresh.data.queued.toLocaleString()} title${refresh.data.queued === 1 ? "" : "s"} again in the background.`
            : refresh.isError
              ? "The refresh could not be started."
              : n === undefined
                ? "Counting…"
                : n === 0
                  ? "Nothing identified to fetch again."
                  : `${n.toLocaleString()} identified title${n === 1 ? "" : "s"}`}
        </div>
      </div>
      {!refresh.isSuccess && n !== undefined && n > 0 && (
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={refresh.isPending}
            onClick={() =>
              refresh.mutate({ libraryID: library.id, scope: "language" })
            }
          >
            Refresh {n.toLocaleString()}
          </button>
        </div>
      )}
    </div>
  );
}
