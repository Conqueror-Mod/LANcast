import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  LifecycleOption,
  saveDesktopPrefs,
  type DesktopState,
} from "./DesktopSettings";
import { GAMES_ENABLED_KEY } from "@/lib/games";
import "./DesktopSettings.css";

/*
 * The Games pane (ADR 0066).
 *
 * It has a pane of its own for one reason, and it is not tidiness: the switch
 * lived as the fourth option inside "This app", and the first person to go
 * looking for games in LANcast did not find it and reported the feature as
 * shipped with nothing to show. Off by default is a decision worth keeping —
 * a media server should not start listing somebody's games because it found
 * Steam — but off *and* unfindable is indistinguishable from missing.
 *
 * So the word "Games" now appears in the settings list, which is where somebody
 * hunting for it actually looks. It is also where the later options belong:
 * forgetting the remembered displays, and a second launcher when one arrives.
 *
 * Writing goes through saveDesktopPrefs, which re-reads before it writes, so
 * this pane can change one preference without holding a copy of the other
 * three — see the note there.
 */
export function GamesSettings() {
  const [state, setState] = useState<DesktopState | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const supported = typeof window.lancastDesktopState === "function";
  const qc = useQueryClient();

  useEffect(() => {
    if (!supported) return;
    window
      .lancastDesktopState!()
      .then(setState)
      .catch(() => setState(null));
  }, [supported]);

  // A browser tab has no games to list and could not start one if it had.
  if (!supported || !state) return null;

  const save = async (next: boolean) => {
    setSaving(true);
    setSaveError("");
    try {
      const res = await saveDesktopPrefs({ games: next });
      if (!res.ok) setSaveError(res.error ?? "could not be saved");
    } catch (e) {
      setSaveError(String(e));
    } finally {
      setSaving(false);
      const fresh = await window.lancastDesktopState!().catch(() => null);
      if (fresh) setState(fresh);
      /*
       * The rail is looking at this too.
       *
       * Turning games on here has to make the tab appear and turning it off has
       * to take it away. Without this the setting is right, the file on disk is
       * right, and only the picture is stale — the quietest bug this project
       * has, and the one it has shipped four times.
       */
      qc.invalidateQueries({ queryKey: GAMES_ENABLED_KEY });
    }
  };

  return (
    <section className="settings__section">
      <span className="section-label">Games</span>

      <p className="desktop-note">
        LANcast can list the games Steam has installed on this computer and start
        them. It reads Steam's own files on this disk — there is no sign-in, and
        nothing is sent anywhere.
      </p>

      <LifecycleOption
        title="Show my installed games"
        sub="List the games Steam has installed on this computer, in a Games tab. LANcast starts them; it does not stream them."
        checked={state.games}
        onChange={save}
        busy={saving}
        error={saveError}
      />

      <p className="desktop-note">
        This is a setting for this computer, and the tab appears only in the
        LANcast desktop app: the games are installed here, so a phone or a
        browser tab could not start one. Switching it on adds{" "}
        <strong>Games</strong> to the rail on the left straight away.
      </p>
    </section>
  );
}
