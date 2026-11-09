import { useState } from "react";
import { ProviderKeys } from "@/components/ProviderKeys";
import { LanguageRefreshRow } from "@/components/LanguageRefreshRow";
import { useLibraries, useSettings, useUpdateSettings } from "@/api/hooks";

/*
 * Two settings the server has always accepted and validated, and that no
 * control has ever reached: the provider rate limit and the update check. Both
 * were editable only by hand-editing config.json — which is the same
 * "capability with no path to it" that hid track deletion, arrived at from the
 * configuration side rather than the UI side.
 *
 * Separate sections rather than surgery inside AdminSections. They re-read the
 * same queries, which react-query serves from one cache entry, so the cost of
 * keeping the edit contained is nothing.
 */
function RateLimitSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  if (!settings) return null;
  return (
    <section className="settings__section">
      <span className="section-label">Provider requests</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Rate limit</div>
          <div className="set-row__sub">
            Requests per second to TMDB, OMDb and OpenSubtitles. Lower this if a
            provider starts refusing; the server rejects anything above 50.
          </div>
        </div>
        <div className="set-row__actions">
          <input
            className="set-input set-input--num"
            type="number"
            min={1}
            max={50}
            step={1}
            defaultValue={settings.rate_per_sec}
            aria-label="Provider requests per second"
            // On blur rather than on change: this is a number field, and
            // committing every keystroke would PATCH "1" on the way to "12"
            // and briefly throttle the server to a crawl.
            onBlur={(e) => {
              const v = Number(e.target.value);
              if (v >= 1 && v <= 50 && v !== settings.rate_per_sec) {
                update.mutate({ rate_per_sec: v });
              } else {
                e.target.value = String(settings.rate_per_sec);
              }
            }}
          />
        </div>
      </div>
    </section>
  );
}

/*
 * The refresh a language change offers, and never starts (ADR 0122).
 *
 * Shown once the new language is saved. It is its own confirmation: the counts
 * are on screen before anything is asked for, and "Not now" puts it away
 * without a word to the server.
 */
function LanguageRefreshOffer({
  language,
  dismiss,
}: {
  language: string;
  dismiss: () => void;
}) {
  const { data: libraries } = useLibraries();
  const eligible = (libraries ?? []).filter(
    (l) => l.kind === "movie" || l.kind === "show",
  );
  return (
    <div className="set-lang-offer" role="group" aria-label="Refresh in the new language">
      <p className="set-row__sub">
        Titles and descriptions already stored are still in the old language.
        Fetch them again in {language}? Nothing is searched again, matches stay
        as they are, and anything you edited by hand is left alone.
      </p>
      {eligible.length === 0 ? (
        <p className="set-row__sub">No film or show libraries to refresh.</p>
      ) : (
        eligible.map((l) => <LanguageRefreshRow key={l.id} library={l} />)
      )}
      <button className="set-btn" onClick={dismiss}>
        Not now
      </button>
    </div>
  );
}

export function MetadataPane() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  // The language just chosen, while its refresh is on offer.
  const [offered, setOffered] = useState<string | null>(null);
  const languages = settings?.metadata_languages ?? [];
  const languageName = (code: string) =>
    languages.find((l) => l.code === code)?.name ?? code;
  return (
    <>
      <section className="settings__section">
        <span className="section-label">Metadata</span>
        {settings && (
          <>
            <ProviderKeys
              settings={settings}
              pending={update.isPending}
              save={(patch) => update.mutate(patch)}
            />
            {/*
              Which country's certificate to store.

              The list comes from the server rather than from a constant
              here, and that is not ceremony: which countries can be offered
              is a fact about the server's rating ladder. A certificate the
              ladder cannot place reads as unrated, and an account ceiling
              blocks unrated — so a client with its own list would eventually
              offer a country that empties a child's library.
            */}
            {settings.certification_countries &&
              settings.certification_countries.length > 0 && (
                <>
                  <label className="set-row">
                    <span>Age rating country</span>
                    <select
                      className="set-input"
                      value={settings.certification_country ?? ""}
                      disabled={update.isPending}
                      onChange={(e) =>
                        update.mutate({
                          certification_country: e.target.value,
                        })
                      }
                    >
                      <option value="">
                        United States, then United Kingdom
                      </option>
                      {settings.certification_countries.map((c) => (
                        <option key={c.code} value={c.code}>
                          {c.name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <p className="set-row__sub">
                    Which country&rsquo;s age rating to show — PG-13, 15, FSK
                    16 — on films and shows. The chosen country is preferred;
                    anything it has no rating for still falls back to the
                    American or British one, so picking a country never leaves
                    a title unrated. Only countries whose ratings LANcast can
                    compare are offered, because an account with a rating
                    limit hides anything it cannot place. This applies to
                    metadata fetched from now on — run{" "}
                    <em>Refresh metadata</em> on a library to change what is
                    already stored.
                  </p>
                </>
              )}
            {/*
              The metadata language (ADR 0122). Served by the server like the
              countries above, so the client never offers a tag the server
              would refuse.
            */}
            {languages.length > 0 && (
              <>
                <label className="set-row">
                  <span>Metadata language</span>
                  <select
                    className="set-input"
                    aria-label="Metadata language"
                    value={settings.metadata_language ?? "en-US"}
                    disabled={update.isPending}
                    onChange={(e) => {
                      const code = e.target.value;
                      update.mutate(
                        { metadata_language: code },
                        { onSuccess: () => setOffered(code) },
                      );
                    }}
                  >
                    {languages.map((l) => (
                      <option key={l.code} value={l.code}>
                        {l.name === l.native ? l.name : `${l.name} — ${l.native}`}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="set-row__sub">
                  The language film and show titles, descriptions and taglines
                  are fetched in from TMDB. Where TMDB has no translation the
                  English text is used, and files named in English still match.
                  Trailers in this language are preferred, with English ones
                  as well.
                </p>
                {offered && (
                  <LanguageRefreshOffer
                    language={languageName(offered)}
                    dismiss={() => setOffered(null)}
                  />
                )}
              </>
            )}
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.auto_enrich}
                onChange={(e) =>
                  update.mutate({ auto_enrich: e.target.checked })
                }
              />
              Automatically fetch metadata after a scan
            </label>
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.write_nfo}
                onChange={(e) =>
                  update.mutate({ write_nfo: e.target.checked })
                }
              />
              Write NFO sidecar files next to media
            </label>
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.match_music}
                onChange={(e) =>
                  update.mutate({ match_music: e.target.checked })
                }
              />
              Identify albums with MusicBrainz
            </label>
            <p className="set-row__sub">
              Each album is matched to its record in MusicBrainz, which adds
              what kind of record it is and fills a missing year, genre or
              cover. Your tags always win. Album titles and artist names are
              sent to musicbrainz.org, about one a second; the first pass
              over a large library takes a couple of hours. Genres from
              MusicBrainz come from its users and are shown with credit.
            </p>
          </>
        )}
      </section>
      <RateLimitSection />
    </>
  );
}
