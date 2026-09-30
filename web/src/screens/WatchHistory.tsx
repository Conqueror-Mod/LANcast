import { useRef } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useViewings } from "@/api/hooks";
import { useInfiniteScroll } from "@/lib/useInfiniteScroll";
import { useBackHandler } from "@/focus/FocusController";
import { episodeCode } from "@/lib/format";
import type { Viewing } from "@/api/types";
import "./Browse.css";
import "./Profile.css";

/*
 * Every finished viewing, newest first, grouped by month (ADR 0074).
 *
 * Not the profile's Recently played, which reads the state table and so shows
 * each title once, dated from its last play. Here a film watched three times
 * is three rows — the question this page answers is "when did I watch it",
 * and only a log can say.
 *
 * The export links are plain downloads of the caller's own history. LANcast
 * sends the file nowhere; importing it into another service is the person's
 * decision, made with the file in hand.
 */
export function WatchHistory() {
  const navigate = useNavigate();
  const { data, isLoading, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useViewings();
  const viewings = (data?.pages ?? []).flatMap((p) => p.viewings);
  const total = data?.pages[0]?.total ?? 0;

  const sentinel = useRef<HTMLDivElement | null>(null);
  useInfiniteScroll(sentinel, { hasNextPage, isFetchingNextPage, fetchNextPage });

  const back = () => navigate("/profile");
  useBackHandler(back);

  return (
    <div className="browse profile">
      <div className="browse__head browse__head--sticky">
        <button className="pl-back" onClick={back}>
          ← Profile
        </button>
        <h1 className="browse__title">Watch history</h1>
        <span className="browse__count">{total ? total.toLocaleString() : ""}</span>
      </div>

      <div className="profile__exports">
        <span className="profile__hint">
          Every film and episode you finished, each time you finished it. Only
          you can see it.
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
          Nothing here yet. Finish a film or an episode and it is added, with the
          date.
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
    </div>
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
  const detail =
    v.kind === "episode"
      ? [v.series, code].filter(Boolean).join(" · ")
      : v.year
        ? String(v.year)
        : "";
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
