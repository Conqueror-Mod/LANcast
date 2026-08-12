import type { Item } from "@/api/types";

// Container kinds hold other items rather than a playable file: a show holds
// seasons, a season holds episodes, a collection holds films, a serial holds
// its parts (ADR 0010, ADR 0017). Everything else is a leaf you can play.
//
// The set is closed on purpose: `kind` is an open set (ADR 0018), and an
// unfamiliar kind falls through to the child-count check below rather than
// vanishing.
const CONTAINER_KINDS = new Set([
  "show",
  "season",
  "collection",
  "serial",
  // A playlist holds entries through playlist_entry, not parent_id, so its
  // child_count is 0 and the fallback below would call it a leaf — an item with
  // a Play button and no file behind it (ADR 0030). Named, like a gallery, for
  // exactly that reason.
  "playlist",
  // A gallery holds photos (ADR 0028). Named rather than left to the
  // child-count fallback, so an empty gallery mid-scan reads as a container
  // with nothing in it rather than as something to open.
  "gallery",
  // Music (ADR 0024): an artist holds albums, an album holds tracks. Named
  // rather than left to the child-count fallback, so an album mid-scan — rows
  // created, tracks not yet parented — reads as a container with nothing in it
  // rather than as a playable leaf offering a Play button for a file it has no
  // path to.
  "artist",
  "album",
]);

// isContainer decides whether an item should present its children rather than a
// Play button. A multi-part work is a `movie` whose parent-ness shows only in
// its child count (ADR 0017) — so kind alone is not enough, and an item with
// children is a container whatever its kind.
export function isContainer(item: Item): boolean {
  return CONTAINER_KINDS.has(item.kind) || (item.child_count ?? 0) > 0;
}

// childLabel names what a container holds, taken from the children themselves so
// a movie-parent of parts reads "Parts", not the generic fallback.
export function childLabel(childKind: string | undefined): string {
  switch (childKind) {
    case "season":
      return "Seasons";
    case "episode":
      return "Episodes";
    case "part":
      return "Parts";
    case "chapter":
      return "Chapters";
    case "movie":
      return "Films";
    case "album":
      return "Albums";
    case "track":
      return "Tracks";
    case "photo":
      return "Photos";
    default:
      return "Contents";
  }
}

// childCountLabel renders "12 tracks" / "1 track" for a list of children whose
// kind is known — the detail page's meta line, which had been rendering the
// plural label unconditionally and saying "1 tracks" on every single-track
// album. Singular from the count, plural from the label, which is already the
// right word for every kind childLabel knows.
export function childCountLabel(n: number, childKind: string | undefined): string {
  const plural = childLabel(childKind).toLowerCase();
  if (n !== 1) return `${n} ${plural}`;
  // "Contents" is the fallback label and has no sensible singular; it is also
  // the only one that is not a plain plural, so it is the only special case.
  const singular = plural === "contents" ? "item" : plural.replace(/s$/, "");
  return `1 ${singular}`;
}

// containerNoun is the singular word for what a container of this kind holds,
// derived from the container's own kind — the children's kind is not on the
// parent row in a grid listing. A movie-kind parent of parts (ADR 0017) reads
// "part"; a collection reads "film".
function containerNoun(kind: string): string {
  switch (kind) {
    case "show":
      return "season";
    case "collection":
      return "film";
    case "serial":
    case "movie":
      return "part";
    case "artist":
      return "album";
    case "album":
      return "track";
    case "gallery":
      return "photo";
    default:
      return "item";
  }
}

// The music kinds (ADR 0024). Listening is not watching, and a hub that mixes
// the two is the one place that difference becomes obvious: a row of tracks
// sitting among films reads as a fault in the films.
const MUSIC_KINDS = new Set(["artist", "album", "track"]);

export function isMusic(item: Item): boolean {
  return MUSIC_KINDS.has(item.kind);
}

// The picture kinds (ADR 0028). Home keeps them in their own row for the reason
// music has one: a photograph among films is not a film that failed to load,
// and a square crop beside a 2:3 poster is a row with no shared baseline.
const PICTURE_KINDS = new Set(["gallery", "photo"]);

export function isPicture(item: Item): boolean {
  return PICTURE_KINDS.has(item.kind);
}

// Kinds whose artwork is square rather than a 2:3 poster. A record sleeve is
// square, and an artist wearing a borrowed album cover (ADR 0025) is square by
// inheritance — so both frame square until artist images arrive from a provider,
// and that ADR revisits this line when they do.
//
// Today this is exactly the music set, and it is still written separately: they
// agree by coincidence of the current media types, not by definition. A square
// non-music kind would otherwise silently become music.
const SQUARE_ART_KINDS = new Set([
  "artist",
  "album",
  "track",
  // Pictures are framed square too, for a different reason than sleeves are:
  // a photo library is a mix of portrait and landscape, and a grid that lets
  // every tile keep its own aspect is a ragged grid — the fault the music and
  // film split was made to avoid. Square crops all of them equally.
  "gallery",
  "photo",
]);

// isSquareArt picks the tile's aspect ratio from what the art actually is.
//
// This is not cosmetic. A square cover in a 2:3 frame is cropped by
// object-fit: cover, which quietly removes a third of every sleeve — and the
// result still looks like a working grid, which is why it needs deciding here
// rather than being noticed later.
export function isSquareArt(item: Item): boolean {
  return SQUARE_ART_KINDS.has(item.kind);
}

// containerCountLabel renders a container's child count as a short noun phrase —
// "3 seasons", "2 parts", "1 film" — or null for a leaf, so a poster tile can
// show at a glance that opening it leads to more items rather than a Play button.
export function containerCountLabel(item: Item): string | null {
  const n = item.child_count ?? 0;
  if (!isContainer(item) || n <= 0) return null;
  const noun = containerNoun(item.kind);
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}
