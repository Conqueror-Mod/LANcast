import { useCallback, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useItem,
  useTrailer,
  useChildren,
  useCollectionMembers,
  useIsAdmin,
} from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useFocusable, useBackHandler } from "@/focus/FocusController";
import { runtime, rating, ratingLabel } from "@/lib/format";
import { isContainer, childLabel, isPicture } from "@/lib/kind";
import { FixMatch } from "@/components/FixMatch";
import { RemoveDialog } from "@/components/RemoveDialog";
import { PosterTile } from "@/components/PosterTile";
import { TrackList } from "@/components/TrackList";
import { TrailerModal } from "@/components/TrailerModal";
import type { Credit } from "@/api/types";
import "./Detail.css";

function PlayButton({
  onPlay,
  label = "Play",
}: {
  onPlay: () => void;
  label?: string;
}) {
  const focusable = useFocusable(onPlay);
  return (
    <button {...focusable} className="detail__play" onClick={onPlay}>
      <span className="detail__play-icon" aria-hidden="true">
        ▶
      </span>
      {label}
    </button>
  );
}

function BackButton({ onBack }: { onBack: () => void }) {
  const focusable = useFocusable(onBack);
  return (
    <button {...focusable} className="detail__back" onClick={onBack}>
      ← Back
    </button>
  );
}

function FixMatchButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__fix" onClick={onOpen}>
      Fix match
    </button>
  );
}

function TrailerButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__trailer-btn" onClick={onOpen}>
      <span aria-hidden="true">▶</span> Trailer
    </button>
  );
}

function RemoveButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__remove" onClick={onOpen}>
      Remove
    </button>
  );
}

function castOf(credits: Credit[] | undefined) {
  return (credits ?? []).filter((c) => c.role === "actor").slice(0, 12);
}

export function Detail() {
  const { id } = useParams();
  const itemID = Number(id);
  const navigate = useNavigate();

  const { data: item, isLoading, isError } = useItem(itemID);
  const { data: trailer } = useTrailer(itemID);

  // A container (show, season, collection, or a multi-part work) has no file to
  // play — it holds other items. Fetch those; the query stays idle for a plain
  // leaf. A collection's members come through the join table, everything else
  // through parent_id, so exactly one of these fires.
  const container = item ? isContainer(item) : false;
  const isCollection = item?.kind === "collection";
  // An album is asked for in the order it plays; everything else takes the
  // default. Without this a record arrives alphabetically (see useChildren).
  const isAlbum = item?.kind === "album";
  const { data: parentChildren } = useChildren(
    itemID,
    container && !isCollection,
    isAlbum ? "track" : undefined,
  );
  const { data: members } = useCollectionMembers(
    itemID,
    container && isCollection,
  );
  const children = isCollection ? members : parentChildren;

  const [fixOpen, setFixOpen] = useState(false);
  const [trailerOpen, setTrailerOpen] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);
  const isAdmin = useIsAdmin();
  const back = useCallback(() => navigate(-1), [navigate]);
  useBackHandler(back);

  if (isLoading) return <div className="detail detail--empty" />;
  if (isError || !item) {
    return (
      <div className="detail detail--empty">
        <p className="detail__message">This item could not be loaded.</p>
        <BackButton onBack={back} />
      </div>
    );
  }

  /*
   * Theme-music hook point. design.md specifies an ~800ms-delayed, fading theme
   * on the detail page, but the theme-music subsystem is not built yet (blocked
   * on OST identification; ADR 0005). When it lands, it starts here — and must
   * degrade silently when no theme is available. Nothing to play for now.
   */

  const fanart = artworkURL(item.artwork?.fanart, "fanart");
  const poster = artworkURL(item.artwork?.poster, "poster2x");
  const cast = castOf(item.credits);
  // Fix match corrects a provider's identification. A season and a collection
  // have none to correct — one is structural, the other provider-derived — and
  // neither does music: ADR 0024 ships no music provider, so the only thing to
  // search would be TMDB, which would answer a record with films. An artist and
  // an album are assembled from the tags of the files beneath them.
  const isMusic =
    item.kind === "artist" || item.kind === "album" || item.kind === "track";
  const canFixMatch =
    item.kind !== "collection" && item.kind !== "season" && !isMusic;
  // Removing something removes files. An artist and an album have none — they
  // are rows the scanner invented and sweeps when they empty — so, like a
  // collection, they are not offered. A track is a real file and keeps it.
  const canRemove =
    item.kind !== "collection" &&
    item.kind !== "artist" &&
    item.kind !== "album";
  // Children that can be played directly (not themselves containers), in order —
  // the queue behind Play all. A show's children are seasons, so it gets none;
  // a season's episodes, a work's parts, and a collection's films all qualify.
  // Pictures are not playable. Without this a gallery offers "Play all" over
  // its photos and hands the player a JPEG — the kind of nonsense that only
  // appears when a new media type arrives and every screen still assumes a leaf
  // is something you press play on. The viewer replaces this in the next phase
  // (ADR 0028).
  const playableChildren = (children ?? []).filter(
    (c) => !isContainer(c) && !isPicture(c),
  );

  const meta = [
    // An album's artist belongs on the line that says what this is, ahead of
    // the year — "Between the Buried and Me · 2021", the way a record is named.
    isAlbum ? (item.artist ?? "") : "",
    item.year ? String(item.year) : "",
    runtime(item.duration_ms),
    item.content_rating ?? "",
    rating(item.rating) && `★ ${rating(item.rating)}`,
  ].filter(Boolean);

  return (
    <div className="detail">
      {fanart && (
        <div
          className="detail__fanart"
          style={{ backgroundImage: `url(${fanart})` }}
          aria-hidden="true"
        />
      )}
      <div className="detail__scrim" aria-hidden="true" />

      <div className="detail__body">
        <BackButton onBack={back} />

        <div className="detail__hero">
          {poster && (
            <img
              className="detail__poster"
              src={poster}
              alt=""
              draggable={false}
            />
          )}

          <div className="detail__info">
            <h1 className="detail__title">{item.title}</h1>

            {meta.length > 0 && (
              <div className="detail__meta">
                {meta.map((m, i) => (
                  <span key={i}>{m}</span>
                ))}
              </div>
            )}

            {item.genres && item.genres.length > 0 && (
              <div className="detail__genres">{item.genres.join(" · ")}</div>
            )}

            {/* The file this row came from. Shown because a wrongly matched
                title is impossible to correct if you cannot tell which file it
                is — the numbered pieces of an anthology look identical
                otherwise. */}
            {item.file_name && (
              <div className="detail__file" title={item.file_name}>
                {item.file_name}
              </div>
            )}

            {item.ratings && item.ratings.length > 0 && (
              <div className="detail__ratings">
                {item.ratings.map((r) => (
                  <span className="detail__rating" key={r.source}>
                    <span className="detail__rating-src">
                      {ratingLabel(r.source)}
                    </span>
                    <span className="detail__rating-val">{r.display}</span>
                  </span>
                ))}
              </div>
            )}

            {/* Metadata correction lives with the metadata, not the playback
                controls — that was the point of moving it here. A collection
                and a season have no user-correctable identity of their own
                (one is provider-derived, the other structural), so Fix match
                does not apply to them. */}
            {canFixMatch && (
              <div className="detail__metafix">
                {item.metadata_updated_at != null &&
                  (item.match_state === "review" ||
                    item.match_state === "unmatched") && (
                    <span className="detail__matchbadge">
                      {item.match_state === "review"
                        ? "Needs review"
                        : "No match"}
                    </span>
                  )}
                <FixMatchButton onOpen={() => setFixOpen(true)} />
              </div>
            )}

            {/* A leaf plays itself. A container whose children are themselves
                playable (a work's parts, a serial's chapters, a season's
                episodes, a collection's films) offers Play all, which queues
                them in order. A show, whose children are seasons, gets neither —
                you drill into a season first. */}
            <div className="detail__actions">
              {!container && !isPicture(item) && (
                <PlayButton onPlay={() => navigate(`/watch/${item.id}`)} />
              )}
              {container && playableChildren.length > 0 && (
                <PlayButton
                  label="Play all"
                  onPlay={() =>
                    navigate(
                      `/watch/${playableChildren[0].id}?queue=${playableChildren
                        .map((c) => c.id)
                        .join(",")}`,
                    )
                  }
                />
              )}
              {trailer && <TrailerButton onOpen={() => setTrailerOpen(true)} />}
              {isAdmin && canRemove && (
                <RemoveButton onOpen={() => setRemoveOpen(true)} />
              )}
            </div>

            {item.overview && (
              <p className="detail__overview">{item.overview}</p>
            )}

            {cast.length > 0 && (
              <div className="detail__cast">
                <span className="section-label">Cast</span>
                <div className="detail__cast-row">
                  {cast.map((c, i) => (
                    <div className="detail__cast-member" key={i}>
                      <span className="detail__cast-name">{c.name}</span>
                      {c.character && (
                        <span className="detail__cast-character">
                          {c.character}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>

        {container && children && children.length > 0 && (
          <section className="detail__children">
            <span className="section-label">
              {childLabel(children[0]?.kind)}
            </span>
            {/* A record is a numbered list, not a grid: twelve copies of one
                cover say nothing, and a track is identified by its number and
                its length. The album's `artist` is the album artist, so a
                per-track performer shows only where it differs — the
                compilation case (ADR 0024). */}
            {isAlbum ? (
              <TrackList
                tracks={children}
                albumArtist={item.artist ?? undefined}
              />
            ) : (
              <div className="detail__children-grid">
                {children.map((child) => (
                  <PosterTile
                    key={child.id}
                    // A part or chapter often has no artwork of its own (a
                    // miniseries's pieces are not separately matched), which
                    // would leave a blank tile. Fall back to the parent's poster
                    // so the grid reads as one work rather than empty cards.
                    item={
                      child.artwork?.poster
                        ? child
                        : { ...child, artwork: item.artwork }
                    }
                  />
                ))}
              </div>
            )}
          </section>
        )}
      </div>

      {fixOpen && <FixMatch item={item} onClose={() => setFixOpen(false)} />}
      {removeOpen && (
        <RemoveDialog
          item={item}
          onClose={() => setRemoveOpen(false)}
          onDone={() => {
            setRemoveOpen(false);
            back();
          }}
        />
      )}
      {trailerOpen && trailer && (
        <TrailerModal
          trailer={trailer}
          title={item.title}
          onClose={() => setTrailerOpen(false)}
        />
      )}
    </div>
  );
}
