/*
 * The photosensitivity warning for the music visualizers.
 *
 * One component so the two places it is said (the Player pane, for anybody
 * who watches; the Visualizers pane, for whoever uploads presets) cannot say
 * different things. The output pass and the motion limits reduce flashing
 * (ADR 0091, vizMath.ts), and Calm halves the motion, but none of that can
 * promise nobody reacts, so the warning does not claim it does. It is the
 * existing warm-edged note, not gold: gold means where you are.
 */
export function PhotosensitivityWarning() {
  return (
    <div className="set-row__note set-row__note--warn" role="note" aria-label="Photosensitivity warning">
      <strong>Photosensitivity warning</strong>
      <span>
        The visualizers show moving and flashing light and patterns. A small number of people can
        have a seizure from images like these, including people who have never had one before.
        LANcast limits how fast the visualizers can flash, and Calm halves their motion, but no
        setting can rule a reaction out.
      </span>
      <span>
        If you or anyone watching has epilepsy or is sensitive to flashing light, keep Now Playing
        on the cover. Stop watching straight away if you feel dizzy, see changes in your vision, or
        notice twitching, confusion or loss of awareness.
      </span>
    </div>
  );
}
