import { Link } from "react-router-dom";
import { AskToJoin } from "./AskToJoin";
import { PosterTile } from "./PosterTile";
import type { MenuAction } from "./Menu";
import { friendRows } from "@/lib/presence";
import type { FinishedMemory, Item, PeerPresence } from "@/api/types";
import "./BridgePanels.css";

/*
 * The Bridge's two smaller panels (ADR 0118 §4): friends on paired servers,
 * and On this day. The home page decides whether each appears, so neither
 * ever draws an empty frame.
 */

/** How many friends the panel lists before pointing at People. */
const FRIENDS_SHOWN = 4;

/*
 * Whether the friends panel has anything to say.
 *
 * With paired servers, always: the roster is worth a glance even when nobody
 * is watching. With none, only to somebody who can pair -- an administrator --
 * because to anybody else "Pair with a friend" is a button that would fail.
 */
export function showFriends(peers: PeerPresence[] | undefined, admin: boolean): boolean {
  if (!peers) return false;
  return peers.length > 0 || admin;
}

export function FriendsPanel({ peers }: { peers: PeerPresence[] }) {
  if (peers.length === 0) {
    return (
      <section className="panel panel--friends" aria-labelledby="friends-title">
        <header className="resume__head">
          <h2 id="friends-title" className="resume__title">
            Friends
          </h2>
        </header>
        <p className="friends__idle">
          Pair with a friend's LANcast to see what they're watching and watch it together.
        </p>
        <Link className="friends__go" to="/settings?pane=peers">
          Pair with a friend
        </Link>
      </section>
    );
  }

  const { rows, unreachable } = friendRows(peers);
  const watching = rows.some((r) => r.person.watching);
  const shown = rows.slice(0, FRIENDS_SHOWN);
  return (
    <section className="panel panel--friends" aria-labelledby="friends-title">
      <header className="resume__head">
        <h2 id="friends-title" className="resume__title">
          Friends
        </h2>
        <Link className="resume__all" to="/people">
          People
        </Link>
      </header>
      {!watching && <p className="friends__idle">Nobody's watching right now.</p>}
      <ul className="friends__list">
        {shown.map((r) => (
          <li key={`${r.peer.fingerprint}/${r.person.id}`} className="friends__row">
            <span className="friends__who">
              <span className="friends__name">{r.person.name}</span>
              <span className="friends__status">{r.status}</span>
            </span>
            {r.joinable && (
              <AskToJoin fingerprint={r.peer.fingerprint} person={r.person.id} name={r.person.name} />
            )}
          </li>
        ))}
        {/*
          Said in words, never drawn as a quiet friend: a server that cannot be
          reached has hidden presence silently before (a one-way pairing, a dead
          first address), and the panel is not going to be the next place it
          hides.
        */}
        {unreachable.map((p) => (
          <li key={p.fingerprint} className="friends__row friends__row--down">
            <span className="friends__who">
              <span className="friends__name">{p.name}</span>
              <span className="friends__status">Can't reach this server</span>
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

/** Whole local years between a photograph and now, for "3 years ago". */
export function yearsAgo(takenAt: number, now = new Date()): number {
  return now.getFullYear() - new Date(takenAt * 1000).getFullYear();
}

/** How many tiles the panel shows, photographs and history together. */
const MEMORIES_SHOWN = 4;

/** At most this many from your history while there are photographs too. */
const WATCHED_BESIDE_PHOTOS = 2;

/** The word under a tile from your history, in that kind's own terms. */
export function finishedVerb(kind: string): string {
  if (kind === "book") return "Listened";
  if (kind === "comic") return "Read";
  return "Watched";
}

/*
 * What the panel shows (ADR 0119 §3a): what you finished on this date first,
 * because it is yours and rarer, then photographs. Two from history at most
 * while there are photographs to fill the rest, so neither source crowds out
 * the other.
 */
export function memoryTiles(
  photos: Item[],
  watched: FinishedMemory[],
): { item: Item; at: number; note?: string }[] {
  const fromHistory = watched
    .slice(0, photos.length > 0 ? WATCHED_BESIDE_PHOTOS : MEMORIES_SHOWN)
    .map((m) => ({ item: m.item, at: m.finished_at, note: finishedVerb(m.item.kind) }));
  const fromPhotos = photos
    .slice(0, MEMORIES_SHOWN - fromHistory.length)
    .map((it) => ({ item: it, at: it.taken_at ?? 0 }));
  return [...fromHistory, ...fromPhotos];
}

export function MemoryPanel({
  items,
  watched = [],
  itemActions,
}: {
  items: Item[];
  /** What you finished on this date in an earlier year. */
  watched?: FinishedMemory[];
  /** What a right-click on a tile offers. */
  itemActions?: (item: Item) => MenuAction[];
}) {
  const shown = memoryTiles(items, watched);
  if (shown.length === 0) return null;
  // Said only when every tile shown is from the same year: "3 years ago" over
  // things from three different years would be true of one.
  const years = new Set(shown.map((t) => (t.at ? yearsAgo(t.at) : 0)));
  const ago = years.size === 1 ? [...years][0] : 0;
  return (
    <section className="panel panel--memory" aria-labelledby="memory-title">
      <header className="resume__head">
        <h2 id="memory-title" className="resume__title">
          On this day
        </h2>
        {ago > 0 && (
          <span className="memory__ago">
            {ago === 1 ? "A year ago" : `${ago} years ago`}
          </span>
        )}
      </header>
      {/* Poster tiles, so a marked photograph is covered here as on every
          other Home surface (ADR 0051), and the D-pad reaches each one. */}
      <div className="memory__grid">
        {shown.map((t) => (
          <div className="memory__cell" key={t.item.id}>
            <PosterTile item={t.item} actions={itemActions} />
            {t.note && <span className="memory__note">{t.note}</span>}
          </div>
        ))}
      </div>
    </section>
  );
}
