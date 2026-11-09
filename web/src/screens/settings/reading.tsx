import { useReadingPrefs, useSetReadingPrefs } from "@/lib/readingPrefs";
import { BOOK_SPEEDS } from "@/playback/bookTimeline";
import type { ReadingPreferencesUpdate } from "@/api/types";

/*
 * Your account › Reading & listening (settings-3-plan step 4, ADR 0120).
 *
 * Defaults for audiobooks and comics. Each is about how you like to read or
 * listen, so it follows you; and each is only a default, so the book player
 * and the reader can still do something else on the night.
 */

function speedLabel(s: number): string {
  return s === 1 ? "1× (normal)" : `${s}×`;
}

export function ReadingPane() {
  const { data: prefs } = useReadingPrefs();
  const set = useSetReadingPrefs();
  const account = prefs?.account === true;
  const save = (u: ReadingPreferencesUpdate) => set.mutate(u);

  return (
    <>
      <section className="settings__section">
        <span className="section-label">Audiobooks</span>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Speed</div>
            <div className="set-row__sub">
              What a book starts at. A book you have set a speed for in the
              player keeps its own.
            </div>
          </div>
          <div className="set-row__actions">
            <select
              className="set-input"
              aria-label="Audiobook speed"
              disabled={!account}
              value={String(prefs?.book_speed ?? 1)}
              onChange={(e) => save({ book_speed: Number(e.target.value) as ReadingPreferencesUpdate["book_speed"] })}
            >
              {BOOK_SPEEDS.map((s) => (
                <option key={s} value={String(s)}>
                  {speedLabel(s)}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Sleep timer</div>
            <div className="set-row__sub">
              Set when a book starts playing, for listening in bed. It fades out
              rather than stopping dead, and the player can change or cancel it.
            </div>
          </div>
          <div className="set-row__actions">
            <select
              className="set-input"
              aria-label="Sleep timer"
              disabled={!account}
              value={prefs?.book_sleep ?? "off"}
              onChange={(e) => save({ book_sleep: e.target.value as ReadingPreferencesUpdate["book_sleep"] })}
            >
              <option value="off">Off</option>
              <option value="15">15 minutes</option>
              <option value="30">30 minutes</option>
              <option value="45">45 minutes</option>
              <option value="60">1 hour</option>
              <option value="chapter">End of chapter</option>
            </select>
          </div>
        </div>
      </section>

      <section className="settings__section">
        <span className="section-label">Comics</span>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Pages</div>
            <div className="set-row__sub">
              Two facing pages show the cover alone, then each pair as it was
              printed. A screen taller than it is wide always shows one.
            </div>
          </div>
          <div className="set-row__actions">
            <select
              className="set-input"
              aria-label="Comic pages"
              disabled={!account}
              value={prefs?.comic_layout ?? "single"}
              onChange={(e) => save({ comic_layout: e.target.value as ReadingPreferencesUpdate["comic_layout"] })}
            >
              <option value="single">One at a time</option>
              <option value="spread">Two facing pages</option>
            </select>
          </div>
        </div>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Fit</div>
            <div className="set-row__sub">
              The whole page on screen, or as wide as the screen and scrolled
              down — easier on a phone, where a whole page is small.
            </div>
          </div>
          <div className="set-row__actions">
            <select
              className="set-input"
              aria-label="Comic fit"
              disabled={!account}
              value={prefs?.comic_fit ?? "page"}
              onChange={(e) => save({ comic_fit: e.target.value as ReadingPreferencesUpdate["comic_fit"] })}
            >
              <option value="page">Whole page</option>
              <option value="width">Fit to width</option>
            </select>
          </div>
        </div>
      </section>
      {set.isError && (
        <p className="set-row__sub" role="alert">
          That did not save: {(set.error as Error).message}
        </p>
      )}
    </>
  );
}
