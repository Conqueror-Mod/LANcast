// Container kinds hold other items rather than a playable file: a show holds
// seasons, a season holds episodes, a collection holds films, a serial holds
// its parts (ADR 0010, ADR 0017). Everything else is a leaf you can play.
//
// The set is closed on purpose: `kind` is an open set (ADR 0018), and an
// unfamiliar kind must degrade to "playable leaf" rather than vanish — better a
// stray Play button on an unknown type than a dead-end tile with no action.
const CONTAINER_KINDS = new Set(["show", "season", "collection", "serial"]);

export function isContainer(kind: string): boolean {
  return CONTAINER_KINDS.has(kind);
}

// childLabel names what a container holds, for the section heading over its
// children grid.
export function childLabel(kind: string): string {
  switch (kind) {
    case "show":
      return "Seasons";
    case "season":
      return "Episodes";
    case "collection":
      return "Films";
    case "serial":
      return "Parts";
    default:
      return "Contents";
  }
}
