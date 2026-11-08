import { SubtitleStyleSettings } from "@/components/PlayerSettings";
import { LanguagePreferences } from "@/components/LanguagePreferences";

function LanguagesSection() {
  return (
    <section className="settings__section">
      <span className="section-label">Languages &amp; subtitles</span>
      <LanguagePreferences />
    </section>
  );
}

export function LanguagesPane() {
  return (
    <>
      <LanguagesSection />
      <SubtitleStyleSettings />
    </>
  );
}
