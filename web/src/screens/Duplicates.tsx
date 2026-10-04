import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useIsAdmin, useLibraries, usePhotoDuplicates } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { PhotoViewer } from "@/components/PhotoViewer";
import { RemoveDialog } from "@/components/RemoveDialog";
import type { DuplicateCopy, DuplicateGroup, Item } from "@/api/types";
import { formatBytes } from "@/lib/format";
import "./Duplicates.css";

/*
 * A picture library's exact duplicates (ADR 0075).
 *
 * # It says where, and not which
 *
 * Measured on a real library before this was built: 38 groups of identical
 * photos, and most of them the same photo filed in two albums — Animals and
 * Me & Us. That is somebody's filing, not a mistake. So every copy carries its
 * album, and the page never picks a copy to keep. Removing one says what it
 * means: which album the photo leaves, and where a copy stays.
 *
 * # Removal is the ordinary removal
 *
 * The same dialog and the same DELETE as anywhere else: admin only, "remove
 * from library" keeps the file, "delete from disk" is refused by the server
 * when media deletion is off. This page adds a list, not a power.
 */

const ROOT = "the library root";

function albumName(c: DuplicateCopy): string {
  return c.album ?? ROOT;
}

/**
 * What removing one copy does to the albums, in a sentence.
 * Exported for the test, which is the only place this logic is stated.
 */
export function removalNote(group: DuplicateGroup, copy: DuplicateCopy): string {
  const here = albumName(copy);
  const others = group.copies.filter((c) => c.item.id !== copy.item.id);
  if (others.some((c) => albumName(c) === here)) {
    return `Another copy stays in ${here === ROOT ? here : `"${here}"`}.`;
  }
  const kept = [...new Set(others.map(albumName))]
    .map((a) => (a === ROOT ? a : `"${a}"`))
    .join(", ");
  const from = here === ROOT ? ROOT : `"${here}"`;
  return `This removes the photo from ${from}. A copy stays in ${kept}.`;
}

function Group({
  group,
  admin,
  onShow,
  onRemove,
}: {
  group: DuplicateGroup;
  admin: boolean;
  onShow: (photos: Item[], at: number) => void;
  onRemove: (copy: DuplicateCopy) => void;
}) {
  const photos = group.copies.map((c) => c.item);
  return (
    <section className="dupes__group">
      <h2 className="dupes__group-head">
        {group.copies.length} copies
        <span>{formatBytes(group.size_bytes)} each</span>
      </h2>
      <div className="dupes__copies">
        {group.copies.map((c, i) => (
          <div key={c.item.id} className="dupes__copy">
            <PosterTile item={c.item} onOpen={() => onShow(photos, i)} />
            <div className="dupes__album">{c.album ?? "Library root"}</div>
            {admin && (
              <button className="dupes__remove" onClick={() => onRemove(c)}>
                Remove…
              </button>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

export function Duplicates() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const admin = useIsAdmin();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data, isLoading } = usePhotoDuplicates(libraryID);
  const [shown, setShown] = useState<{ photos: Item[]; at: number } | null>(null);
  const [removing, setRemoving] = useState<{ group: DuplicateGroup; copy: DuplicateCopy } | null>(
    null,
  );

  const groups = data?.groups ?? [];
  return (
    <div className="dupes">
      <div className="dupes__bar">
        <button className="dupes__back" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1 className="dupes__title">
          {library?.name ?? "Photos"} <span>duplicates</span>
        </h1>
        {data && data.extra_copies > 0 && (
          <span className="dupes__total">
            {data.extra_copies} extra {data.extra_copies === 1 ? "copy" : "copies"}
          </span>
        )}
      </div>

      <p className="dupes__lede">
        Photos that are the same file, byte for byte. The same photo kept in two
        albums shows up here too, so check where each copy lives before removing
        one.
      </p>

      {isLoading && <p className="dupes__empty">Comparing photos…</p>}
      {!isLoading && groups.length === 0 && (
        <p className="dupes__empty">
          No duplicates. A photo is compared once LANcast has read it, so a
          library that is still being processed may show more later.
        </p>
      )}

      {groups.map((g) => (
        <Group
          key={g.sha256}
          group={g}
          admin={admin}
          onShow={(photos, at) => setShown({ photos, at })}
          onRemove={(copy) => setRemoving({ group: g, copy })}
        />
      ))}

      {shown && (
        <PhotoViewer photos={shown.photos} startAt={shown.at} onClose={() => setShown(null)} />
      )}
      {removing && (
        <RemoveDialog
          item={removing.copy.item}
          note={removalNote(removing.group, removing.copy)}
          onClose={() => setRemoving(null)}
          onDone={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
