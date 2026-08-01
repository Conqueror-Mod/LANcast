import type { Item } from "@/api/types";

// Container kinds hold other items rather than a playable file: a show holds
// seasons, a season holds episodes, a collection holds films, a serial holds
// its parts (ADR 0010, ADR 0017). Everything else is a leaf you can play.
//
// The set is closed on purpose: `kind` is an open set (ADR 0018), and an
// unfamiliar kind falls through to the child-count check below rather than
// vanishing.
const CONTAINER_KINDS = new Set(["show", "season", "collection", "serial"]);

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
    default:
      return "Contents";
  }
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
    default:
      return "item";
  }
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
