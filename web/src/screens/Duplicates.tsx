import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useIsAdmin, useLibraries, usePhotoDuplicates, usePhotoNearCopies } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { PhotoViewer } from "@/components/PhotoViewer";
import { RemoveDialog } from "@/components/RemoveDialog";
import type { DuplicateCopy, DuplicateGroup, Item, NearCopyGroup } from "@/api/types";
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

/*
 * Which groups are the tidy-up and which are filing.
 *
 * On a real library, 10 of 37 groups had every copy in one album — four
 * identical copies of one photo in one folder — and the other 27 were the same
 * photo kept in two albums. The first kind is almost always an accident and the
 * second often a decision, so they are shown apart, accidents first. Nothing is
 * hidden: the second section is the same list, labelled for what it probably
 * is. Order within each is the server's, largest group first.
 */
export function splitGroups(groups: DuplicateGroup[]): {
  sameAlbum: DuplicateGroup[];
  acrossAlbums: DuplicateGroup[];
} {
  const sameAlbum: DuplicateGroup[] = [];
  const acrossAlbums: DuplicateGroup[] = [];
  for (const g of groups) {
    const albums = new Set(g.copies.map((c) => c.album ?? null));
    (albums.size === 1 ? sameAlbum : acrossAlbums).push(g);
  }
  return { sameAlbum, acrossAlbums };
}

/** A photo's size in pixels, as a person reads it: "3648 × 2736". */
export function dimensions(item: Item): string {
  return item.width && item.height ? `${item.width} × ${item.height}` : "Size unknown";
}

const area = (item: Item) => (item.width ?? 0) * (item.height ?? 0);

/**
 * Which copies to label Largest: every copy at the biggest size, and only when
 * some copy is smaller. Half the groups on a real library were the same
 * picture saved again at the same size, and marking one of those "Largest"
 * named a difference that was not there. Exported for the test.
 */
export function largestIDs(group: NearCopyGroup): Set<number> {
  const sizes = group.copies.map((c) => area(c.item));
  const top = Math.max(...sizes);
  if (sizes.every((a) => a === top)) return new Set();
  return new Set(group.copies.filter((c) => area(c.item) === top).map((c) => c.item.id));
}

/**
 * What removing one near copy leaves, in a sentence, by size: another copy as
 * big stays, or a bigger one does, or this was the only biggest and the
 * picture is kept smaller. Exported for the test.
 */
export function nearRemovalNote(group: NearCopyGroup, copy: DuplicateCopy): string {
  const others = group.copies.filter((c) => c.item.id !== copy.item.id);
  if (others.length === 0) return "";
  const mine = area(copy.item);
  const place = (c: DuplicateCopy) => (c.album ? `"${c.album}"` : ROOT);
  const same = others.find((c) => area(c.item) === mine);
  const bigger = others
    .filter((c) => area(c.item) > mine)
    .sort((a, b) => area(b.item) - area(a.item))[0];
  if (bigger) return `The ${dimensions(bigger.item)} copy stays in ${place(bigger)}.`;
  if (same) return `Another copy at the same size stays in ${place(same)}.`;
  const next = [...others].sort((a, b) => area(b.item) - area(a.item))[0];
  return `This is the largest copy. The ${dimensions(next.item)} copy stays, so the picture is kept smaller.`;
}

function NearGroup({
  group,
  admin,
  onShow,
  onRemove,
}: {
  group: NearCopyGroup;
  admin: boolean;
  onShow: (photos: Item[], at: number) => void;
  onRemove: (copy: DuplicateCopy) => void;
}) {
  const photos = group.copies.map((c) => c.item);
  const largest = largestIDs(group);
  return (
    <section className="dupes__group">
      <h2 className="dupes__group-head">
        {group.copies.length} versions
      </h2>
      <div className="dupes__copies">
        {group.copies.map((c, i) => (
          <div key={c.item.id} className="dupes__copy">
            <PosterTile item={c.item} onOpen={() => onShow(photos, i)} />
            <div className="dupes__size">
              {dimensions(c.item)}
              {largest.has(c.item.id) && <span className="dupes__largest">Largest</span>}
            </div>
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
  const { data: near } = usePhotoNearCopies(libraryID);
  const [shown, setShown] = useState<{ photos: Item[]; at: number } | null>(null);
  const [removing, setRemoving] = useState<{ copy: DuplicateCopy; note: string } | null>(null);
  const nearGroups = near?.groups ?? [];

  const groups = data?.groups ?? [];
  const { sameAlbum, acrossAlbums } = splitGroups(groups);
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
      {!isLoading && groups.length === 0 && nearGroups.length === 0 && (
        <p className="dupes__empty">
          No duplicates. A photo is compared once LANcast has read it, so a
          library that is still being processed may show more later.
        </p>
      )}

      {[
        {
          key: "same",
          title: "In the same album",
          note: "Every copy sits in one album, so these are most likely accidental.",
          list: sameAlbum,
        },
        {
          key: "across",
          title: "Filed in more than one album",
          note: "The same photo kept in different albums, which is often on purpose.",
          list: acrossAlbums,
        },
      ]
        .filter((s) => s.list.length > 0)
        .map((s) => (
          <section key={s.key} className="dupes__section">
            <h2 className="dupes__section-head">
              {s.title}
              <span>
                {s.list.length} {s.list.length === 1 ? "group" : "groups"}
              </span>
            </h2>
            <p className="dupes__section-note">{s.note}</p>
            <div className="dupes__groups">
              {s.list.map((g) => (
                <Group
                  key={g.sha256}
                  group={g}
                  admin={admin}
                  onShow={(photos, at) => setShown({ photos, at })}
                  onRemove={(copy) => setRemoving({ copy, note: removalNote(g, copy) })}
                />
              ))}
            </div>
          </section>
        ))}

      {/* Near copies, after the exact ones and never mixed with them: those
          are the same file, these are a measured judgement (ADR 0075
          amendment). Nothing here is removed for anyone; the largest is
          marked as the one to keep. */}
      {(nearGroups.length > 0 || (near?.pending ?? 0) > 0) && (
        <section className="dupes__section">
          <h2 className="dupes__section-head">
            Probably the same picture
            {nearGroups.length > 0 && (
              <span>
                {nearGroups.length} {nearGroups.length === 1 ? "group" : "groups"}
              </span>
            )}
          </h2>
          <p className="dupes__section-note">
            Not the same file: the same picture saved at another size or saved
            again. The largest version is marked. Bursts, crops and edited
            copies are left out.
          </p>
          {(near?.pending ?? 0) > 0 && (
            <p className="dupes__section-note">
              {near!.pending} {near!.pending === 1 ? "photo hasn't" : "photos haven't"} been
              compared yet. Photos are compared once they are indexed for
              search, so press Index photos on the library if this stays.
            </p>
          )}
          <div className="dupes__groups">
            {nearGroups.map((g) => (
              <NearGroup
                key={g.keep}
                group={g}
                admin={admin}
                onShow={(photos, at) => setShown({ photos, at })}
                onRemove={(copy) => setRemoving({ copy, note: nearRemovalNote(g, copy) })}
              />
            ))}
          </div>
        </section>
      )}

      {shown && (
        <PhotoViewer photos={shown.photos} startAt={shown.at} onClose={() => setShown(null)} />
      )}
      {removing && (
        <RemoveDialog
          item={removing.copy.item}
          note={removing.note}
          onClose={() => setRemoving(null)}
          onDone={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
