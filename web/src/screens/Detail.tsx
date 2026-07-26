import { useCallback, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useItem, useTrailer } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useFocusable, useBackHandler } from "@/focus/FocusController";
import { runtime, rating } from "@/lib/format";
import { FixMatch } from "@/components/FixMatch";
import { TrailerModal } from "@/components/TrailerModal";
import type { Credit } from "@/api/types";
import "./Detail.css";

function PlayButton({ onPlay }: { onPlay: () => void }) {
  const focusable = useFocusable(onPlay);
  return (
    <button {...focusable} className="detail__play" onClick={onPlay}>
      <span className="detail__play-icon" aria-hidden="true">
        ▶
      </span>
      Play
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

function castOf(credits: Credit[] | undefined) {
  return (credits ?? []).filter((c) => c.role === "actor").slice(0, 12);
}

export function Detail() {
  const { id } = useParams();
  const itemID = Number(id);
  const navigate = useNavigate();

  const { data: item, isLoading, isError } = useItem(itemID);
  const { data: trailer } = useTrailer(itemID);

  const [fixOpen, setFixOpen] = useState(false);
  const [trailerOpen, setTrailerOpen] = useState(false);
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

  const meta = [
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
            <img className="detail__poster" src={poster} alt="" draggable={false} />
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

            {/* Metadata correction lives with the metadata, not the playback
                controls — that was the point of moving it here. */}
            <div className="detail__metafix">
              {item.metadata_updated_at != null &&
                (item.match_state === "review" ||
                  item.match_state === "unmatched") && (
                  <span className="detail__matchbadge">
                    {item.match_state === "review" ? "Needs review" : "No match"}
                  </span>
                )}
              <FixMatchButton onOpen={() => setFixOpen(true)} />
            </div>

            <div className="detail__actions">
              <PlayButton onPlay={() => navigate(`/watch/${item.id}`)} />
              {trailer && <TrailerButton onOpen={() => setTrailerOpen(true)} />}
            </div>

            {item.overview && <p className="detail__overview">{item.overview}</p>}

            {cast.length > 0 && (
              <div className="detail__cast">
                <span className="section-label">Cast</span>
                <div className="detail__cast-row">
                  {cast.map((c, i) => (
                    <div className="detail__cast-member" key={i}>
                      <span className="detail__cast-name">{c.name}</span>
                      {c.character && (
                        <span className="detail__cast-character">{c.character}</span>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {fixOpen && <FixMatch item={item} onClose={() => setFixOpen(false)} />}
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
