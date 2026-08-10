import { useNavigate } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import { containerCountLabel, isSquareArt } from "@/lib/kind";
import { rating } from "@/lib/format";
import type { Item } from "@/api/types";
import "./PosterTile.css";

function progressPct(item: Item): number {
  const pos = item.progress?.position_ms ?? 0;
  const dur = item.duration_ms ?? 0;
  if (!pos || !dur) return 0;
  return Math.min(100, (pos / dur) * 100);
}

// onOpen overrides what pressing the tile does. A photo in a gallery selects
// itself into the banner above rather than navigating: a photograph has no
// detail page worth visiting — no synopsis, no cast, no year — and sending
// someone to one would be a worse answer than showing them the picture.
export function PosterTile({
  item,
  onOpen,
}: {
  item: Item;
  onOpen?: () => void;
}) {
  const navigate = useNavigate();
  const open = onOpen ?? (() => navigate(`/item/${item.id}`));
  const focusable = useFocusable(open);

  const poster = artworkURL(item.artwork?.poster, "poster");
  const pct = progressPct(item);
  // A container shows how much it holds ("3 seasons"); a leaf shows its year.
  const count = containerCountLabel(item);
  const score = rating(item.rating);

  return (
    <button
      {...focusable}
      className="poster-tile"
      onClick={open}
      title={item.title}
      aria-label={item.title}
    >
      <div
        className={
          "poster-tile__art" +
          (isSquareArt(item) ? " poster-tile__art--square" : "")
        }
      >
        {poster ? (
          <img src={poster} alt="" loading="lazy" draggable={false} />
        ) : (
          <div className="poster-tile__placeholder">
            <span>{item.title}</span>
          </div>
        )}
        {item.content_rating && (
          <span className="poster-tile__cert">{item.content_rating}</span>
        )}
        {score && (
          <span className="poster-tile__rating">
            <span className="poster-tile__star" aria-hidden="true">
              ★
            </span>
            {score}
          </span>
        )}
        {pct > 0 && (
          <div className="poster-tile__progress" style={{ width: `${pct}%` }} />
        )}
      </div>
      {/*
        A photo's caption is its filename, and a photo library's filenames are
        UUIDs and camera serials — the reason the scanner stores them verbatim
        rather than tidying them. Printing 2,600 of those under a grid is noise
        that makes the pictures harder to look at, so the tile carries the title
        for assistive technology (aria-label, above) and shows nothing.
      */}
      {item.kind !== "photo" && (
        <div className="poster-tile__meta">
          <span className="poster-tile__title">{item.title}</span>
          {count ? (
            <span className="poster-tile__year">{count}</span>
          ) : (
            item.year && <span className="poster-tile__year">{item.year}</span>
          )}
        </div>
      )}
    </button>
  );
}
