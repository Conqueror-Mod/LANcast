import type { PeerPerson, PeerPresence } from "@/api/types";

/*
 * The sentence for one person, and a function rather than a ternary in the
 * markup because there are four cases and three of them are easy to collapse by
 * accident.
 *
 * Withholding presence is a *choice* and must never be rendered as an absence
 * or as being offline — the rule the local list already holds itself to. And
 * somebody can only be known to be offline if they share; otherwise there is
 * nothing to know, and "Offline" would be inventing a fact about a person who
 * has told us nothing.
 *
 * It said "Not sharing with you", three lines above a control about sharing
 * *libraries*, on a card that by then carried both halves of that word. It was
 * read — correctly, for the sentence it appeared to be — as "they have shared
 * no libraries", while four of them were sitting in the rail. Presence says
 * what it is about now, and nothing on this card says "sharing" unqualified.
 */
export function statusOf(person: PeerPerson, reachable: boolean, reachesUs = false): string {
  // A friend whose server sends us its roster but cannot be called (ADR 0103):
  // what they watch cannot be asked, so "not showing you" would invent a
  // choice. Checked first for exactly that reason.
  if (!reachable && reachesUs) return "What they watch can't be seen from here";
  if (!person.shares) return "Not showing you what they watch";
  if (!reachable) return "Server not answering";
  if (!person.online) return "Offline";
  if (person.watching) return `Watching ${person.watching}`;
  return "Online";
}

/*
 * Whether you could join what this person is watching: the same test the
 * People screen has always used before offering Ask to join. The server
 * refuses a request without a presence grant (ADR 0045 §7) either way; this
 * only decides whether a button is drawn.
 */
export function canJoin(person: PeerPerson, reachable: boolean): boolean {
  return reachable && person.shares && !!person.online && !!person.watching;
}

/** One person on one paired server, with what is true about them now. */
export type FriendRow = {
  peer: PeerPresence;
  person: PeerPerson;
  status: string;
  joinable: boolean;
};

/*
 * The friends panel's rows (ADR 0118 §4): somebody watching first, then
 * somebody online, then the rest, so the row worth acting on is at the top.
 * A server that answers nothing and sends nothing is one row of its own --
 * "can't reach" said in words, never its people drawn as merely quiet.
 */
export function friendRows(peers: PeerPresence[]): {
  rows: FriendRow[];
  unreachable: PeerPresence[];
} {
  const rows: FriendRow[] = [];
  const unreachable: PeerPresence[] = [];
  for (const peer of peers) {
    const reachesUs = peer.reaches_us ?? false;
    if (!peer.reachable && !reachesUs) {
      unreachable.push(peer);
      continue;
    }
    for (const person of peer.people) {
      rows.push({
        peer,
        person,
        status: statusOf(person, peer.reachable, reachesUs),
        joinable: canJoin(person, peer.reachable),
      });
    }
  }
  const rank = (r: FriendRow) => (r.joinable ? 0 : r.person.watching ? 1 : r.person.online ? 2 : 3);
  rows.sort((a, b) => rank(a) - rank(b));
  return { rows, unreachable };
}
