import { useState } from "react";
import { useReview } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import { FixMatch } from "@/components/FixMatch";
import type { Item } from "@/api/types";
import "./Review.css";

function ReviewRow({ item, onFix }: { item: Item; onFix: () => void }) {
  const focusable = useFocusable(onFix);
  const poster = artworkURL(item.artwork?.poster, "thumb");
  const pct =
    item.match_score != null ? `${Math.round(item.match_score * 100)}%` : "—";

  return (
    <button {...focusable} className="review-row" onClick={onFix}>
      {poster ? (
        <img className="review-row__poster" src={poster} alt="" loading="lazy" />
      ) : (
        <div className="review-row__poster review-row__poster--empty" />
      )}
      <div className="review-row__main">
        <div className="review-row__title">
          {item.title}
          {item.year ? <span className="review-row__year"> ({item.year})</span> : null}
        </div>
        <div className="review-row__meta">
          <span
            className={
              "review-row__badge review-row__badge--" +
              (item.match_state ?? "unmatched")
            }
          >
            {item.match_state === "review" ? "Uncertain" : "No match"}
          </span>
          <span className="review-row__score">best {pct}</span>
        </div>
      </div>
      <span className="review-row__fix">Fix →</span>
    </button>
  );
}

// The metadata-health queue: everything the matcher was not confident about,
// with a one-click path into Fix match (where the score breakdown explains why).
export function Review() {
  const { data, isLoading } = useReview();
  const [fixItem, setFixItem] = useState<Item | null>(null);

  const items = data?.items ?? [];

  return (
    <div className="review">
      <div className="review__head">
        <span className="section-label">Needs review</span>
        <span className="review__rule" />
        {data && <span className="review__count">{data.total}</span>}
      </div>
      <p className="review__lead">
        Matches LANcast wasn't confident about. Opening one shows why it scored
        the way it did; confirming a match locks it against future rescans.
      </p>

      {!isLoading && items.length === 0 && (
        <p className="review__empty">Everything is matched. Nothing to review.</p>
      )}

      <div className="review__list">
        {items.map((item) => (
          <ReviewRow key={item.id} item={item} onFix={() => setFixItem(item)} />
        ))}
      </div>

      {fixItem && <FixMatch item={fixItem} onClose={() => setFixItem(null)} />}
    </div>
  );
}
