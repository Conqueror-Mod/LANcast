import { usePrefs, QUALITIES, DEFAULTS, DIALOGUE_LEVELS, CROSSFADES, STAGES, type Stage, type SubFont } from "@/playback/prefs";
import { EQ_PRESETS } from "@/playback/elementAudio";
import { FONTS } from "@/playback/cueVars";
import { COLORS, SIZES } from "./PlaybackSettings";
import { usePlaybackPrefs, useSetPlaybackPrefs } from "@/api/hooks";
import "./PlayerSettings.css";

/*
 * This device › Player: the player's own preferences, settable before
 * anything plays (docs/settings-plan.md, step 2).
 *
 * They lived only in the player's panel, so the first chance to set a quality
 * or turn on night mode was after a film had started. Same preferences, same
 * store (playback/prefs): change one here and the panel shows it, and the
 * other way round. What needs something playing — the audio stream, speed,
 * which subtitle track — stays in the panel, because it is about *this* film.
 */
export function PlayerSettings() {
  const [prefs, setPrefs] = usePrefs();
  return (
    <>
      <section className="settings__section">
        <span className="section-label">Films and episodes</span>
        <label className="set-row">
          <span>Quality on this device</span>
          <select
            className="set-input"
            value={prefs.quality}
            onChange={(e) => setPrefs({ quality: e.target.value })}
          >
            {QUALITIES.map((q) => (
              <option key={q.id} value={q.id}>
                {q.label}
              </option>
            ))}
          </select>
        </label>
        {prefs.quality !== DEFAULTS.quality && (
          <p className="set-row__sub">
            Anything above this is converted on the server before it is sent. The
            server&rsquo;s own limit, if it has one, still applies on top.
          </p>
        )}
        <label className="set-row">
          <span>Night mode</span>
          <select
            className="set-input"
            value={prefs.nightVideo ? "on" : "off"}
            onChange={(e) => setPrefs({ nightVideo: e.target.value === "on" })}
          >
            <option value="off">Off</option>
            <option value="on">On</option>
          </select>
        </label>
        <p className="set-row__sub">
          Lifts quiet speech and tames loud scenes. Where a film&rsquo;s sound cannot
          take it, the player says so and leaves it alone.
        </p>
        <label className="set-row">
          <span>Dialogue boost</span>
          <select
            className="set-input"
            value={String(prefs.dialogueVideo)}
            onChange={(e) => setPrefs({ dialogueVideo: Number(e.target.value) })}
          >
            {DIALOGUE_LEVELS.map((label, level) => (
              <option key={level} value={level}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={prefs.autoPlay}
            onChange={(e) => setPrefs({ autoPlay: e.target.checked })}
          />
          Play the next episode or track automatically
        </label>
        <SkipIntrosToggle />
      </section>

      <section className="settings__section">
        <span className="section-label">Music</span>
        <label className="set-row">
          <span>Night mode</span>
          <select
            className="set-input"
            value={prefs.nightMusic ? "on" : "off"}
            onChange={(e) => setPrefs({ nightMusic: e.target.value === "on" })}
          >
            <option value="off">Off</option>
            <option value="on">On</option>
          </select>
        </label>
        <label className="set-row">
          <span>Equaliser</span>
          <select
            className="set-input"
            value={prefs.eqPreset}
            onChange={(e) => {
              const p = EQ_PRESETS.find((x) => x.id === e.target.value);
              if (p) setPrefs({ eqPreset: p.id, eq: [...p.gains] });
            }}
          >
            {EQ_PRESETS.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
            {prefs.eqPreset === "custom" && <option value="custom">Custom</option>}
          </select>
        </label>
        <p className="set-row__sub">
          The individual bands are in the player&rsquo;s settings while music plays.
        </p>
        <label className="set-row">
          <span>Crossfade between albums</span>
          <select
            className="set-input"
            value={String(prefs.crossfade)}
            onChange={(e) => setPrefs({ crossfade: Number(e.target.value) })}
          >
            {CROSSFADES.map((s) => (
              <option key={s} value={s}>
                {s === 0 ? "Off" : `${s} s`}
              </option>
            ))}
          </select>
        </label>
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={prefs.taper}
            onChange={(e) => setPrefs({ taper: e.target.checked })}
          />
          Fade in at the start and out at the end of a listening session
        </label>
        <label className="set-row">
          <span>Now Playing shows</span>
          <select
            className="set-input"
            value={prefs.stage}
            onChange={(e) => setPrefs({ stage: e.target.value as Stage })}
          >
            {STAGES.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
        {/* Half the motion (docs/now-playing-plan.md, Photosensitivity). On
            regardless while the system asks for reduced motion. */}
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={prefs.vizCalm}
            onChange={(e) => setPrefs({ vizCalm: e.target.checked })}
          />
          Calm visualizers: half the motion
        </label>
      </section>
    </>
  );
}

/*
 * Your account › Languages & subtitles: how subtitles look.
 *
 * Per account (decided 2026-10-12): it follows the person to every device.
 * Edited through this device's preferences, which SubtitleStyleSync keeps in
 * step with the account, so the player's panel and this agree wherever the
 * change is made.
 */
export function SubtitleStyleSettings() {
  const [prefs, setPrefs] = usePrefs();
  return (
    <section className="settings__section">
      <span className="section-label">Subtitle style</span>
      <div
        className="subpreview"
        aria-hidden="true"
        style={{ paddingBottom: `${Math.max(prefs.subPosition, 2) / 2}%` }}
      >
        <span
          className={`subpreview__cue subpreview__cue--${prefs.subFont}`}
          style={{ color: prefs.subColor, fontSize: `${prefs.subSize * 1.1}rem` }}
        >
          This is how your subtitles look.
        </span>
      </div>
      <label className="set-row">
        <span>Colour</span>
        <select
          className="set-input"
          value={prefs.subColor}
          onChange={(e) => setPrefs({ subColor: e.target.value })}
        >
          {COLORS.map((c) => (
            <option key={c.value} value={c.value}>
              {c.label}
            </option>
          ))}
        </select>
      </label>
      <label className="set-row">
        <span>Typeface</span>
        <select
          className="set-input"
          value={prefs.subFont}
          onChange={(e) => setPrefs({ subFont: e.target.value as SubFont })}
        >
          {FONTS.map((f) => (
            <option key={f.value} value={f.value}>
              {f.label}
            </option>
          ))}
        </select>
      </label>
      <label className="set-row">
        <span>Size</span>
        <select
          className="set-input"
          value={String(prefs.subSize)}
          onChange={(e) => setPrefs({ subSize: Number(e.target.value) })}
        >
          {SIZES.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </label>
      <label className="set-row">
        <span>Height above the bottom</span>
        <input
          type="range"
          min={0}
          max={40}
          step={1}
          value={prefs.subPosition}
          onChange={(e) => setPrefs({ subPosition: Number(e.target.value) })}
          aria-label="Subtitle height above the bottom"
        />
      </label>
      <p className="set-row__sub">
        Follows your account to every device you sign in on. The subtitle offset is
        not part of it: that corrects timing for one file, and stays in the player.
      </p>
    </section>
  );
}

/*
 * Whether an intro skips itself (ADR 0083). Per account, unlike the rows
 * around it, so it says so: it follows the person to every device, the way
 * their subtitle style does. Not drawn until the account's answer is known, so
 * the box never shows a state the server does not hold.
 */
function SkipIntrosToggle() {
  const prefs = usePlaybackPrefs();
  const set = useSetPlaybackPrefs();
  if (!prefs.data) return null;
  return (
    <>
      <label className="set-toggle">
        <input
          type="checkbox"
          checked={prefs.data.skip_intros}
          disabled={set.isPending}
          onChange={(e) => set.mutate({ skip_intros: e.target.checked })}
        />
        Skip intros automatically
      </label>
      <p className="set-row__sub">
        When an episode reaches its title sequence, it skips it and says so, with
        Watch intro to go back. Follows your account to every device you sign in
        on. Credits always keep their button.
      </p>
    </>
  );
}
