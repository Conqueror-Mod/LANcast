import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useCreatePlaylist,
  useDeletePlaylist,
  useLibraries,
  usePlaylistEntries,
  usePlaylists,
  useRenamePlaylist,
} from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useFocusable, useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./Playlists.css";

/*
 * The playlists page.
 *
 * Playlists could be made, edited and played before this screen existed, and
 * were nearly impossible to *find*: a playlist is filed in a library whose top
 * level is artists (ADR 0024), so nothing listed it. You could reach one by a
 * search that happened to match its name, or a recently-added shelf that
 * happened to still carry it. Both are accidents, and a feature reachable only
 * by accident is the same failure as a button on a page nothing navigates to.
 *
 * Scoped to one library rather than global. A playlist belongs to the library
 * its tracks and its .m3u live in; a global page would have to invent a
 * grouping across libraries or silently mix them, and per-library can widen
 * later without breaking a page anyone has come to rely on.
 */

// The quilt: a tile made from the covers of the first few entries.
//
// A playlist has no artwork and never will — there is no provider to ask what a
// list somebody named "The Gym One" looks like. Composed client-side from
// entries already fetched, so this costs no new server surface, no cache entry,
// and nothing to invalidate when the playlist changes.
function PlaylistArt({ playlist }: { playlist: Item }) {
  // Only the entries this tile needs. The query is the same one the detail page
  // uses, so opening a playlist after seeing it here is served from cache.
  const { data: entries } = usePlaylistEntries(playlist.id, true);
  const covers = (entries ?? [])
    .map((e) => artworkURL(e.artwork?.poster, "thumb"))
    .filter((u): u is string => Boolean(u))
    .slice(0, 4);

  // An empty list, or one whose tracks have no covers, gets its initial rather
  // than a blank rectangle. A grid of grey holes reads as broken; a lettered
  // tile reads as a playlist that has not been filled in yet, which is what it
  // is.
  if (covers.length === 0) {
    return (
      <div className="pl-tile__art pl-tile__art--empty" aria-hidden="true">
        <span>{(playlist.title ?? "?").slice(0, 1).toUpperCase()}</span>
      </div>
    );
  }

  // One cover fills the tile; two, three or four make the quilt. Three is
  // deliberately not padded to four with a blank — the odd one out spans, so a
  // three-track list looks composed rather than missing a corner.
  return (
    <div
      className={`pl-tile__art pl-tile__art--n${covers.length}`}
      aria-hidden="true"
    >
      {covers.map((src, i) => (
        <img key={i} src={src} alt="" draggable={false} />
      ))}
    </div>
  );
}

function PlaylistTile({
  playlist,
  onOpen,
  onRename,
  onDelete,
}: {
  playlist: Item;
  onOpen: () => void;
  onRename: () => void;
  onDelete: () => void;
}) {
  const focusable = useFocusable(onOpen);
  const n = playlist.child_count ?? 0;

  return (
    <div className="pl-tile">
      <button
        {...focusable}
        className="pl-tile__open"
        onClick={onOpen}
        aria-label={`Open ${playlist.title}`}
      >
        <PlaylistArt playlist={playlist} />
        <span className="pl-tile__title">{playlist.title}</span>
        {/* The count comes from the server now: child_count is the entry count
            for a playlist, repeats included. It read 0 for every playlist until
            v0.6.12, which is why this line could not exist before. */}
        <span className="pl-tile__count">
          {n === 1 ? "1 track" : `${n} tracks`}
        </span>
      </button>
      <div className="pl-tile__acts">
        <button
          className="pl-tile__act"
          onClick={onRename}
          aria-label={`Rename ${playlist.title}`}
          title="Rename"
        >
          ✎
        </button>
        <button
          className="pl-tile__act"
          onClick={onDelete}
          aria-label={`Delete ${playlist.title}`}
          title="Delete"
        >
          ×
        </button>
      </div>
    </div>
  );
}

/** Rename, and the confirm step of a delete, share this one-field row. */
function TitlePrompt({
  label,
  initial,
  confirmLabel,
  onConfirm,
  onCancel,
}: {
  label: string;
  initial: string;
  confirmLabel: string;
  onConfirm: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initial);
  useBackHandler(onCancel);
  return (
    <div className="pl-prompt__overlay" onClick={onCancel}>
      <div
        className="pl-prompt"
        role="dialog"
        aria-label={label}
        onClick={(e) => e.stopPropagation()}
      >
        <span className="section-label">{label}</span>
        <input
          className="pl-prompt__input"
          value={value}
          autoFocus
          aria-label={label}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && value.trim()) onConfirm(value.trim());
            if (e.key === "Escape") onCancel();
          }}
        />
        <div className="pl-prompt__row">
          <button className="pl-prompt__cancel" onClick={onCancel}>
            Cancel
          </button>
          <button
            className="pl-prompt__ok"
            disabled={value.trim() === ""}
            onClick={() => onConfirm(value.trim())}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

function RenameTile({
  playlist,
  onDone,
}: {
  playlist: Item;
  onDone: () => void;
}) {
  const rename = useRenamePlaylist(playlist.id);
  return (
    <TitlePrompt
      label="Rename playlist"
      initial={playlist.title}
      confirmLabel="Rename"
      onCancel={onDone}
      onConfirm={(title) => rename.mutate(title, { onSuccess: onDone })}
    />
  );
}

function DeleteTile({
  playlist,
  onDone,
}: {
  playlist: Item;
  onDone: () => void;
}) {
  const del = useDeletePlaylist(playlist.id);
  useBackHandler(onDone);
  return (
    <div className="pl-prompt__overlay" onClick={onDone}>
      <div
        className="pl-prompt"
        role="dialog"
        aria-label={`Delete ${playlist.title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <span className="section-label">Delete playlist</span>
        <p className="pl-prompt__title">{playlist.title}</p>
        {/* Said plainly, because the word "delete" beside a list of music is
            frightening in a way this action does not deserve: the tracks are
            files in the library and this removes a list, not a note. */}
        <p className="pl-prompt__sub">
          The list goes. The tracks in it stay in your library.
        </p>
        <div className="pl-prompt__row">
          <button className="pl-prompt__cancel" onClick={onDone}>
            Cancel
          </button>
          <button
            className="pl-prompt__ok pl-prompt__ok--danger"
            disabled={del.isPending}
            onClick={() => del.mutate(undefined, { onSuccess: onDone })}
          >
            {del.isPending ? "Deleting…" : "Delete"}
          </button>
        </div>
      </div>
    </div>
  );
}

export function Playlists() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);

  const { data: playlists, isLoading } = usePlaylists(libraryID);
  const create = useCreatePlaylist();
  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState<Item | null>(null);
  const [deleting, setDeleting] = useState<Item | null>(null);

  const back = () => navigate(`/library/${libraryID}`);
  useBackHandler(back);

  return (
    <div className="browse">
      <div className="browse__head">
        <button className="pl-back" onClick={back}>
          ← {library?.name ?? "Library"}
        </button>
        <h1 className="browse__title">Playlists</h1>
        <span className="browse__count">
          {playlists ? `${playlists.length}` : ""}
        </span>
      </div>

      <div className="browse__filters">
        <button className="browse__playall-btn" onClick={() => setCreating(true)}>
          <span aria-hidden="true">+</span> New playlist
        </button>
      </div>

      {/* An empty state that says how a playlist comes into being, because on
          this page the answer is not obvious: they are usually born from a
          track, and the .m3u route is invisible unless you know it exists. */}
      {!isLoading && (playlists ?? []).length === 0 && (
        <p className="browse__message">
          No playlists in this library yet. Make one here, add tracks to it from
          any song, or drop an <code>.m3u</code> file in the library and rescan.
        </p>
      )}

      <div className="pl-grid">
        {(playlists ?? []).map((p) => (
          <PlaylistTile
            key={p.id}
            playlist={p}
            onOpen={() => navigate(`/item/${p.id}`)}
            onRename={() => setRenaming(p)}
            onDelete={() => setDeleting(p)}
          />
        ))}
      </div>

      {creating && (
        <TitlePrompt
          label="New playlist"
          initial=""
          confirmLabel="Create"
          onCancel={() => setCreating(false)}
          onConfirm={(title) =>
            create.mutate(
              { title, library_id: libraryID },
              {
                onSuccess: (playlist) => {
                  setCreating(false);
                  // Straight into it. A playlist made from this page is empty,
                  // and the useful next step is adding to it — which happens on
                  // its own page, not here.
                  navigate(`/item/${playlist.id}`);
                },
              },
            )
          }
        />
      )}
      {renaming && (
        <RenameTile playlist={renaming} onDone={() => setRenaming(null)} />
      )}
      {deleting && (
        <DeleteTile playlist={deleting} onDone={() => setDeleting(null)} />
      )}
    </div>
  );
}
