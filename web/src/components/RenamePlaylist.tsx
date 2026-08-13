import { useState } from "react";
import { useRenamePlaylist } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./AddToPlaylist.css";

/*
 * Renaming a playlist, from the playlist.
 *
 * Rename already existed on the Playlists page, on the tile. It did not exist
 * on the playlist's own page — where Delete did, which is the wrong pair of
 * capabilities to offer: the destructive one available and the harmless one a
 * screen away.
 *
 * Borrows the picker's dialog styling rather than growing a third dialog
 * language for one text field.
 */
export function RenamePlaylist({
  playlist,
  onClose,
}: {
  playlist: Item;
  onClose: () => void;
}) {
  const rename = useRenamePlaylist(playlist.id);
  const [title, setTitle] = useState(playlist.title);
  useBackHandler(onClose);

  const save = () => {
    const next = title.trim();
    if (!next || next === playlist.title) return onClose();
    rename.mutate(next, { onSuccess: onClose });
  };

  return (
    <div className="addpl__overlay" onClick={onClose}>
      <div
        className="addpl"
        role="dialog"
        aria-label={`Rename ${playlist.title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="addpl__head">
          <span className="section-label">Rename playlist</span>
          <button className="addpl__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        {rename.isError && (
          <p className="addpl__error">
            {(rename.error as Error)?.message ?? "Could not rename this playlist."}
          </p>
        )}

        <div className="addpl__new">
          <input
            className="addpl__input"
            value={title}
            autoFocus
            aria-label="Playlist name"
            disabled={rename.isPending}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") save();
              if (e.key === "Escape") onClose();
            }}
          />
          <button
            className="addpl__create"
            onClick={save}
            disabled={rename.isPending || title.trim() === ""}
          >
            {rename.isPending ? "Saving…" : "Rename"}
          </button>
        </div>
      </div>
    </div>
  );
}
