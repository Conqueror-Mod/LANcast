import { Link } from "react-router-dom";
import { AskToJoin } from "./AskToJoin";
import { PosterTile } from "./PosterTile";
import { friendRows } from "@/lib/presence";
import type { Item, PeerPresence } from "@/api/types";
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

/** How many photographs the panel shows before pointing at the rest. */
const MEMORIES_SHOWN = 4;

export function MemoryPanel({ items }: { items: Item[] }) {
  if (items.length === 0) return null;
  const shown = items.slice(0, MEMORIES_SHOWN);
  // Said only when every photograph shown is from the same year: "3 years
  // ago" over pictures from three different years would be true of one.
  const years = new Set(shown.map((it) => (it.taken_at ? yearsAgo(it.taken_at) : 0)));
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
        {shown.map((it) => (
          <PosterTile key={it.id} item={it} />
        ))}
      </div>
    </section>
  );
}
