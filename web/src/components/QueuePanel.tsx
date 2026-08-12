import { useEffect, useRef } from "react";
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
export function QueuePanel({
  ids,
  currentID,
  onPick,
}: {
  ids: number[];
  currentID: number;
  onPick: (id: number, at: number) => void;
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
  useEffect(() => {
    currentRow.current?.scrollIntoView({ block: "center" });
    // On open only. Re-running as the queue advances would drag the list out
    // from under someone who had scrolled off to look at something else.
  }, []);

  return (
    <div className="queue" role="menu" aria-label="Queue">
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
  );
}

function QueueRow({
  id,
  position,
  current,
  rowRef,
  onPick,
}: {
  id: number;
  position: number;
  current: boolean;
  rowRef?: React.RefObject<HTMLButtonElement>;
  onPick: () => void;
}) {
  const { data: item } = useItem(id);
  return (
    <button
      role="menuitem"
      ref={rowRef}
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
