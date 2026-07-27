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
