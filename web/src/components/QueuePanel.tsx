import { createContext, memo, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
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
/*
 * How much of a long queue is on the page at once.
 *
 * Every row used to be rendered, and re-rendered on every tick of the
 * playback clock (about four a second), because the panel had no way to tell
 * a new position in the song from a new queue. On "shuffle the whole library"
 * that is 9,000 rows, each with a query subscription: one re-render measured
 * 690 ms in the test DOM, against 50 ms for 500 rows, and that work landed on
 * the same thread as the visualizer's frames. Reported by Chris as jitter
 * while a whole library was shuffled and none on an album.
 *
 * So the list is memoised (a new position in the song re-renders nothing),
 * and past WINDOW_FROM rows only a window around the playing track is drawn,
 * growing by WINDOW_STEP as either end comes near view. Numbers stay the
 * queue's own, so track 41 of 9,000 still reads 41.
 */
export const WINDOW_FROM = 300;
export const WINDOW_BEFORE = 30;
export const WINDOW_AFTER = 150;
export const WINDOW_STEP = 200;

interface PanelProps {
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
}

const NO_LANE: number[] = [];

/*
 * The callers hand over fresh arrow functions on every render, which would
 * defeat any memo. The wrapper keeps the latest in a ref and gives the list
 * callbacks that never change, so the list re-renders only for what it shows.
 */
export function QueuePanel({ ids, currentID, onPick, upNext = NO_LANE, onDrop, inline = false }: PanelProps) {
  const pickRef = useRef(onPick);
  pickRef.current = onPick;
  const dropRef = useRef(onDrop);
  dropRef.current = onDrop;
  const pick = useCallback((id: number, at: number) => pickRef.current(id, at), []);
  const drop = useCallback((at: number) => dropRef.current?.(at), []);
  return (
    <QueueList
      ids={ids}
      currentID={currentID}
      upNext={upNext}
      inline={inline}
      onPick={pick}
      onDrop={onDrop ? drop : undefined}
    />
  );
}

/** Which slice of `ids` is drawn: everything for a short queue, a window for a long one. */
export function initialWindow(length: number, at: number): [number, number] {
  if (length <= WINDOW_FROM) return [0, length];
  const centre = at < 0 ? 0 : at;
  return [Math.max(0, centre - WINDOW_BEFORE), Math.min(length, centre + WINDOW_AFTER)];
}

const QueueList = memo(function QueueList({
  ids,
  currentID,
  onPick,
  upNext,
  onDrop,
  inline,
}: {
  ids: number[];
  currentID: number;
  onPick: (id: number, at: number) => void;
  upNext: number[];
  onDrop?: (at: number) => void;
  inline: boolean;
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
  const at = ids.indexOf(currentID);

  const [win, setWin] = useState<{ ids: number[]; range: [number, number] }>(() => ({
    ids,
    range: initialWindow(ids.length, at),
  }));
  // A new queue, or a playing track outside what is drawn: centre on it again.
  let [from, to] = win.range;
  if (win.ids !== ids || (at >= 0 && (at < from || at >= to))) {
    [from, to] = initialWindow(ids.length, at);
    setWin({ ids, range: [from, to] });
  }

  // Rows added above would push everything down under the reader; the scroll
  // is moved by what was added, so the row they were looking at stays put.
  const heightBefore = useRef<number | null>(null);
  const grow = useCallback((up: boolean) => {
    setWin((w) => {
      const [f, t] = w.range;
      if (up) {
        heightBefore.current = panel.current?.scrollHeight ?? null;
        return { ids: w.ids, range: [Math.max(0, f - WINDOW_STEP), t] };
      }
      return { ids: w.ids, range: [f, Math.min(w.ids.length, t + WINDOW_STEP)] };
    });
  }, []);
  useLayoutEffect(() => {
    const box = panel.current;
    if (box && heightBefore.current !== null) {
      box.scrollTop += box.scrollHeight - heightBefore.current;
    }
    heightBefore.current = null;
  }, [from]);

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

  const shown = ids.slice(from, to);
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
                at={-1}
                lane={i}
                current={false}
                onPick={onPick}
                onDrop={onDrop}
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
        {from > 0 && <QueueMore count={from} up onGrow={grow} />}
        {shown.map((id, k) => {
          const i = from + k;
          return (
            <QueueRow
              key={id}
              id={id}
              position={i + 1}
              // The row's position, not just its id: two rows in a playlist can
              // carry the same track, and only the position says which was
              // pressed.
              at={i}
              current={id === currentID}
              rowRef={id === currentID ? currentRow : undefined}
              onPick={onPick}
            />
          );
        })}
        {to < ids.length && <QueueMore count={ids.length - to} up={false} onGrow={grow} />}
      </div>
    </div>
    </RowSightContext.Provider>
  );
});

/*
 * The edge of a window: grows it when it comes near view, and is a button for
 * anything that cannot scroll it there (no IntersectionObserver, a remote).
 */
function QueueMore({ count, up, onGrow }: { count: number; up: boolean; onGrow: (up: boolean) => void }) {
  const own = useRef<HTMLButtonElement>(null);
  const sight = useContext(RowSightContext);
  useEffect(() => {
    if (!sight || !own.current) return;
    return sight(own.current, () => onGrow(up));
    // A new count is a new edge, to be watched afresh.
  }, [sight, count, up, onGrow]);
  return (
    <button ref={own} className="queue__more" onClick={() => onGrow(up)}>
      {up ? `${count.toLocaleString()} earlier` : `${count.toLocaleString()} more`}
    </button>
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

const QueueRow = memo(function QueueRow({
  id,
  position,
  at,
  lane,
  current,
  rowRef,
  onPick,
  onDrop,
}: {
  id: number;
  position: number;
  /** Its place in the order, for onPick; -1 on the hand-queued lane. */
  at: number;
  /** Its place in the lane, for onDrop. */
  lane?: number;
  current: boolean;
  rowRef?: React.RefObject<HTMLButtonElement>;
  onPick: (id: number, at: number) => void;
  /** Present only on the hand-queued lane, where taking one back off is safe. */
  onDrop?: (at: number) => void;
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
      onClick={() => onPick(id, at)}
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

  if (!onDrop || lane === undefined) return row;
  return (
    <div className="queue__line">
      {row}
      <button
        className="queue__drop"
        onClick={() => onDrop(lane)}
        aria-label={`Remove ${item?.title ?? `item ${position}`} from up next`}
        title="Remove from up next"
      >
        ×
      </button>
    </div>
  );
});

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
