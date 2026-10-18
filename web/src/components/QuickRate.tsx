import { useState } from "react";
import { useSetRating } from "@/api/hooks";
import type { Item } from "@/api/types";
import "./QuickRate.css";

/*
 * Five stars under a tile on Rate what you've watched.
 *
 * Whole stars only, unlike the detail page's control (RateItem), which reaches
 * a half by a second click on the same star. Here the first click is the
 * answer and the tile leaves the shelf, so there is no second click to make;
 * the half is a detail-page refinement, one click away from any poster.
 *
 * Nothing is lit at rest: an unrated title has no stars to show, and the only
 * gold is under the pointer or the focus, which is where you are.
 */
export function QuickRate({ item }: { item: Item }) {
  const set = useSetRating(item.id);
  const [hover, setHover] = useState(0);
  return (
    <div
      className="quickrate"
      role="group"
      aria-label={`Rate ${item.title}`}
      onMouseLeave={() => setHover(0)}
    >
      {[1, 2, 3, 4, 5].map((star) => (
        <button
          key={star}
          type="button"
          className={"quickrate__star" + (hover >= star ? " is-lit" : "")}
          disabled={set.isPending}
          onMouseEnter={() => setHover(star)}
          onFocus={() => setHover(star)}
          onBlur={() => setHover(0)}
          onClick={() => set.mutate({ score: star * 2 })}
          aria-label={`${star} star${star === 1 ? "" : "s"}`}
        >
          ★
        </button>
      ))}
    </div>
  );
}
