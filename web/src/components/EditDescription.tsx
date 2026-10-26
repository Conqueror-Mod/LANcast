import { useState } from "react";
import { useSetOverview } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import { errorMessage } from "@/lib/errors";
import type { Item } from "@/api/types";
import "./AddToPlaylist.css";
import "./EditDescription.css";

/*
 * Writing a collection's description by hand.
 *
 * A franchise usually arrives with one from its provider. A keyword-defined
 * grouping never does — a keyword is a tag with nothing behind it — so this is
 * the only way the Marvel Cinematic Universe gets a sentence under its title.
 * Saving locks the field, so no refresh writes over it afterwards.
 *
 * The playlist dialog's styling, for the same reason RenamePlaylist borrows
 * it: one dialog language for one text field.
 */
export function EditDescription({
  item,
  onClose,
}: {
  item: Item;
  onClose: () => void;
}) {
  const save = useSetOverview(item.id);
  const [text, setText] = useState(item.overview ?? "");
  useBackHandler(onClose);

  const submit = () => {
    const next = text.trim();
    if (next === (item.overview ?? "").trim()) return onClose();
    save.mutate(next, { onSuccess: onClose });
  };

  return (
    <div className="addpl__overlay" onClick={onClose}>
      <div
        className="addpl editdesc"
        role="dialog"
        aria-label={`Description of ${item.title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="addpl__head">
          <span className="section-label">Description</span>
          <button className="addpl__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        {save.isError && <p className="addpl__error">{errorMessage(save.error)}</p>}

        <textarea
          className="addpl__input editdesc__text"
          value={text}
          autoFocus
          rows={6}
          aria-label="Description"
          disabled={save.isPending}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            // Enter is a new line in a paragraph; Ctrl+Enter saves.
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) submit();
          }}
        />
        <p className="editdesc__hint">
          Saved descriptions are kept through every metadata refresh.
        </p>
        <div className="editdesc__actions">
          <button className="addpl__create" onClick={submit} disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}
