import { useState } from "react";
import { Link } from "react-router-dom";
import {
  useCurrentUser,
  useMarkedItems,
  useMyRatings,
  useProfile,
  useSetAvatar,
  useTags,
} from "@/api/hooks";
import { Shelf } from "@/components/Shelf";
import { AVATARS, AvatarGlyph } from "@/components/Avatar";
import { YearInReview } from "@/components/YearInReview";
import { artworkURL } from "@/api/client";
import { runtime, episodeCode } from "@/lib/format";
import type { HistoryEntry, RatedItem } from "@/api/types";
import "./Profile.css";

/*
 * The profile page.
 *
 * The rail has carried the signed-in name since the shell was built, with a
 * comment calling it "a destination in waiting". This is that destination, and
 * it is deliberately the small half of the backlog item: history, honest
 * totals, and who you are.
 *
 * What it does not have is as considered as what it does. Other people's
 * viewing lives on the People page, behind the opt-in ADR 0035 settled, and
 * nothing here is visible to anybody else: your favourites, your ratings and
 * your tags are private to this account (ADR 0062), so the page is a mirror
 * rather than a profile anyone else visits.
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

      {data?.user.secured && <AvatarPicker />}

      <div className="profile__stats">
        <Stat label="Started" value={stats ? String(stats.started) : "—"} />
        <Stat label="Finished" value={stats ? String(stats.finished) : "—"} />
        {/* Shown only when it says something the card beside it does not.
            Equal numbers mean nothing has been rewatched, and a second card
            repeating the first is noise on most profiles.

            Absent rather than zero on a server too old to report it: a client
            newer than its server is ordinary, and "0 viewings" beside "412
            finished" is a wrong statement rather than a missing one. */}
        {stats?.viewings != null && stats.viewings > stats.finished && (
          <Stat
            label="Viewings"
            value={String(stats.viewings)}
            note="including rewatches"
          />
        )}
        <Stat
          label="Time watched"
          value={stats ? (runtime(stats.watched_ms) || "0m") : "—"}
          /* The qualifier is the honest part, and it changed when the server
             learned to count viewings. It used to say "counted once per title",
             which was true of a boolean and stopped being true the moment a
             tally sat beside it — a note that describes an older version of the
             thing it labels is worse than none.

             A title with no known runtime is still counted once, because there
             is no measurement of how long one viewing of it was. */
          note="every viewing counted"
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

      {/*
        Above Recently played, below the lifetime totals.

        It is the same history read a different way, so it belongs with the
        numbers it is derived from rather than on a route of its own — and a
        nav entry for something seasonal would be a permanent fixture for an
        occasional read.
      */}
      <YearInReview />

      {data?.user.secured && (
        <>
          <FavouritesShelf />
          <YourRatings />
          <YourTags />
        </>
      )}

      <div className="profile__labelrow">
        <span className="section-label profile__label">Recently played</span>
        {/* The log beside this list: this one shows each title once, dated
            from its last play; the history has every time it was finished. */}
        {data?.user.secured && (
          <Link className="profile__loglink" to="/profile/viewings">
            Full watch history →
          </Link>
        )}
      </div>

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

/*
 * Which picture sits beside your name in the rail.
 *
 * Six drawn animals and nothing else — the server stores a key, not an image
 * (PUT /api/profile/avatar). Pressing the chosen one again clears it, back to
 * the plain person glyph, so there is no separate "none" to find.
 *
 * Gold marks the chosen one, and that is the design rule working rather than
 * bending: it is *where you are* in this set, the same thing a selected rail
 * row says.
 */
function AvatarPicker() {
  const user = useCurrentUser();
  const setAvatar = useSetAvatar();
  const current = setAvatar.isPending ? setAvatar.variables : (user?.avatar ?? "");
  return (
    <section className="profile__avatars" aria-label="Your picture">
      <span className="section-label">Your picture</span>
      <div className="profile__avatarrow" role="group" aria-label="Choose a picture">
        {AVATARS.map((a) => {
          const on = current === a.key;
          return (
            <button
              key={a.key}
              type="button"
              className={"profile__avatar" + (on ? " is-on" : "")}
              aria-pressed={on}
              aria-label={a.label}
              title={on ? `${a.label} — press again to remove` : a.label}
              disabled={setAvatar.isPending}
              onClick={() => setAvatar.mutate(on ? "" : a.key)}
            >
              <AvatarGlyph avatar={a.key} size={34} />
            </button>
          );
        })}
      </div>
      {setAvatar.isError && (
        <p className="profile__avatarerr">That picture could not be saved.</p>
      )}
    </section>
  );
}

/*
 * Everything favourited, from every library and at any level.
 *
 * The first shelf's worth, and the whole list a click away. A shelf with
 * nothing on it is a line saying how to start one, because an absent section
 * is also how a broken one looks.
 */
const SHELF = 20;

function FavouritesShelf() {
  const { data, isLoading } = useMarkedItems({ favourite: true }, SHELF);
  const items = data?.pages[0]?.items ?? [];
  if (isLoading) return null;
  if (items.length === 0) {
    return (
      <section className="profile__section" aria-label="Favourites">
        <span className="section-label profile__label">Favourites</span>
        <p className="profile__hint">
          Nothing favourited yet. Press the heart on any title's page and it
          shows up here.
        </p>
      </section>
    );
  }
  return <Shelf title="Favourites" items={items} seeAllTo="/profile/favourites" />;
}

/*
 * Your ratings, newest first.
 *
 * Yours alone: the route carries no user id, so there is no way for this list
 * to show anybody else's (ADR 0035). Out of ten, printed as such — the page
 * shows the number you chose rather than converting it into stars you did not.
 */
const RATINGS_FIRST = 10;
const RATINGS_MAX = 200;

function YourRatings() {
  const [limit, setLimit] = useState(RATINGS_FIRST);
  const { data, isLoading } = useMyRatings(limit);
  const ratings = data?.ratings ?? [];
  if (isLoading && ratings.length === 0) return null;
  return (
    <section className="profile__section" aria-label="Your ratings">
      <span className="section-label profile__label">Your ratings</span>
      {ratings.length === 0 ? (
        <p className="profile__hint">
          Nothing rated yet. A score you give a title on its page is kept here,
          and only you can see it.
        </p>
      ) : (
        <div className="profile__history">
          {ratings.map((r) => (
            <RatingRow key={r.item.id} rated={r} />
          ))}
        </div>
      )}
      {/* A full page means there may be more. The server caps a page at 200,
          which is the most this offers. */}
      {ratings.length === limit && limit < RATINGS_MAX && (
        <div className="profile__pager">
          <button className="profile__page" onClick={() => setLimit(RATINGS_MAX)}>
            Show all ratings
          </button>
        </div>
      )}
    </section>
  );
}

function RatingRow({ rated }: { rated: RatedItem }) {
  const { item, rating } = rated;
  const poster = artworkURL(item.artwork?.poster, "thumb");
  const detail = [item.series, episodeCode(item), item.year]
    .filter(Boolean)
    .join(" · ");
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
          {item.missing && <span className="profile__missing">missing</span>}
        </span>
        {detail && <span className="profile__detail">{detail}</span>}
        {rating.review && <span className="profile__review">{rating.review}</span>}
      </div>
      <div className="profile__when">
        <span className="profile__score" aria-label={`${rating.score} out of 10`}>
          {rating.score}
          <span className="profile__scoreof">/10</span>
        </span>
        <span>{new Date(rating.updated_at * 1000).toLocaleDateString()}</span>
      </div>
    </Link>
  );
}

/*
 * Your tags, each opening everything that carries it, across libraries.
 *
 * Inside a library a tag is one of the grid's filters. This is the question
 * that does not stop at a library's edge: everything you called "Christmas".
 */
function YourTags() {
  const { data, isLoading } = useTags();
  const tags = data?.tags ?? [];
  if (isLoading) return null;
  return (
    <section className="profile__section" aria-label="Your tags">
      <span className="section-label profile__label">Your tags</span>
      {tags.length === 0 ? (
        <p className="profile__hint">
          No tags yet. Add one from any title's page to group things your own
          way. Nobody else on this server sees them.
        </p>
      ) : (
        <div className="profile__tags">
          {tags.map((t) => (
            <Link key={t.id} className="profile__tag" to={`/profile/tags/${t.id}`}>
              {t.name}
              <span className="profile__tagcount">{t.count}</span>
            </Link>
          ))}
        </div>
      )}
    </section>
  );
}
