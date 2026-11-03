import { useState } from "react";
import { useCandidates, useApplyMatch, useUnlockField } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import type { Item, MatchCandidate, ScoreBreakdown } from "@/api/types";
import { scorePct } from "@/lib/format";
import "./FixMatch.css";

// A labelled meter for one score component, so a candidate's total is explained
// rather than asserted. The weak component (e.g. a wrong year) reads at a glance.
function Meter({ label, v, note }: { label: string; v: number; note?: string }) {
  // The bar is drawn from the exact value and the label is floored: a meter may
  // fill to its true width, but the number beside it must not round upward into
  // a confidence the scorer did not have.
  const clamped = Math.max(0, Math.min(1, v));
  const pct = scorePct(clamped);
  return (
    <div className="fixmatch__meter" title={`${label}: ${pct}`}>
      <span className="fixmatch__meter-label">{label}</span>
      <span className="fixmatch__meter-track">
        <span
          className="fixmatch__meter-fill"
          style={{ width: `${clamped * 100}%` }}
        />
      </span>
      <span className="fixmatch__meter-val">{note ?? pct}</span>
    </div>
  );
}

function ScoreBar({
  breakdown,
  total,
  showYear,
  titleOnly = false,
  album = false,
}: {
  breakdown: ScoreBreakdown;
  total: number;
  showYear: boolean;
  // A game is scored on its title alone (the ROM database has no popularity,
  // and a DAT's year is not what somebody searches by). Meters for the
  // others would sit at zero and read as a poor match.
  titleOnly?: boolean;
  // An album is scored on its title, its artist and its year (ADR 0115).
  // MusicBrainz has no popularity, so that meter would sit at zero too.
  album?: boolean;
}) {
  return (
    <div className="fixmatch__break">
      <span className="fixmatch__total">{scorePct(total)} match</span>
      <Meter label="Title" v={breakdown.title} />
      {album && <Meter label="Artist" v={breakdown.artist ?? 0} />}
      {showYear && !titleOnly && (
        <Meter
          label="Year"
          v={breakdown.year}
          note={breakdown.year_gap ? `${breakdown.year_gap}y off` : undefined}
        />
      )}
      {!titleOnly && !album && <Meter label="Popularity" v={breakdown.popularity} />}
    </div>
  );
}

// What a candidate is, for the label beside its title. An album says what
// kind of record it is, since "Album" against "Single" is often the choice.
function kindLabel(c: MatchCandidate): string {
  if (c.Kind === "rom") return "Game";
  if (c.Kind === "album") return c.ReleaseType || "Album";
  if (c.Kind === "artist") return "Artist";
  return c.Kind === "show" || c.Kind === "episode" || c.Kind === "season" ? "TV" : "Movie";
}

// The page a MusicBrainz candidate is, so the record can be checked there
// before it is chosen.
function musicBrainzURL(c: MatchCandidate): string | null {
  if (c.Provider !== "musicbrainz") return null;
  const kind = c.Kind === "artist" ? "artist" : "release-group";
  return `https://musicbrainz.org/${kind}/${encodeURIComponent(c.ExternalID)}`;
}

// Correcting an item's identity: search a provider, pick the right title, and
// confirm it. Confirming locks the identity so a rescan never re-litigates it.
export function FixMatch({ item, onClose }: { item: Item; onClose: () => void }) {
  const [text, setText] = useState(item.title);
  // Auto-search on open with an empty query so the server scores against the
  // item's full identity — title *and* year. Passing the title as ?q= drops the
  // year server-side, which would blank out the year sub-score and hide the very
  // thing the breakdown exists to show. A user-typed search still sends ?q=.
  const [query, setQuery] = useState<string | null>("");
  // An album is searched by its artist too, and a folder tagged with the
  // label as its artist needs that replaced (ADR 0115). Empty is the album's
  // own artist.
  const isAlbum = item.kind === "album";
  // An artist is never searched by name: its candidates are the artists its
  // albums credit, so it has no search box at all.
  const isArtist = item.kind === "artist";
  const [artistText, setArtistText] = useState("");
  const [artistQuery, setArtistQuery] = useState("");
  const candidates = useCandidates(item.id, query, artistQuery);
  const search = () => {
    setQuery(text.trim());
    setArtistQuery(artistText.trim());
  };
  const currentLink = item.provider === "musicbrainz" && item.external_id
    ? musicBrainzURL({ Provider: item.provider, ExternalID: item.external_id, Kind: item.kind } as MatchCandidate)
    : null;
  const apply = useApplyMatch(item.id);
  const unlock = useUnlockField(item.id);

  useBackHandler(onClose);

  /*
   * A confirmed match usually locks the row and the dialog closes. It does not
   * lock a row whose shape is still wrong (ADR 0041) -- a parentless film in a
   * shows library, which is what a lost `EP1` marker produces -- because
   * locking there stops a rescan *fixing* the row when the filename is
   * corrected on disk.
   *
   * The identity is applied either way, so the choice is honoured. What
   * changes is that the row stays reviewable, and somebody who just pressed
   * Confirm has to be told why rather than watching it reappear in the queue.
   * `match_state` is the signal: anything but `locked` after a successful
   * confirm means the door was deliberately left open.
   */
  const [unsettled, setUnsettled] = useState(false);
  const pick = (c: MatchCandidate) =>
    apply.mutate(c, {
      onSuccess: (updated) => {
        if (updated?.match_state === "locked") {
          onClose();
          return;
        }
        setUnsettled(true);
      },
    });

  const locked = item.locked_fields ?? [];

  return (
    <div className="fixmatch__overlay" onClick={onClose}>
      <div className="fixmatch" onClick={(e) => e.stopPropagation()} role="dialog">
        {unsettled && (
          /*
             Not an error, and not styled as one. The match was applied; this
             says what did not happen and what to do about it, because the file
             is corrected on disk rather than in here -- that is the decision
             ADR 0041 records, not a limitation of this dialog.
          */
          <p className="fixmatch__unsettled">
            Match applied, and this row is still open for review. It is a film
            sitting loose in a shows library, which is usually a file whose
            episode number was lost from its name — locking it now would stop a
            rescan picking up the fix. Correct the filename on disk and rescan,
            or move the file to a film library.
          </p>
        )}
        <div className="fixmatch__head">
          <span className="section-label">Fix match</span>
          <button className="fixmatch__x" onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>

        <p className="fixmatch__current">
          {currentLink ? (
            <a href={currentLink} target="_blank" rel="noreferrer noopener">
              Currently MusicBrainz #{item.external_id}
            </a>
          ) : item.provider && item.external_id ? (
            `Currently ${item.provider} #${item.external_id}`
          ) : (
            "Not matched"
          )}
          {item.match_state ? ` · ${item.match_state}` : ""}
        </p>

        {/* Which file this actually is. Correcting a match is guesswork without
            it when several rows share a generic title. */}
        {item.file_name && (
          <p className="fixmatch__file" title={item.file_name}>
            {item.file_name}
          </p>
        )}

        {locked.length > 0 && (
          <div className="fixmatch__locks">
            <span className="fixmatch__locks-label">Locked:</span>
            {locked.map((f) => (
              <button
                key={f}
                className="fixmatch__lock"
                title="Release this field so a refresh can update it"
                disabled={unlock.isPending}
                onClick={() => unlock.mutate(f)}
              >
                {f} ✕
              </button>
            ))}
          </div>
        )}

        {isArtist ? (
          <p className="fixmatch__msg fixmatch__why">
            An artist is identified by its albums, not by its name — several
            artists share most names. These are the artists its albums are
            credited to. If none is right, fix the albums.
          </p>
        ) : (
          <div className="fixmatch__search">
            <input
              className="fixmatch__input"
              value={text}
              placeholder={isAlbum ? "Album title, or a MusicBrainz link" : "Search by title"}
              aria-label={isAlbum ? "Album title" : "Title"}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") search();
              }}
              autoFocus
            />
            {isAlbum && (
              <input
                className="fixmatch__input fixmatch__input--artist"
                value={artistText}
                placeholder="Artist (blank: the album artist)"
                aria-label="Artist"
                onChange={(e) => setArtistText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") search();
                }}
              />
            )}
            <button className="fixmatch__go" onClick={search}>
              Search
            </button>
          </div>
        )}

        {candidates.isFetching && <p className="fixmatch__msg">Searching…</p>}
        {candidates.isError && (
          <p className="fixmatch__msg">
            {(candidates.error as Error).message ?? "No provider available."}
          </p>
        )}
        {apply.isError && (
          <p className="fixmatch__msg">
            {(apply.error as Error).message ?? "Could not apply the match."}
          </p>
        )}
        {candidates.data?.length === 0 && !candidates.isFetching && (
          <p className="fixmatch__msg">No matches found.</p>
        )}

        <div className="fixmatch__results">
          {candidates.data?.map((c) => {
            const current =
              c.Provider === item.provider && c.ExternalID === item.external_id;
            return (
              <button
                key={`${c.Provider}-${c.Kind}-${c.ExternalID}`}
                className={"fixmatch__cand" + (current ? " is-current" : "")}
                disabled={apply.isPending}
                onClick={() => pick(c)}
              >
                {c.PosterURL ? (
                  <img
                    className={"fixmatch__poster" + (c.Kind === "rom" ? " fixmatch__poster--box" : "")}
                    src={c.PosterURL}
                    alt=""
                    loading="lazy"
                  />
                ) : (
                  <div className="fixmatch__poster fixmatch__poster--empty" />
                )}
                <div className="fixmatch__cand-body">
                  <div className="fixmatch__cand-title">
                    {c.Title}
                    {c.Year ? <span className="fixmatch__year"> ({c.Year})</span> : null}
                    <span className="fixmatch__kind">{kindLabel(c)}</span>
                    {current && <span className="fixmatch__current-tag">current</span>}
                  </div>
                  {c.Artist && <div className="fixmatch__artist">{c.Artist}</div>}
                  {c.Overview && (
                    <div className="fixmatch__overview">{c.Overview}</div>
                  )}
                  {c.Kind === "artist" ? (
                    // A share of votes, not a likeness: there is nothing to
                    // meter but how many of its albums say so.
                    <div className="fixmatch__share">
                      {c.Score > 0
                        ? `Credited on ${scorePct(c.Score)} of its matched albums`
                        : "No matched album credits this artist"}
                    </div>
                  ) : (
                    <ScoreBar
                      breakdown={c.Breakdown}
                      total={c.Score}
                      showYear={item.kind !== "episode"}
                      titleOnly={c.Kind === "rom"}
                      album={c.Kind === "album"}
                    />
                  )}
                </div>
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
