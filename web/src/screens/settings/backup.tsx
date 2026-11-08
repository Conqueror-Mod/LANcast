import { useSettings, useUpdateSettings } from "@/api/hooks";
import { BackupSettings } from "@/components/BackupSettings";
import { RuleSelect } from "./shared";

export function BackupPane() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  return (
    <>
      {/* Backup first: what cannot be rebuilt leads, storage follows. */}
      <BackupSettings />
      {settings && (
        <>
          <section className="settings__section">
            <span className="section-label">Backup schedule</span>
            <RuleSelect
              title="Back up automatically"
              sub="On a timer, so the database is protected without anybody remembering to. Scheduled backups appear in the list above, marked as scheduled."
              value={settings.backup_every_days ?? 7}
              options={[
                { value: 0, label: "Never" },
                { value: 1, label: "Every day" },
                { value: 7, label: "Every week" },
                { value: 14, label: "Every two weeks" },
                { value: 30, label: "Every month" },
              ]}
              onChange={(v) => update.mutate({ backup_every_days: v })}
            />
            <RuleSelect
              title="Scheduled backups to keep"
              sub="Older scheduled backups past this number are deleted after each new one. A backup you take yourself is never deleted by the schedule."
              value={settings.backup_keep ?? 4}
              disabled={(settings.backup_every_days ?? 7) === 0}
              options={[1, 2, 3, 4, 6, 8, 12].map((n) => ({ value: n, label: String(n) }))}
              onChange={(v) => update.mutate({ backup_keep: v })}
            />
          </section>
          <section className="settings__section">
            <span className="section-label">Storage</span>
            <RuleSelect
              title="Artwork cache limit"
              sub="Posters and stills LANcast has downloaded. Artwork nothing uses any more is always cleared out, whatever this says. A limit additionally drops resized copies, which are rebuilt from the originals when they are next needed — the originals themselves are never deleted, so a limit below what your library actually uses simply will not be met."
              value={settings.artwork_cache_mb ?? 0}
              options={[
                { value: 0, label: "No limit" },
                { value: 512, label: "512 MB" },
                { value: 1024, label: "1 GB" },
                { value: 2048, label: "2 GB" },
                { value: 5120, label: "5 GB" },
                { value: 10240, label: "10 GB" },
              ]}
              onChange={(v) => update.mutate({ artwork_cache_mb: v })}
            />
          </section>
        </>
      )}
    </>
  );
}
