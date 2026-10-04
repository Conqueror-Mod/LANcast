import { useEffect, useRef, useState } from "react";
import { useItem } from "@/api/hooks";
import { episodeCode } from "@/lib/format";

/*
 * Up Next: the card an episode's credits bring up.
 *
 * # What it is for
 *
 * Skip credits answers "I am done with this one". This answers the question a
 * person watching a season is actually asking at that moment — what is next,
 * and will it start — and, left alone, starts it.
 *
 * # It does not advance the queue itself
 *
 * Both of its ways forward go through `rollOn`, which plays out the last few
 * seconds so the episode ends the way every episode ends. That ending is where
 * an item is recorded as watched, where auto play is consulted and where the
 * still-watching run is counted, and a card with its own path to the next
 * episode would be a second copy of all three that could disagree with the
 * first. The countdown running out is reported as unattended; Play now as a
 * person.
 *
 * # Text, no picture
 *
 * A still from an episode not yet reached is a spoiler (lib/spoilers.ts), and
 * the card appears precisely when that episode has not been reached. Its code
 * and title are what a person needs to recognise it.
 */

/** How long the card counts down before rolling on. */
export const UP_NEXT_SECONDS = 10;

export interface UpNextCardProps {
  nextID: number;
  /** Count down and roll on: auto play is on, and the item is playing. A
   *  pause holds the count; auto play off shows the card with no count. */
  counting: boolean;
  /** The countdown ran out. */
  onTimeout: () => void;
  onPlayNow: () => void;
  onCancel: () => void;
}

export function UpNextCard({ nextID, counting, onTimeout, onPlayNow, onCancel }: UpNextCardProps) {
  const { data: next } = useItem(nextID);
  const [left, setLeft] = useState(UP_NEXT_SECONDS);
  // Fired once per card. Without it a re-render at zero — a pause and resume,
  // a refetch of the next item — would roll on a second time.
  const fired = useRef(false);

  useEffect(() => {
    setLeft(UP_NEXT_SECONDS);
    fired.current = false;
  }, [nextID]);

  useEffect(() => {
    if (!counting || fired.current) return;
    if (left <= 0) {
      fired.current = true;
      onTimeout();
      return;
    }
    const t = window.setTimeout(() => setLeft((n) => n - 1), 1000);
    return () => window.clearTimeout(t);
  }, [counting, left, onTimeout]);

  const code = next ? episodeCode(next) : null;
  return (
    <div className="player__upnext" role="dialog" aria-label="Up next">
      <div className="player__upnext-label">Up next</div>
      <div className="player__upnext-title">
        {code && <span className="player__upnext-code">{code}</span>}
        {next?.title ?? "…"}
      </div>
      {counting && <div className="player__upnext-count">Playing in {Math.max(left, 0)}</div>}
      <div className="player__upnext-actions">
        <button className="player__upnext-play" onClick={onPlayNow}>
          Play now
        </button>
        <button className="player__upnext-cancel" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}
