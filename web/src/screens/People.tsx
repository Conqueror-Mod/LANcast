import { useState } from "react";
import { Link } from "react-router-dom";
import {
  useGrantPresence,
  usePeerPresence,
  usePeople,
  usePersonActivity,
} from "@/api/hooks";
import { artworkURL } from "@/api/client";
import type { PeerPerson, PeerPresence, Person } from "@/api/types";
import "./People.css";

/*
 * People on this server.
 *
 * The roadmap called this "Find Friends", which on a self-hosted household
 * server means the accounts already on it — there is no directory to search and
 * no second server to federate with. Naming it People rather than Friends is
 * the honest version of the same feature.
 *
 * Governed by ADR 0035: viewing is private by default and shared only by an
 * explicit opt-in. This page is where that decision becomes visible, so it says
 * plainly who has chosen not to share rather than showing an empty list — "has
 * not shared" and "watches nothing" are different statements, and a page that
 * cannot tell them apart accuses the private of being inactive.
 *
 * Since ADR 0045 it also lists people on **paired servers**, which is the half
 * of the page the header comment used to say did not exist. The discipline is
 * the same and now has to hold across a network: a peer that is switched off, a
 * person who has not granted you presence, and a person sitting idle look
 * identical if you only render a title, and they are three different facts.
 */
export function People() {
  const { data, isLoading } = usePeople();
  const people = data?.people ?? [];

  return (
    <div className="browse people">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">People</h1>
        <span className="browse__count">{people.length || ""}</span>
      </div>

      {isLoading && <p className="browse__message">Loading…</p>}

      {!isLoading && people.length === 0 && (
        <p className="browse__message">
          Nobody else has an account on this server yet. An administrator can add
          people in <strong>Settings → Users</strong>.
        </p>
      )}

      <div className="people__list">
        {people.map((p) => (
          <PersonCard key={p.id} person={p} />
        ))}
      </div>

      <PeersSection />

      {people.length > 0 && (
        <p className="people__foot">
          People choose for themselves whether to share what they have watched.
          Yours is off unless you turn it on, in{" "}
          <Link to="/settings?pane=account">Settings → Account</Link>.
        </p>
      )}
    </div>
  );
}

function PersonCard({ person }: { person: Person }) {
  const [open, setOpen] = useState(false);
  // Asked only when they share. The endpoint answers an empty list either way,
  // so requesting it otherwise is a call whose answer is already known.
  const { data } = usePersonActivity(person.id, person.sharing && open);
  const activity = data?.activity ?? [];

  return (
    <div className="people__card">
      <div className="people__head">
        <span className="people__avatar" aria-hidden="true">
          {initials(person.name)}
        </span>
        <div className="people__who">
          <span className="people__name">{person.name}</span>
          <span className="people__meta">
            {person.role === "admin" ? "administrator · " : ""}
            joined {new Date(person.joined_at * 1000).toLocaleDateString()}
          </span>
        </div>

        {person.sharing ? (
          <button
            className="people__toggle"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
          >
            {open ? "Hide" : `${person.watched} finished`}
          </button>
        ) : (
          /* Said as a choice, not an absence. */
          <span className="people__private">Not sharing</span>
        )}
      </div>

      {open && person.sharing && (
        <div className="people__activity">
          {activity.length === 0 ? (
            <p className="people__empty">Nothing finished yet.</p>
          ) : (
            activity.map((e) => {
              const poster = artworkURL(e.item.artwork?.poster, "thumb");
              return (
                <Link
                  className="people__row"
                  to={`/item/${e.item.id}`}
                  key={`${e.item.id}-${e.played_at}`}
                >
                  <span className="people__art">
                    {poster ? (
                      <img src={poster} alt="" loading="lazy" />
                    ) : (
                      <span aria-hidden="true">
                        {e.item.title.slice(0, 1).toUpperCase()}
                      </span>
                    )}
                  </span>
                  <span className="people__title">{e.item.title}</span>
                  <span className="people__when">
                    {new Date(e.played_at * 1000).toLocaleDateString()}
                  </span>
                </Link>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}

/*
 * Initials rather than an uploaded picture.
 *
 * An upload is a file store, a size limit, a format whitelist and a moderation
 * question, for a household of four. Initials need none of that and are legible
 * at every size the page uses.
 */
function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

/*
 * People on other servers.
 *
 * Rendered under its own heading rather than mixed into the local list, because
 * a name on another machine is a different kind of thing: you cannot be an
 * administrator of it, their history is not yours to open, and whether you can
 * see them at all is a decision made on a server you do not run.
 */
function PeersSection() {
  const { data, isLoading } = usePeerPresence();
  const peers = data?.peers ?? [];

  if (isLoading || peers.length === 0) return null;

  return (
    <section className="people__peers">
      <h2 className="people__peers-title">Other servers</h2>
      {peers.map((peer) => (
        <PeerCard key={peer.fingerprint} peer={peer} />
      ))}
      <p className="people__foot">
        Presence is separate from sharing what you have watched, and it is off
        until you turn it on for a named person. It says only that you are
        watching something and what it is called — never how far in, and never
        which episode.
      </p>
    </section>
  );
}

function PeerCard({ peer }: { peer: PeerPresence }) {
  return (
    <div className="people__peer">
      <div className="people__peer-head">
        <span className="people__peer-name">{peer.name}</span>
        {/*
         * Reachability is about the machine and says nothing about anybody on
         * it. Kept visually quiet: an offline server is ordinary, and styling it
         * as an error trains people to ignore the row.
         */}
        <span
          className={
            peer.reachable
              ? "people__peer-state people__peer-state--up"
              : "people__peer-state"
          }
        >
          {peer.reachable ? "Online" : "Not answering"}
        </span>
      </div>

      {peer.people.length === 0 && (
        <p className="people__peer-empty">
          Nobody on this server has chosen to appear in its roster yet.
        </p>
      )}

      {peer.people.map((person) => (
        <PeerPersonRow
          key={person.id}
          person={person}
          fingerprint={peer.fingerprint}
          reachable={peer.reachable}
        />
      ))}
    </div>
  );
}

function PeerPersonRow({
  person,
  fingerprint,
  reachable,
}: {
  person: PeerPerson;
  fingerprint: string;
  reachable: boolean;
}) {
  const grant = useGrantPresence();

  return (
    <div className="people__peer-person">
      <div className="people__peer-who">
        <span className="people__peer-person-name">{person.name}</span>
        <span className="people__peer-status">
          {statusOf(person, reachable)}
        </span>
      </div>

      {/*
       * Watch Together is deliberately present and deliberately disabled. ADR
       * 0045 §7 gives a presence grant the right to *ask*, with the host
       * answering in the moment — but the asking needs the remote guest
       * sessions of ADR 0046, which are phase 4 and not built. An affordance
       * that says why it cannot be used yet is more honest than a missing one,
       * and it is where the button goes when it can.
       */}
      {person.shares && person.watching && (
        <button
          type="button"
          className="button people__join"
          disabled
          title="Joining across servers needs remote guest sessions, which are not built yet (ADR 0046)."
        >
          Watch Together
        </button>
      )}

      <label className="people__grant">
        <input
          type="checkbox"
          checked={person.granted}
          disabled={grant.isPending}
          onChange={(e) =>
            grant.mutate({
              fingerprint,
              person: person.id,
              on: e.currentTarget.checked,
            })
          }
        />
        <span>Let them see me</span>
      </label>
    </div>
  );
}

/*
 * The sentence for one person, and a function rather than a ternary in the
 * markup because there are four cases and three of them are easy to collapse by
 * accident.
 *
 * "Not sharing" is a *choice* and must never be rendered as an absence or as
 * being offline — the rule the local list already holds itself to. And somebody
 * can only be known to be offline if they share; otherwise there is nothing to
 * know, and "Offline" would be inventing a fact about a person who has told us
 * nothing.
 */
function statusOf(person: PeerPerson, reachable: boolean): string {
  if (!person.shares) return "Not sharing with you";
  if (!reachable) return "Server not answering";
  if (!person.online) return "Offline";
  if (person.watching) return `Watching ${person.watching}`;
  return "Online";
}
