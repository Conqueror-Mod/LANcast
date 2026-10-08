import { useEffect, useState } from "react";
import { PLATFORMS } from "@/lib/platforms";
import "./RetroCoreSettings.css";
import { retroAvailability, retroSupported, type RetroAvailability } from "@/playback/retro";

/*
 * Which emulator core plays each console on this machine (ADR 0073).
 *
 * A fact about this computer, so it lives in the desktop client and this
 * section appears only there. Each console shows the default core and whether
 * it is ready; a person can point a console at a core .dll already on this
 * machine — the ADR's advanced option, and the way to play before LANcast can
 * fetch a pinned build of each core itself.
 */
export function RetroCoreSettings() {
  const [paths, setPaths] = useState<Record<string, string>>({});
  const [status, setStatus] = useState<Record<string, RetroAvailability>>({});
  const [editing, setEditing] = useState<Record<string, string>>({});
  const [error, setError] = useState<Record<string, string>>({});

  const refresh = async () => {
    const chosen = (await window.lancastRetroCores?.().catch(() => ({}))) ?? {};
    setPaths(chosen);
    const next: Record<string, RetroAvailability> = {};
    for (const p of PLATFORMS) next[p.key] = await retroAvailability(p.key);
    setStatus(next);
  };

  useEffect(() => {
    if (retroSupported()) void refresh();
  }, []);

  if (!retroSupported()) return null;

  const save = async (platform: string) => {
    try {
      await window.lancastRetroSetCore?.(platform, (editing[platform] ?? "").trim());
      setError((e) => ({ ...e, [platform]: "" }));
      setEditing((e) => {
        const n = { ...e };
        delete n[platform];
        return n;
      });
      await refresh();
    } catch (err) {
      setError((e) => ({ ...e, [platform]: String((err as Error)?.message ?? err) }));
    }
  };

  return (
    <section className="settings__section">
      <span className="section-label">Retro games</span>
      <p className="set-row__sub">
        Each console plays through an emulator core on this computer. To use one you already
        have, paste the full path of its <code>.dll</code>. Leave it empty to use LANcast&apos;s
        default.
      </p>
      {PLATFORMS.map((p) => {
        const st = status[p.key];
        const value = editing[p.key] ?? paths[p.key] ?? "";
        return (
          <div className="set-row" key={p.key}>
            <div className="set-row__main">
              <div className="set-row__title">{p.label}</div>
              <div className="set-row__sub">
                {st?.core ? `${st.core} (${st.licence}) — ` : ""}
                {st ? (st.available ? "ready" : st.reason) : "checking…"}
              </div>
              {!st?.needs_gl && (
                <div className="set-row__inline">
                  <input
                    type="text"
                    className="set-input"
                    aria-label={`Core for ${p.label}`}
                    placeholder="C:\\path\\to\\core_libretro.dll"
                    value={value}
                    onChange={(e) => setEditing((m) => ({ ...m, [p.key]: e.target.value }))}
                  />
                  <button
                    className="set-btn"
                    disabled={editing[p.key] === undefined}
                    onClick={() => void save(p.key)}
                  >
                    Save
                  </button>
                </div>
              )}
              {error[p.key] && <div className="set-row__sub set-row__sub--warn">{error[p.key]}</div>}
            </div>
          </div>
        );
      })}
    </section>
  );
}
