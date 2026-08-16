import { useState } from "react";
import { Link } from "react-router-dom";
import { useProfile } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { runtime, episodeCode } from "@/lib/format";
import type { HistoryEntry } from "@/api/types";
import "./Profile.css";

/*
 * The profile page.
 *
 * The rail has carried the signed-in name since the shell was built, with a
 * comment calling it "a destination in waiting". This is that destination, and
 * it is deliberately the small half of the backlog item: history, honest
 * totals, and who you are.
 *
 * What it does not have is as considered as what it does. No Find Friends, no
 * Trending, no reviews — those need data the server does not collect and, in
 * two of the three cases, a decision about who may see whose viewing that
 * nobody has made. A page of empty scaffolding promising four features is worse
 * than a page of three true numbers, because the scaffolding is what people
 * plan around.
 */

const PAGE = 50;

export function Profile() {
  const [offset, setOffset] = useState(0);
  const { data, isLoading, isError } = useProfile(PAGE, offset);

  if (isError) {
    return (
      <div className="browse">
        <p className="browse__message">Your profile could not be loaded.</p>
      </div>
    );
  }

  const stats = data?.stats;
  const history = data?.history ?? [];

  return (
    <div className="browse profile">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">{data?.user.name ?? "Profile"}</h1>
        {/* An unconfigured loopback server has no account, and the history
            belongs to the migrated 'local' id. Saying so beats inventing a
            person called Local and letting somebody wonder who they are. */}
        {data && !data.user.secured && (
          <span className="profile__badge">no account — this server is open on loopback</span>
        )}
        {data?.user.admin && <span className="profile__badge">admin</span>}
      </div>

      <div className="profile__stats">
        <Stat label="Started" value={stats ? String(stats.started) : "—"} />
        <Stat label="Finished" value={stats ? String(stats.finished) : "—"} />
        <Stat
          label="Time watched"
          value={stats ? (runtime(stats.watched_ms) || "0m") : "—"}
          /* The qualifier is the honest part. This is time actually spent, not
             the runtime of everything opened — and it counts each item once,
             because the server keeps one row per item and not a log of every
             sitting. */
          note="counted once per title"
        />
        <Stat
          label="Since"
          value={
            stats?.first_at
              ? new Date(stats.first_at * 1000).toLocaleDateString()
              : "—"
          }
        />
      </div>

      <span className="section-label profile__label">Recently played</span>

      {isLoading && <p className="browse__message">Loading…</p>}

      {!isLoading && history.length === 0 && (
        <p className="browse__message">
          Nothing played yet. Anything you watch or listen to shows up here.
        </p>
      )}

      <div className="profile__history">
        {history.map((e) => (
          <HistoryRow key={`${e.item.id}-${e.played_at}`} entry={e} />
        ))}
      </div>

      {(offset > 0 || data?.has_more) && (
        <div className="profile__pager">
          <button
            className="profile__page"
            disabled={offset === 0}
            onClick={() => setOffset(Math.max(0, offset - PAGE))}
          >
            ← Newer
          </button>
          <button
            className="profile__page"
            disabled={!data?.has_more}
            onClick={() => setOffset(offset + PAGE)}
          >
            Older →
          </button>
        </div>
      )}
    </div>
  );
}

function Stat({
  label,
  value,
  note,
}: {
  label: string;
  value: string;
  note?: string;
}) {
  return (
    <div className="profile__stat">
      <span className="profile__statvalue">{value}</span>
      <span className="profile__statlabel">{label}</span>
      {note && <span className="profile__statnote">{note}</span>}
    </div>
  );
}

function HistoryRow({ entry }: { entry: HistoryEntry }) {
  const { item } = entry;
  const poster = artworkURL(item.artwork?.poster, "thumb");
  const pct =
    item.duration_ms && item.duration_ms > 0
      ? Math.min(100, (entry.position_ms / item.duration_ms) * 100)
      : 0;

  // An episode says which one; a track says its album. Both read from the same
  // three columns (ADR 0024) and the difference is the kind — which this line
  // used to ignore, so a song showed the disc and track it was written into:
  // Pearl Jam's *Black* as S00E33. episodeCode makes that check once.
  const detail = [item.series, episodeCode(item)].filter(Boolean).join(" · ");

  return (
    <Link className="profile__row" to={`/item/${item.id}`}>
      <div className="profile__art">
        {poster ? (
          <img src={poster} alt="" loading="lazy" />
        ) : (
          <span aria-hidden="true">{item.title.slice(0, 1).toUpperCase()}</span>
        )}
      </div>

      <div className="profile__what">
        <span className="profile__titleline">
          {item.title}
          {/* An item whose file is gone stays in the history, because "what
              happened to the film I watched last week" is a question about
              history — but it says so, rather than looking playable. */}
          {item.missing && <span className="profile__missing">missing</span>}
        </span>
        {detail && <span className="profile__detail">{detail}</span>}
        {pct > 0 && !entry.watched && (
          <span className="profile__bar" aria-hidden="true">
            <span style={{ width: `${pct}%` }} />
          </span>
        )}
      </div>

      <div className="profile__when">
        <span>{new Date(entry.played_at * 1000).toLocaleDateString()}</span>
        <span className="profile__state">
          {entry.watched ? "Finished" : runtime(entry.position_ms) || "Started"}
        </span>
      </div>
    </Link>
  );
}
