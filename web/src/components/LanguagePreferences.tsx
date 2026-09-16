import { useEffect, useState } from "react";
import { useLanguagePreferences, useSetLanguagePreferences } from "@/api/hooks";

/*
 * Which language this account wants to hear, and when it wants subtitles.
 *
 * On the Account pane rather than Playback, and that is the whole placement
 * decision: everything on Playback is a fact about *this screen* — how big the
 * subtitles are at this viewing distance, how much bandwidth there is to it.
 * This is a fact about the person, and it follows them to the next screen they
 * sit at.
 */

/*
 * The languages offered.
 *
 * A short list rather than every ISO 639 code, for the reason the certification
 * country list is short: a picker of nine hundred entries is not a picker. The
 * server accepts any well-formed code, so nothing here is a limit on what can
 * be stored — only on what this control offers, and the list is the languages a
 * domestic library actually carries tracks in.
 */
const LANGUAGES: { code: string; name: string }[] = [
  { code: "en", name: "English" },
  { code: "es", name: "Spanish" },
  { code: "fr", name: "French" },
  { code: "de", name: "German" },
  { code: "it", name: "Italian" },
  { code: "pt", name: "Portuguese" },
  { code: "nl", name: "Dutch" },
  { code: "ja", name: "Japanese" },
  { code: "ko", name: "Korean" },
  { code: "zh", name: "Chinese" },
  { code: "ru", name: "Russian" },
];

export function LanguagePreferences() {
  const { data } = useLanguagePreferences();
  const save = useSetLanguagePreferences();

  /*
   * Held locally so the three controls move together.
   *
   * The server takes all three at once — a subtitle language with no mode is
   * silently inert — so the page cannot send one field as it changes. It sends
   * the whole answer.
   */
  const [audio, setAudio] = useState("");
  const [subtitle, setSubtitle] = useState("");
  const [mode, setMode] = useState("off");

  useEffect(() => {
    if (!data) return;
    setAudio(data.preferred_audio_lang ?? "");
    setSubtitle(data.preferred_subtitle_lang ?? "");
    setMode(data.subtitle_mode ?? "off");
  }, [data]);

  const commit = (next: {
    audio?: string;
    subtitle?: string;
    mode?: string;
  }) => {
    const body = {
      preferred_audio_lang: next.audio ?? audio,
      preferred_subtitle_lang: next.subtitle ?? subtitle,
      subtitle_mode: next.mode ?? mode,
    };
    save.mutate(body);
  };

  return (
    <>
      <span className="set-sublabel">Language</span>

      <label className="set-row">
        <span>Preferred audio</span>
        <select
          className="set-input"
          value={audio}
          disabled={save.isPending}
          onChange={(e) => {
            setAudio(e.target.value);
            commit({ audio: e.target.value });
          }}
        >
          <option value="">No preference</option>
          {LANGUAGES.map((l) => (
            <option key={l.code} value={l.code}>
              {l.name}
            </option>
          ))}
        </select>
      </label>
      <p className="set-row__sub">
        Films and episodes start on this language where they carry a track in
        it. Anything that does not plays whatever the file leads with, rather
        than an arbitrary track &mdash; so setting this never makes a title play
        in a language you did not ask for.
      </p>

      <label className="set-row">
        <span>Subtitles</span>
        <select
          className="set-input"
          value={mode}
          disabled={save.isPending}
          onChange={(e) => {
            setMode(e.target.value);
            commit({ mode: e.target.value });
          }}
        >
          <option value="off">Off</option>
          <option value="foreign">
            When the audio is not in my language
          </option>
          <option value="always">Always</option>
        </select>
      </label>

      {mode !== "off" && (
        <>
          <label className="set-row">
            <span>Subtitle language</span>
            <select
              className="set-input"
              value={subtitle}
              disabled={save.isPending}
              onChange={(e) => {
                setSubtitle(e.target.value);
                commit({ subtitle: e.target.value });
              }}
            >
              <option value="">No preference</option>
              {LANGUAGES.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.name}
                </option>
              ))}
            </select>
          </label>
          <p className="set-row__sub">
            {mode === "foreign"
              ? /* Judged on what plays, not on what the file is. Worth saying,
                   because the other reading — "this is a foreign film" — is the
                   one people expect and the one that puts subtitles over an
                   English dub they chose deliberately. */
                "Shown when the audio that ends up playing is not in your preferred language. A foreign film whose English track was chosen gets none."
              : "Shown whenever a track exists in this language."}
          </p>
        </>
      )}

      {save.isError && (
        <p className="set-error">{String(save.error?.message)}</p>
      )}
    </>
  );
}
