import { RetroAchievementsCard, useRetroAchievements } from "@/components/RetroAchievementsCard";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import {
  useCurrentUser,
  useMarkedItems,
  useMyRatings,
  useProfile,
  useProfileKinds,
  useTags,
  type HistoryKind,
} from "@/api/hooks";
import { Shelf } from "@/components/Shelf";
import { PersonPicture } from "@/components/Avatar";
import { useFocusZone } from "@/focus/FocusController";
import { PictureEditor } from "@/components/PictureEditor";
import { YearInReview } from "@/components/YearInReview";
import { artworkURL } from "@/api/client";
import { runtime, episodeCode } from "@/lib/format";
import type { HistoryEntry, KindEntry, ProfileStats, RatedItem, SavedGame } from "@/api/types";
import type { components } from "@/api/schema";
import { kindCards } from "@/lib/profileKinds";
import { ViewingLog } from "./WatchHistory";

type PlayedItem = components["schemas"]["PlayedItem"];
import "@/components/FilterBar.css";
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

/*
 * Four tabs (profile-2-plan §4b), named in the address so a tab can be linked
 * and Back returns to the one you left. With a remote the row is the first
 * stop under the header, so every section is two presses away instead of the
 * thirty-two it took to reach Recently played on one long page.
 */
const TABS = [
  { id: "overview", label: "Overview" },
  { id: "history", label: "History" },
  { id: "ratings", label: "Ratings & tags" },
  { id: "achievements", label: "Achievements" },
] as const;
type Tab = (typeof TABS)[number]["id"];

// History's filter. Games are not here: a retro game leaves no play record,
// only its saves, which the Overview shows (profile-2-plan §4d).
const HISTORY_KINDS: { id: HistoryKind; label: string }[] = [
  { id: "", label: "All" },
  { id: "video", label: "Video" },
  { id: "music", label: "Music" },
  { id: "books", label: "Books" },
  { id: "comics", label: "Comics" },
];

export function Profile() {
  const [offset, setOffset] = useState(0);
  const [params, setParams] = useSearchParams();
  // History's filter and switch, from the address (profile-2-plan §4e).
  const askedKind = params.get("kind") ?? "";
  const kind: HistoryKind = HISTORY_KINDS.some((k) => k.id === askedKind)
    ? (askedKind as HistoryKind)
    : "";
  const every = params.get("every") === "1";
  const { data, isLoading, isError } = useProfile(PAGE, offset, kind);
  const ra = useRetroAchievements();
  const secured = !!data?.user.secured;
  /*
   * Every control on the page, reachable with a remote. Before the tabs only
   * the shelves registered themselves, so Edit, the tab row, the rating rows
   * and the pager could not be reached by arrow at all.
   */
  const zone = useFocusZone();

  // Tabs that have something behind them: the private lists need an account,
  // and Achievements needs RetroAchievements on this server.
  const tabs = TABS.filter((t) => {
    if (t.id === "ratings") return secured;
    if (t.id === "achievements") return secured && !!ra.data?.available;
    return true;
  });
  const asked = params.get("tab");
  const tab: Tab = tabs.some((t) => t.id === asked) ? (asked as Tab) : "overview";

  const setHistory = (change: { kind?: HistoryKind; every?: boolean }) => {
    const next = new URLSearchParams(params);
    if (change.kind !== undefined) {
      if (change.kind) next.set("kind", change.kind);
      else next.delete("kind");
    }
    if (change.every !== undefined) {
      if (change.every) next.set("every", "1");
      else next.delete("every");
    }
    // A different list starts at its first page.
    setOffset(0);
    // Replace rather than push: Back leaves the page, it does not step
    // through every chip pressed on the way.
    setParams(next, { replace: true });
  };

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
    <div className="browse profile" ref={zone}>
      <ProfileHero
        name={data?.user.name ?? "Profile"}
        secured={secured}
        admin={!!data?.user.admin}
        loaded={!!data}
        since={stats?.first_at ?? null}
      />

      <div className="profile__tabs" role="tablist" aria-label="Your profile">
        {tabs.map((t) => {
          const on = t.id === tab;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`profile-tab-${t.id}`}
              className={"profile__tab" + (on ? " is-on" : "")}
              aria-selected={on}
              aria-controls="profile-panel"
              onClick={() => {
                if (on) return;
                const next = new URLSearchParams(params);
                if (t.id === "overview") next.delete("tab");
                else next.set("tab", t.id);
                setParams(next);
              }}
            >
              {t.label}
            </button>
          );
        })}
      </div>

      <div
        className="profile__panel"
        id="profile-panel"
        role="tabpanel"
        aria-labelledby={`profile-tab-${tab}`}
      >
        {tab === "overview" && (
          <>
            {stats?.by_kind ? (
              <KindStats stats={stats} />
            ) : (
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
            </div>
            )}

            {/* What you are in the middle of comes before what you went back
                to most: it is the part of the page you would act on. */}
            {secured && <NewerKinds />}

            <MostPlayed items={data?.most_played ?? []} />

            {/* The same history read a different way, so it belongs with the
                numbers it is derived from rather than on a route of its own. */}
            <YearInReview />

            {secured && <FavouritesShelf />}
          </>
        )}

        {tab === "history" && (
          <>
            {/* One History (profile-2-plan §4e): a filter by kind, and a
                switch between each title once (the state table, dated from
                its last play) and every time (the log, one row per finish).
                Both live in the address, so Back and a bookmark keep them. */}
            <div className="profile__historybar">
              <div className="profile__chips" role="group" aria-label="Show">
                {HISTORY_KINDS.map((k) => (
                  <button
                    key={k.id || "all"}
                    type="button"
                    className={"chip" + (k.id === kind ? " is-on" : "")}
                    aria-pressed={k.id === kind}
                    onClick={() => setHistory({ kind: k.id })}
                  >
                    {k.label}
                  </button>
                ))}
              </div>
              {secured && (
                <div className="profile__chips" role="group" aria-label="Count">
                  <button
                    type="button"
                    className={"chip" + (!every ? " is-on" : "")}
                    aria-pressed={!every}
                    onClick={() => setHistory({ every: false })}
                  >
                    Each title once
                  </button>
                  <button
                    type="button"
                    className={"chip" + (every ? " is-on" : "")}
                    aria-pressed={every}
                    onClick={() => setHistory({ every: true })}
                  >
                    Every time
                  </button>
                </div>
              )}
            </div>
          </>
        )}

        {tab === "history" && every && secured && <ViewingLog kind={kind} />}

        {tab === "history" && !(every && secured) && (
          <>
            {isLoading && <p className="browse__message">Loading…</p>}

            {!isLoading && history.length === 0 && (
              <p className="browse__message">
                {kind
                  ? "Nothing of this kind played yet."
                  : "Nothing played yet. Anything you watch or listen to shows up here."}
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
          </>
        )}

        {tab === "ratings" && (
          <>
            <YourRatings />
            <YourTags />
          </>
        )}

        {/* What you have earned, to look at. Connecting and disconnecting are
            in Settings › Connected accounts (profile-2-plan §6), which the
            card links to. */}
        {tab === "achievements" && <RetroAchievementsCard />}
      </div>
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

/*
 * The totals split by kind (profile-2-plan §4c, step 4). "Time watched" used to
 * count songs, so an album on repeat read as hours of film; each group now has
 * its own card, and what a history reset banked keeps one of its own.
 */
function KindStats({ stats }: { stats: ProfileStats }) {
  const cards = kindCards(stats.by_kind ?? [], stats.earlier);
  if (cards.length === 0) {
    return (
      <p className="browse__message profile__empty">
        Nothing played yet. Your totals start with the first thing you play.
      </p>
    );
  }
  return (
    <div className="profile__stats">
      {cards.map((c) => (
        <Stat
          key={c.id}
          label={c.label}
          value={c.ms == null ? c.finished.toLocaleString() : runtime(c.ms) || "0m"}
          note={c.note}
        />
      ))}
    </div>
  );
}

const PLAYED_GROUPS: { label: string; kinds: string[] }[] = [
  { label: "Films & shows", kinds: ["movie", "episode"] },
  { label: "Music", kinds: ["track"] },
  { label: "Audiobooks", kinds: ["book_part"] },
];

/*
 * What you came back to most: finished at least twice, five per group, so a
 * song on repeat cannot push every film off the list. Nothing is shown until
 * something has been played twice; a list of ties is History's job.
 */
function MostPlayed({ items }: { items: PlayedItem[] }) {
  if (items.length === 0) return null;
  return (
    <section className="profile__section profile__mostplayed" aria-label="Most played">
      <span className="section-label profile__label">Most played</span>
      {PLAYED_GROUPS.map((g) => {
        const rows = items.filter((p) => g.kinds.includes(p.item.kind));
        if (rows.length === 0) return null;
        return (
          <div key={g.label} className="profile__playedgroup">
            <h3 className="profile__grouplabel">{g.label}</h3>
            <div className="profile__history">
              {rows.map((p) => (
                <PlayedRow key={p.item.id} played={p} />
              ))}
            </div>
          </div>
        );
      })}
    </section>
  );
}

/*
 * Books, comics and games on Overview (profile-2-plan §4d): the kinds that
 * arrived after the page did and appeared only through Recently played. Each
 * section is there only when it has something in it, so an account that never
 * reads comics never sees an empty Comics heading.
 *
 * A game shows its last save and nothing about time: LANcast records no play
 * time (§8), and a section that invented some would be worse than none.
 */
function NewerKinds() {
  const { data } = useProfileKinds();
  // An older server has no such route; a missing list is an empty one.
  if (!data) return null;
  const none = { in_progress: [], finished: [] };
  const books = data.books ?? none;
  const comics = data.comics ?? none;
  const games = data.games ?? [];
  const listened = (e: KindEntry) => {
    const at = runtime(e.item.progress?.position_ms ?? 0);
    return at ? `${at} in` : "In progress";
  };
  const reading = (e: KindEntry) =>
    e.page ? `Page ${e.page.page + 1} of ${e.page.pages}` : "In progress";
  const sections = [
    {
      label: "Audiobooks",
      rows: [
        ...books.in_progress.map((e) => ({ e, note: listened(e) })),
        ...books.finished.map((e) => ({ e, note: "Finished" })),
      ],
    },
    {
      label: "Comics",
      rows: [
        ...comics.in_progress.map((e) => ({ e, note: reading(e) })),
        ...comics.finished.map((e) => ({ e, note: "Read" })),
      ],
    },
  ].filter((s) => s.rows.length > 0);
  if (sections.length === 0 && games.length === 0) return null;
  return (
    <>
      {sections.map((s) => (
        <section key={s.label} className="profile__section profile__kind" aria-label={s.label}>
          <span className="section-label profile__label">{s.label}</span>
          <div className="profile__history">
            {s.rows.map(({ e, note }) => (
              <KindRow key={e.item.id} item={e.item} note={note} />
            ))}
          </div>
        </section>
      ))}
      {games.length > 0 && (
        <section className="profile__section profile__kind" aria-label="Games">
          <span className="section-label profile__label">Games</span>
          <div className="profile__history">
            {games.map((g: SavedGame) => (
              <KindRow
                key={g.item.id}
                item={g.item}
                note={`Saved ${new Date(g.saved_at * 1000).toLocaleDateString()}`}
              />
            ))}
          </div>
        </section>
      )}
    </>
  );
}

function KindRow({ item, note }: { item: KindEntry["item"]; note: string }) {
  const poster = artworkURL(item.artwork?.poster, "thumb");
  const detail = [item.series ?? item.artist, item.year ? String(item.year) : ""]
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
        <span className="profile__titleline">{item.title}</span>
        {detail && <span className="profile__detail">{detail}</span>}
      </div>
      <div className="profile__when">
        <span className="profile__state">{note}</span>
      </div>
    </Link>
  );
}

function PlayedRow({ played }: { played: PlayedItem }) {
  const { item } = played;
  const poster = artworkURL(item.artwork?.poster, "thumb");
  const detail = [item.series ?? item.artist, episodeCode(item)].filter(Boolean).join(" · ");
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
        <span className="profile__titleline">{item.title}</span>
        {detail && <span className="profile__detail">{detail}</span>}
      </div>
      <div className="profile__when">
        <span className="profile__state">{played.plays.toLocaleString()} plays</span>
      </div>
    </Link>
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
 * Who you are, at the top of the page (profile-2-plan §4a).
 *
 * Your picture, your name, since when, and your role. Edit beside the picture
 * opens the picture editor, which also holds the six drawn animals. The name is
 * changed in Settings › Profile, where the account's facts are kept (ADR 0120);
 * the page only shows it, with a link there.
 */
function ProfileHero({
  name,
  secured,
  admin,
  loaded,
  since,
}: {
  name: string;
  secured: boolean;
  admin: boolean;
  loaded: boolean;
  since: number | null;
}) {
  const user = useCurrentUser();
  const [editing, setEditing] = useState(false);
  return (
    <header className="profile__hero">
      <div className="profile__heropic">
        <PersonPicture
          name={name}
          picture={secured ? user?.picture : null}
          avatar={secured ? user?.avatar : null}
          size={96}
          className="profile__mepic"
        />
        {secured && (
          <button
            type="button"
            className="profile__edit"
            aria-label="Edit your picture"
            onClick={() => setEditing(true)}
          >
            Edit
          </button>
        )}
      </div>
      <div className="profile__whoami">
        <h1 className="browse__title profile__name">{name}</h1>
        <div className="profile__meta">
          {admin && <span className="profile__badge">admin</span>}
          {/* An unconfigured loopback server has no account, and the history
              belongs to the migrated 'local' id. Saying so beats inventing a
              person called Local and letting somebody wonder who they are. */}
          {loaded && !secured && (
            <span className="profile__badge">no account — this server is open on loopback</span>
          )}
          {since != null && (
            <span className="profile__since">
              Since {new Date(since * 1000).toLocaleDateString()}
            </span>
          )}
          {secured && (
            <Link className="profile__namelink" to="/settings?pane=profile">
              Change name
            </Link>
          )}
        </div>
      </div>
      {editing && <PictureEditor onClose={() => setEditing(false)} />}
    </header>
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
