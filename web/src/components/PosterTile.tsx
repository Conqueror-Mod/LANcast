import { useNavigate } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./PosterTile.css";

function progressPct(item: Item): number {
  const pos = item.progress?.position_ms ?? 0;
  const dur = item.duration_ms ?? 0;
  if (!pos || !dur) return 0;
  return Math.min(100, (pos / dur) * 100);
}

export function PosterTile({ item }: { item: Item }) {
  const navigate = useNavigate();
  const open = () => navigate(`/item/${item.id}`);
  const focusable = useFocusable(open);

  const poster = artworkURL(item.artwork?.poster, "poster");
  const pct = progressPct(item);

  return (
    <button
      {...focusable}
      className="poster-tile"
      onClick={open}
      title={item.title}
      aria-label={item.title}
    >
      <div className="poster-tile__art">
        {poster ? (
          <img src={poster} alt="" loading="lazy" draggable={false} />
        ) : (
          <div className="poster-tile__placeholder">
            <span>{item.title}</span>
          </div>
        )}
        {pct > 0 && (
          <div className="poster-tile__progress" style={{ width: `${pct}%` }} />
        )}
      </div>
      <div className="poster-tile__meta">
        <span className="poster-tile__title">{item.title}</span>
        {item.year && <span className="poster-tile__year">{item.year}</span>}
      </div>
    </button>
  );
}
