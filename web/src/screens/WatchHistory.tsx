import { useRef } from "react";
import { Link, Navigate } from "react-router-dom";
import { useViewings, type HistoryKind } from "@/api/hooks";
import { useInfiniteScroll } from "@/lib/useInfiniteScroll";
import { episodeCode } from "@/lib/format";
import type { Viewing } from "@/api/types";
import "./Browse.css";
import "./Profile.css";

/*
 * The watch history's old address. It became the Profile History tab's "Every
 * time" (profile-2-plan §4e), and links to it (bookmarks, Year in Review in an
 * older client) still land in the right place.
 */
export function WatchHistory() {
  return <Navigate to="/profile?tab=history&every=1" replace />;
}

/*
 * Every finished viewing, newest first, grouped by month (ADR 0074).
 *
 * Not "Each title once", which reads the state table and so shows each title
 * once, dated from its last play. Here a film watched three times is three
 * rows: the question this answers is "when did I watch it", and only a log can
 * say.
 *
 * The export links are plain downloads of the caller's own history, all of it
 * whatever the filter. LANcast sends the file nowhere; importing it into
 * another service is the person's decision, made with the file in hand.
 */
export function ViewingLog({ kind = "" }: { kind?: HistoryKind }) {
  const { data, isLoading, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useViewings(100, kind);
  const viewings = (data?.pages ?? []).flatMap((p) => p.viewings);
  const total = data?.pages[0]?.total ?? 0;

  const sentinel = useRef<HTMLDivElement | null>(null);
  useInfiniteScroll(sentinel, { hasNextPage, isFetchingNextPage, fetchNextPage });

  // Songs are never logged (ADR 0119): an album played through is a weak
  // signal. Saying so beats an empty list that looks like a fault.
  if (kind === "music") {
    return (
      <p className="browse__message">
        Songs are not kept in the log, only the last time each was played. Switch
        to Each title once to see them.
      </p>
    );
  }

  return (
    <>
      <div className="profile__exports">
        <span className="profile__hint">
          Every film, episode, audiobook and comic you finished, each time you
          finished it. Only you can see it.
          {total > 0 && ` ${total.toLocaleString()} in all.`}
        </span>
        {total > 0 && (
          <span className="profile__exportlinks">
            <a className="profile__page" href="/api/profile/viewings/export?format=csv" download>
              Download CSV
            </a>
            <a
              className="profile__page"
              href="/api/profile/viewings/export?format=trakt"
              download
              title="Shaped for Trakt's history import"
            >
              Download for Trakt
            </a>
          </span>
        )}
      </div>

      {isError && <p className="browse__message">Your watch history could not be loaded.</p>}
      {!isLoading && !isError && viewings.length === 0 && (
        <p className="browse__message">
          Nothing here yet. Finish something and it is added, with the date.
        </p>
      )}

      {byMonth(viewings).map(([month, rows]) => (
        <section key={month} className="profile__section" aria-label={month}>
          <span className="section-label profile__label">{month}</span>
          <div className="profile__history">
            {rows.map((v) => (
              <ViewingRow key={v.id} viewing={v} />
            ))}
          </div>
        </section>
      ))}
      <div ref={sentinel} aria-hidden="true" />
    </>
  );
}

// Grouped in the viewer's own calendar: a film finished at eleven at night on
// the 31st belongs to the month they were in when they watched it.
function byMonth(vs: Viewing[]): [string, Viewing[]][] {
  const out: [string, Viewing[]][] = [];
  for (const v of vs) {
    const label = new Date(v.finished_at * 1000).toLocaleDateString(undefined, {
      month: "long",
      year: "numeric",
    });
    const last = out[out.length - 1];
    if (last && last[0] === label) last[1].push(v);
    else out.push([label, [v]]);
  }
  return out;
}

function ViewingRow({ viewing: v }: { viewing: Viewing }) {
  const code = episodeCode(v);
  /*
   * A book or a comic says what it is (ADR 0119). Without it a finished
   * audiobook reads as a film of the same name, which is the one thing a
   * history of mostly films should not let it look like.
   */
  const kindWord = v.kind === "book" ? "Audiobook" : v.kind === "comic" ? "Comic" : "";
  const detail =
    v.kind === "episode"
      ? [v.series, code].filter(Boolean).join(" · ")
      : [kindWord, v.year ? String(v.year) : ""].filter(Boolean).join(" · ");
  const when = new Date(v.finished_at * 1000).toLocaleDateString();
  const body = (
    <>
      <div className="profile__what">
        <span className="profile__titleline">
          {v.title}
          {/* Deleted from the library since: the row still says what it was. */}
          {v.item_id == null && <span className="profile__missing">removed</span>}
        </span>
        {detail && <span className="profile__detail">{detail}</span>}
      </div>
      <div className="profile__when">
        <span>{when}</span>
        {/* Seeded when the history began, from the last time the title's
            state was written: the best date there was, and it says so. */}
        {v.estimated && (
          <span className="profile__state" title="Recorded before the watch history began; this is the last date LANcast had for it">
            approximate
          </span>
        )}
      </div>
    </>
  );
  return v.item_id != null ? (
    <Link className="profile__row profile__row--log" to={`/item/${v.item_id}`}>
      {body}
    </Link>
  ) : (
    <div className="profile__row profile__row--log">{body}</div>
  );
}
