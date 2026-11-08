import { useState } from "react";
import { useSettings, useUpdateSettings } from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import { RuleNumber, RuleSelect } from "./shared";

/*
 * Pre-roll (ADR 0099): a folder of clips, one played before a film started
 * from the beginning. Empty is off. Saved when the box loses focus, like the
 * other folder settings; the server checks it is a folder and says if not.
 */
export function PrerollSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const [dir, setDir] = useState<string | null>(null);
  // Said once the server has taken it: a box that saves when you leave it
  // and says nothing reads as a box that does not save.
  const [done, setDone] = useState<string | null>(null);
  if (!settings) return null;
  const saved = settings.preroll_dir ?? "";
  const clips = settings.preroll_clips ?? 0;
  return (
    <section className="settings__section">
      <span className="section-label">Pre-roll</span>
      <label className="set-row">
        <span>Folder of clips</span>
        <input
          className="set-input"
          aria-label="Pre-roll folder"
          placeholder="Off"
          value={dir ?? saved}
          onChange={(e) => {
            setDir(e.target.value);
            setDone(null);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
          }}
          onBlur={() => {
            if (dir !== null && dir.trim() !== saved) {
              const next = dir.trim();
              update.mutate({ preroll_dir: next }, { onSuccess: () => setDone(next) });
            }
            setDir(null);
          }}
        />
      </label>
      {done !== null && (
        <span className="set-note" role="status">
          {done ? "Saved." : "Saved. Pre-roll is off."}
        </span>
      )}
      {/* What the folder holds now, from the server: a folder that is set
          but holds no video plays nothing, and saying "saved" alone would
          let that pass for working. */}
      {saved && dir === null && (
        <p className={clips === 0 ? "set-error" : "set-row__sub"}>
          {clips === 0
            ? "This folder has no videos in it, so no clip plays until one is added."
            : clips === 1
              ? "1 clip in this folder."
              : `${clips} clips in this folder; one plays at random.`}
        </p>
      )}
      <p className="set-row__sub">
        A short clip of your choosing, such as a cinema intro, played before a film someone starts
        from the beginning. Never when they carry on watching, never before an episode, and anyone can
        skip it or turn it off for themselves. Put the clips in a folder, such as{" "}
        <code>D:\LANcast\pre-roll</code>; one plays at random. MP4 plays everywhere. Leave empty
        for no pre-roll.
      </p>
      {update.isError && <span className="set-error">{errorMessage(update.error)}</span>}
    </section>
  );
}

export function ProgressPane() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  return (
    <>
      <PrerollSection />
      <section className="settings__section">
        <span className="section-label">Playback &amp; progress</span>
        {settings && (
          <>
            <RuleSelect
              title="Counts as watched at"
              sub="Stop past this much of a film or episode and it is finished. Credits are not the film, and a shelf that keeps offering the last ninety seconds back is a shelf nobody clears."
              value={settings.watched_threshold}
              options={[
                { value: 70, label: "70%" },
                { value: 80, label: "80%" },
                { value: 85, label: "85%" },
                { value: 90, label: "90%" },
                { value: 95, label: "95%" },
                { value: 100, label: "100% — only at the end" },
              ]}
              onChange={(v) => update.mutate({ watched_threshold: v })}
            />
            <RuleNumber
              title="Weeks to keep in Continue Watching"
              sub="Anything untouched for longer drops off the shelf. 0 keeps everything for ever — the half hour of a documentary you abandoned in March is not something you are in the middle of, and it pushes out what you paused last night."
              value={settings.continue_weeks}
              min={0}
              max={520}
              onCommit={(v) => update.mutate({ continue_weeks: v })}
            />
            <RuleNumber
              title="Items in Continue Watching"
              sub="How many the shelf holds at most. A client may ask for fewer; it cannot ask for more."
              value={settings.continue_limit}
              min={1}
              max={100}
              onCommit={(v) => update.mutate({ continue_limit: v })}
            />
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.detect_markers}
                disabled={!settings.media_tools?.probe_available}
                onChange={(e) =>
                  update.mutate({ detect_markers: e.target.checked })
                }
              />
              Detect where the credits start
            </label>
            <p className="set-row__sub">
              Looks at each film and episode in the background for where the
              credits begin, and at each season for where its intro is. That
              is what puts Skip intro and Skip credits on screen, and brings
              an episode&rsquo;s Up Next card in at its credits. Off by default
              because it reads a quarter of every file, which takes hours on a
              large library. Only files that have been read are examined.
            </p>
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.scrub_previews ?? false}
                disabled={!settings.media_tools?.probe_available}
                onChange={(e) => update.mutate({ scrub_previews: e.target.checked })}
              />
              Show a picture over the scrub bar
            </label>
            <p className="set-row__sub">
              Hover or drag along the bar while a film or episode plays and see the moment you would
              land on. Off by default because it reads every film and episode once in the background,
              about 40 seconds each, newest first, and keeps about 2 MB per film beside the database. A film you open that has none yet is made next, and its pictures appear within a minute or so.
              Turning it off stops the work and keeps what was made.
            </p>
          </>
        )}
      </section>
    </>
  );
}
