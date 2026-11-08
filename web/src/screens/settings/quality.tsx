import { useSettings, useUpdateSettings } from "@/api/hooks";

export function QualityPane() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  return (
    <section className="settings__section">
      <span className="section-label">Quality</span>
      {settings && (
        <>
          {settings.quality_rungs && settings.quality_rungs.length > 0 && (
            <>
              <label className="set-row">
                <span>Maximum streaming quality</span>
                <select
                  className="set-input"
                  value={settings.max_quality ?? ""}
                  disabled={update.isPending}
                  onChange={(e) =>
                    update.mutate({ max_quality: e.target.value })
                  }
                >
                  {settings.quality_rungs.map((q) => (
                    <option key={q.id} value={q.id}>
                      {q.label}
                    </option>
                  ))}
                </select>
              </label>
              <p className="set-row__sub">
                A limit on every stream this server sends, whatever a device
                asks for. It only ever lowers quality &mdash; a laptop set to
                480p still gets 480p here. Leave it at <em>No limit</em> for
                a server only reachable on your own network: anything lower
                is a re-encode that makes the picture worse and the machine
                hotter to solve a problem a gigabit link does not have.
                Lower it if this server is reachable from outside the house
                and you would rather cap what leaves it.
              </p>
            </>
          )}

        </>
      )}
    </section>
  );
}
