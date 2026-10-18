import { useEffect, useMemo, useRef } from "react";
import { useLyrics } from "@/api/hooks";
import "./LyricsPanel.css";

/*
 * The words, following the song.
 *
 * Synced lyrics are the whole feature; unsynced ones are a text file, and the
 * panel says which it has rather than letting somebody wonder why nothing is
 * moving. Both are worth showing — plenty of .lrc files are somebody's
 * copy-and-paste, and words that do not scroll beat no words.
 *
 * The current line is found from the clock the player chrome already trusts.
 * Nothing here holds its own timer: a second clock is a second thing to be
 * wrong, and on a transcode the element's own currentTime restarts at zero
 * after every seek, which is exactly the bug `displayTime` exists to avoid.
 */

export function LyricsPanel({
  itemID,
  atMS,
  onClose,
  inline = false,
}: {
  itemID: number;
  /** Where the song is, in milliseconds. */
  atMS: number;
  /** Absent inline: a tab is left by choosing the other one. */
  onClose?: () => void;
  /** Laid out in Now Playing's column rather than over the player. */
  inline?: boolean;
}) {
  const { data, isLoading } = useLyrics(itemID);
  const listRef = useRef<HTMLDivElement>(null);
  const activeRef = useRef<HTMLParagraphElement>(null);

  const lines = useMemo(() => data?.lines ?? [], [data]);
  const synced = data?.synced ?? false;

  /*
   * The last line whose moment has passed.
   *
   * A backwards scan, so a seek lands on the right line immediately rather
   * than walking forward from wherever it was — and -1 before the first line,
   * which is a real state: a song with a long intro should highlight nothing
   * rather than pre-highlighting its first words.
   */
  const active = useMemo(() => {
    if (!synced) return -1;
    for (let i = lines.length - 1; i >= 0; i--) {
      if (lines[i].at_ms <= atMS) return i;
    }
    return -1;
  }, [lines, atMS, synced]);

  // Keep the current line in view, and only when it changes — scrolling on
  // every tick would fight anybody reading ahead.
  useEffect(() => {
    if (active < 0) return;
    activeRef.current?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [active]);

  // Said plainly. A panel that simply never highlights anything reads as
  // broken, and "these do not follow the song" is a different and true
  // statement.
  const untimed = !!data && !synced && lines.length > 0;

  return (
    <aside className={"lyrics" + (inline ? " lyrics--inline" : "")} aria-label="Lyrics">
      {/* Inline, the tab above already says Lyrics; the head is kept only for
          the note. */}
      {(!inline || untimed) && (
        <div className="lyrics__head">
          {!inline && <span className="section-label">Lyrics</span>}
          {untimed && <span className="lyrics__note">not timed to the song</span>}
          {onClose && (
            <button className="lyrics__close" onClick={onClose} aria-label="Close lyrics">
              ×
            </button>
          )}
        </div>
      )}

      <div className="lyrics__lines" ref={listRef}>
        {isLoading && <p className="lyrics__empty">Looking…</p>}

        {!isLoading && lines.length === 0 && (
          <p className="lyrics__empty">
            No lyrics for this track. LANcast reads them from an .lrc file
            beside it or from the track's own tags — it does not go looking
            online.
          </p>
        )}

        {lines.map((line, i) => (
          <p
            key={`${line.at_ms}-${i}`}
            ref={i === active ? activeRef : undefined}
            className={
              "lyrics__line" +
              (i === active ? " is-current" : "") +
              (synced && i < active ? " is-past" : "")
            }
          >
            {/* A stamp with no words is an instrumental break, and it is
                meaningful: it is how a lyric sheet says nothing is sung here
                rather than leaving the previous line lit for ninety seconds. */}
            {line.text || "· · ·"}
          </p>
        ))}
      </div>
    </aside>
  );
}
