import { useState } from "react";
import { Link } from "react-router-dom";
import { usePeople, usePersonActivity } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import type { Person } from "@/api/types";
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
