import { useState } from "react";
import { useAddToPlaylist, useCreatePlaylist, usePlaylists } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./AddToPlaylist.css";

/*
 * "Add to playlist" — the only edit that starts somewhere other than the
 * playlist itself.
 *
 * It appends, which is why there is no position to choose here: a playlist is
 * an ordered sequence and the end is the one place a new track can go without
 * asking a question nobody wants asked mid-record (ADR 0030). Reordering is on
 * the playlist's own page, where the whole sequence is visible.
 *
 * Adding a track that is already in the list is allowed and is not a warning.
 * A playlist may hold the same track twice — a reprise, a set that opens and
 * closes with the same song — and this is the surface where that is expressed.
 * Guarding against it here would make the legitimate case unreachable to serve
 * a mistake that costs one press of × to undo.
 */
export function AddToPlaylist({
  item,
  onClose,
}: {
  item: Item;
  onClose: () => void;
}) {
  const { data: playlists, isLoading } = usePlaylists();
  const add = useAddToPlaylist();
  const create = useCreatePlaylist();
  const [newTitle, setNewTitle] = useState("");
  // What was just added, so the dialog says so rather than closing out from
  // under someone who may want to add the same track to a second list.
  const [added, setAdded] = useState<string | null>(null);
  useBackHandler(onClose);

  const busy = add.isPending || create.isPending;
  const error = (add.error ?? create.error) as Error | null;

  const addTo = (playlist: Item) =>
    add.mutate(
      { playlistID: playlist.id, itemIDs: [item.id] },
      { onSuccess: () => setAdded(playlist.title) },
    );

  // A new playlist is made in the item's own library, which is the only
  // defensible answer: a playlist made from a track belongs where the track
  // lives, and asking someone to pick a library in the middle of adding a song
  // is a question about the database, not about music.
  const createAndAdd = () => {
    const title = newTitle.trim();
    if (!title) return;
    create.mutate(
      { title, library_id: item.library_id },
      {
        onSuccess: (playlist) => {
          setNewTitle("");
          add.mutate(
            { playlistID: playlist.id, itemIDs: [item.id] },
            { onSuccess: () => setAdded(playlist.title) },
          );
        },
      },
    );
  };

  return (
    <div className="addpl__overlay" onClick={onClose}>
      <div
        className="addpl"
        role="dialog"
        aria-label={`Add ${item.title} to a playlist`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="addpl__head">
          <span className="section-label">Add to playlist</span>
          <button className="addpl__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        <p className="addpl__title">{item.title}</p>

        {error && <p className="addpl__error">{error.message}</p>}
        {added && <p className="addpl__done">Added to {added}.</p>}

        <div className="addpl__list">
          {isLoading && <p className="addpl__empty">Loading…</p>}
          {!isLoading && (playlists ?? []).length === 0 && (
            <p className="addpl__empty">
              No playlists yet. Name one below and this goes in it.
            </p>
          )}
          {(playlists ?? []).map((p) => (
            <button
              key={p.id}
              className="addpl__opt"
              disabled={busy}
              onClick={() => addTo(p)}
            >
              {p.title}
            </button>
          ))}
        </div>

        <div className="addpl__new">
          <input
            className="addpl__input"
            value={newTitle}
            placeholder="New playlist…"
            aria-label="New playlist name"
            disabled={busy}
            onChange={(e) => setNewTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") createAndAdd();
            }}
          />
          <button
            className="addpl__create"
            onClick={createAndAdd}
            disabled={busy || newTitle.trim() === ""}
          >
            Create
          </button>
        </div>
      </div>
    </div>
  );
}
