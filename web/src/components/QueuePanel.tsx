import { createContext, useContext, useEffect, useRef, useState } from "react";
import { useItem } from "@/api/hooks";
import type { MediaStream } from "@/api/types";
import "./QueuePanel.css";

// What is playing next, and a way to jump within it.
//
// One row per id, each fetching its own item, which is the same shape the home
// screen uses for per-library shelves and for the same reason: a component per
// row owns its query without anyone calling hooks in a loop. The rows are
// already in the cache in the ordinary case — the queue came from a container
// whose children were just listed — so this is usually no request at all.
//
// A row asks for its item only once it is near enough to be seen (RowSight
// below). "Shuffle the whole library" is a queue of 1,600 tracks, none of them
// cached, and asking for all of them at once broke playback itself.
export function QueuePanel({
  ids,
  currentID,
  onPick,
  upNext = [],
  onDrop,
  inline = false,
}: {
  /**
   * Laid out on the page (Now Playing's column) rather than as a popover over
   * the player. It scrolls itself and follows the track that is playing.
   */
  inline?: boolean;
  ids: number[];
  currentID: number;
  onPick: (id: number, at: number) => void;
  /**
   * Items queued by hand, which play before the queue resumes.
   *
   * Shown in a section of their own because they are not in `ids` and never
   * will be — they are a lane beside the queue, not an insertion into it. Until
   * this existed, queueing something put it somewhere invisible: the panel
   * showed the queue it was not part of, so the only evidence it had worked was
   * the track eventually playing.
   */
  upNext?: number[];
  /** Drops one entry from that lane, by position. */
  onDrop?: (at: number) => void;
}) {
  /*
   * Open the panel on the track that is playing, not on the top of the list.
   *
   * The current row was always marked; on a queue of a dozen that was enough.
   * On "shuffle the whole library" it is 1,591 rows and the marked one is eight
   * hundred down, so the panel opened on a wall of songs with no indication
   * that any of them was the one you could hear. A highlight nobody can reach
   * is the same as no highlight.
   *
   * `block: "center"` rather than "nearest": the useful thing about opening
   * here is seeing what comes *next*, which means the current row wants room
   * beneath it.
   */
  const currentRow = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const sight = useRowSight(panel);
  useEffect(() => {
    if (inline) return;
    currentRow.current?.scrollIntoView({ block: "center" });
    // On open only. Re-running as the queue advances would drag the list out
    // from under someone who had scrolled off to look at something else.
  }, [inline]);

  /*
   * On the page, the list follows the record: the playing row goes to the top
   * whenever the track changes, so what is visible is what comes next.
   *
   * A popover is opened to look at something and closed again, so dragging it
   * would be rude. A column that stays on screen for an album and goes on
   * showing track 1 while track 9 plays is just wrong. Once per track, not on
   * every tick, so somebody scrolling to read is moved at most when a song
   * ends.
   *
   * The scroll is set on the panel itself rather than with scrollIntoView,
   * which also scrolls every scrollable ancestor to get there. `.player` is
   * overflow: hidden, and hidden still scrolls programmatically, so the whole
   * screen could slide up under the cover.
   */
  useEffect(() => {
    if (!inline) return;
    const box = panel.current;
    const row = currentRow.current;
    if (!box || !row) return;
    box.scrollTop += row.getBoundingClientRect().top - box.getBoundingClientRect().top;
  }, [inline, currentID]);

  return (
    <RowSightContext.Provider value={sight}>
    <div
      ref={panel}
      className={"queue" + (inline ? " queue--inline" : "")}
      role="menu"
      aria-label="Queue"
    >
      {upNext.length > 0 && (
        <>
          <div className="queue__head">
            <span className="section-label">Up next</span>
            <span className="queue__count">{upNext.length}</span>
          </div>
          <div className="queue__list">
            {upNext.map((id, i) => (
              <QueueRow
                // Position, not id: the lane can hold the same track twice on
                // purpose, and keying on id would collapse the pair into one
                // row — silently shortening the list somebody is reading.
                key={`${id}@${i}`}
                id={id}
                position={i + 1}
                current={false}
                onPick={() => onPick(id, -1)}
                onDrop={onDrop && (() => onDrop(i))}
              />
            ))}
          </div>
        </>
      )}
      <div className="queue__head">
        <span className="section-label">Queue</span>
        <span className="queue__count">{ids.length}</span>
      </div>
      <div className="queue__list">
        {ids.map((id, i) => (
          <QueueRow
            key={id}
            id={id}
            position={i + 1}
            current={id === currentID}
            rowRef={id === currentID ? currentRow : undefined}
            // The row's position, not just its id: two rows in a playlist can
            // carry the same track, and only the position says which was
            // pressed.
            onPick={() => onPick(id, i)}
          />
        ))}
      </div>
    </div>
    </RowSightContext.Provider>
  );
}

/*
 * ---- which rows are worth fetching ------------------------------------------
 *
 * Every row used to fetch its item the moment it mounted. On a queue of an
 * album that is a dozen requests, mostly answered from the cache. On "shuffle
 * the whole library" it was 1,600 requests in the same instant, none cached,
 * and Chromium does not queue that many: it fails the overflow locally with
 * "Failed to fetch" before anything reaches the server.
 *
 * Among the failures were the two requests that mattered. Now Playing mounts
 * this list as the first track starts, so the track's own `/playback` question
 * and its direct stream were in the same burst. Measured on the reporting
 * install, 2026-10-25: with 1,600 item fetches in flight, the `/playback` fetch
 * and a direct `<audio>` load of an ordinary 320 kbps MP3 both failed in 858 ms,
 * and neither one reached the server. The player read the failed load as "this
 * browser cannot play the file", asked for a conversion, and was told —
 * correctly — that the file plays directly. Shown as "That file would not play
 * directly" and then "The server refused to start playback" on the first
 * track of nearly every whole-library shuffle.
 *
 * So a row asks only once it has been near the visible part of the list, and
 * then keeps asking — scrolling it away again must not blank a title already
 * shown. Without IntersectionObserver there is nothing to measure with, and
 * every row asks, as before.
 */
type RowSight = ((el: Element, onSeen: () => void) => () => void) | null;
const RowSightContext = createContext<RowSight>(null);

function useRowSight(panel: React.RefObject<HTMLDivElement>): RowSight {
  const [sight, setSight] = useState<RowSight>(null);
  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") return;
    const waiting = new Map<Element, () => void>();
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const seen = waiting.get(e.target);
          waiting.delete(e.target);
          io.unobserve(e.target);
          seen?.();
        }
      },
      // A screen's worth ahead, so a row has its title by the time it is
      // scrolled to rather than as it arrives.
      { root: panel.current, rootMargin: "400px 0px" },
    );
    setSight(() => (el: Element, onSeen: () => void) => {
      waiting.set(el, onSeen);
      io.observe(el);
      return () => {
        waiting.delete(el);
        io.unobserve(el);
      };
    });
    return () => io.disconnect();
  }, [panel]);
  return sight;
}

/** Whether this row has been near enough to view to be worth its request. */
function useSeen(el: React.RefObject<Element>): boolean {
  const sight = useContext(RowSightContext);
  const [seen, setSeen] = useState(false);
  const measurable = typeof IntersectionObserver !== "undefined";
  useEffect(() => {
    if (seen || !sight || !el.current) return;
    return sight(el.current, () => setSeen(true));
  }, [seen, sight, el]);
  return seen || !measurable;
}

function QueueRow({
  id,
  position,
  current,
  rowRef,
  onPick,
  onDrop,
}: {
  id: number;
  position: number;
  current: boolean;
  rowRef?: React.RefObject<HTMLButtonElement>;
  onPick: () => void;
  /** Present only on the hand-queued lane, where taking one back off is safe. */
  onDrop?: () => void;
}) {
  const own = useRef<HTMLButtonElement>(null);
  const seen = useSeen(own);
  const { data: item } = useItem(seen ? id : 0);
  // The current row's ref belongs to the panel, which scrolls to it; this one
  // is the row's own, for being seen. A button takes one ref, so both are set.
  const setRef = (el: HTMLButtonElement | null) => {
    (own as React.MutableRefObject<HTMLButtonElement | null>).current = el;
    if (rowRef) (rowRef as React.MutableRefObject<HTMLButtonElement | null>).current = el;
  };
  /*
   * The drop control sits beside the row rather than inside it, for the reason
   * TrackList records: a button cannot contain a button, and the browser's
   * recovery is to drop one — so a × inside the jump target would either not
   * work or would jump to the track it was meant to remove.
   */
  const row = (
    <button
      role="menuitem"
      ref={setRef}
      className={"queue__row" + (current ? " is-current" : "")}
      onClick={onPick}
      aria-current={current ? "true" : undefined}
    >
      <span className="queue__num">{position}</span>
      <span className="queue__title">
        {/* Falls back to the number rather than to "Loading…": the row is
            already identified by its position, and a list of placeholders
            reads as a broken queue rather than a loading one. */}
        {item?.title ?? `Item ${position}`}
      </span>
      {item?.artist && <span className="queue__sub">{item.artist}</span>}
    </button>
  );

  if (!onDrop) return row;
  return (
    <div className="queue__line">
      {row}
      <button
        className="queue__drop"
        onClick={onDrop}
        aria-label={`Remove ${item?.title ?? `item ${position}`} from up next`}
        title="Remove from up next"
      >
        ×
      </button>
    </div>
  );
}

// audioLabel names a track the way a person would choose one: language first,
// then what makes it different from its neighbours.
//
// A file's second English track is usually a commentary or a descriptive audio
// mix, and the title is where that is written. Channels disambiguate the other
// common case — the same language twice, stereo and 5.1 — which is otherwise
// two identical-looking rows.
export function audioLabel(t: MediaStream): string {
  const parts: string[] = [];
  if (t.language) parts.push(t.language.toUpperCase());
  if (t.title) parts.push(t.title);
  if (t.channels === 2) parts.push("Stereo");
  else if (t.channels === 6) parts.push("5.1");
  else if (t.channels === 8) parts.push("7.1");
  else if (t.channels === 1) parts.push("Mono");
  if (t.codec) parts.push(t.codec.toUpperCase());
  // Nothing at all is possible on a bare stream; the index is the only honest
  // thing left to call it.
  return parts.length > 0 ? parts.join(" · ") : `Track ${t.index}`;
}
