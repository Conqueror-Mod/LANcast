import { useDeleteItem } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./RemoveDialog.css";

// Two ways to remove a title. "Remove from library" is non-destructive — the
// files stay on disk and are added to an ignore list so a rescan does not bring
// the title back. "Delete from disk" removes the files permanently. A container
// (a show, a multi-part work) takes its whole subtree with it. onDone runs after
// a successful removal so the caller can navigate away from the now-gone item.
export function RemoveDialog({
  item,
  onClose,
  onDone,
}: {
  item: Item;
  onClose: () => void;
  onDone: () => void;
}) {
  const del = useDeleteItem(item.id);
  useBackHandler(onClose);

  const run = (mode: "ignore" | "delete") =>
    del.mutate(mode, { onSuccess: onDone });

  return (
    <div className="removedlg__overlay" onClick={onClose}>
      <div
        className="removedlg"
        role="dialog"
        aria-label={`Remove ${item.title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="removedlg__head">
          <span className="section-label">Remove</span>
          <button className="removedlg__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        <p className="removedlg__title">{item.title}</p>

        {del.isError && (
          <p className="removedlg__error">
            {(del.error as Error)?.message ?? "Could not remove this title."}
          </p>
        )}

        <button
          className="removedlg__opt"
          disabled={del.isPending}
          onClick={() => run("ignore")}
        >
          <span className="removedlg__opt-title">Remove from library</span>
          <span className="removedlg__opt-sub">
            Keeps the file on disk and stops it being re-added on the next scan.
          </span>
        </button>

        <button
          className="removedlg__opt removedlg__opt--danger"
          disabled={del.isPending}
          onClick={() => run("delete")}
        >
          <span className="removedlg__opt-title">Delete from disk</span>
          <span className="removedlg__opt-sub">
            Permanently deletes the file
            {item.child_count ? " and everything under it" : ""}. This cannot be
            undone.
          </span>
        </button>

        <button className="removedlg__cancel" onClick={onClose} disabled={del.isPending}>
          Cancel
        </button>
      </div>
    </div>
  );
}
