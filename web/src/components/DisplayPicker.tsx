import { useEffect, useRef } from "react";
import { DISPLAY_DEFAULT, useDisplays, type GameRow } from "@/lib/games";

/*
 * "Where should this open?" (ADR 0066's amendment).
 *
 * In the DOM, never a native dialog. A native confirm in a frameless WebView2
 * window is a focus trap — dismissing it does not reliably hand keyboard focus
 * back to the web contents, so the app keeps painting and clicking while
 * nothing can be typed into, which reads as a random freeze that fixes itself
 * when the user alt-tabs away and back.
 *
 * The note at the foot is not boilerplate. This feature cannot work for an
 * exclusive-fullscreen game or one running as administrator, and its failure is
 * invisible from the inside: the game opens, just not where it was asked to.
 * Saying so before somebody chooses is the difference between a limit and a
 * bug.
 */
export function DisplayPicker({
  game,
  onChoose,
  onCancel,
}: {
  game: GameRow;
  onChoose: (device: string) => void;
  onCancel: () => void;
}) {
  const { data: displays = [], isLoading } = useDisplays();
  const first = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    first.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onCancel]);

  return (
    <div
      className="games-modal__scrim"
      // mousedown rather than click, and only when the press started on the
      // scrim: a drag that begins inside the panel and ends outside it is not
      // somebody asking to close.
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onCancel();
      }}
    >
      <div
        className="games-modal"
        role="dialog"
        aria-modal="true"
        aria-label={`Where should ${game.name} open?`}
      >
        <h2 className="games-modal__title">Where should {game.name} open?</h2>
        <p className="games-modal__sub">
          Asked once per game. You can change it later on the game's page.
        </p>

        {isLoading && <p className="games-modal__sub">Looking at your screens…</p>}

        <div className="games-modal__list">
          {displays.map((d, i) => (
            <button
              key={d.device}
              ref={i === 0 ? first : undefined}
              className="games-modal__option"
              onClick={() => onChoose(d.device)}
            >
              <span>{d.label}</span>
              {d.current && (
                <span className="games-modal__here">LANcast is here</span>
              )}
            </button>
          ))}

          {/* A real answer, not a way out of the question: choosing it stops
              the picker coming back for this game. */}
          <button
            className="games-modal__option"
            ref={displays.length === 0 ? first : undefined}
            onClick={() => onChoose(DISPLAY_DEFAULT)}
          >
            <span>Wherever it opens</span>
            <span className="games-modal__here">leave it alone</span>
          </button>
        </div>

        <div className="games-modal__foot">
          <button className="games__secondary" onClick={onCancel}>
            Cancel
          </button>
        </div>

        <p className="games-modal__note">
          LANcast moves the game's window once it appears. A game set to
          exclusive fullscreen uses its own display setting instead, and one that
          runs as administrator cannot be moved at all — in both cases it opens
          where it always did.
        </p>
      </div>
    </div>
  );
}
