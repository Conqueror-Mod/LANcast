import { useCallback, useEffect, useRef, useState } from "react";
import { formatBytes } from "@/lib/format";
import { usePlaybackIfAny } from "@/playback/PlaybackProvider";
import "./DesktopSettings.css";

/*
 * How the server runs: as a Windows service, or only while the app is open
 * (ADR 0092).
 *
 * Switching is a move of the whole library between two folders, so it is not
 * a preference the page writes: the window does it, through bindings only the
 * desktop app on the server's own machine has. A browser tab, a phone, and a
 * window pointed at another computer see nothing here — the destination is
 * this person's folder on this machine, and the consent prompt can only be
 * shown here.
 *
 * Administrators only, as a matter of what is shown. The boundary that
 * protects the library is Windows' own: the work is done by an elevated
 * helper, and nobody gets past the prompt without an administrator's rights.
 */

declare global {
  interface Window {
    lancastServerMode?: () => Promise<ServerModeState>;
    lancastSwitchServerMode?: (
      to: "service" | "app",
    ) => Promise<{ ok: boolean; error?: string }>;
    lancastServerModeProgress?: () => Promise<ServerModeProgress>;
  }
}

export interface ServerModeState {
  mode: "service" | "app";
  // A service registration with no library in its folder: the tail of an
  // interrupted switch, finished by switching to the app.
  leftover_service: boolean;
  addr: string;
  // Why this window cannot switch, when it cannot.
  blocked?: string;
  switching: boolean;
}

export interface ServerModeResult {
  to: "service" | "app";
  library: string;
  moved_aside?: string;
  old_folder?: string;
  old_bytes?: number;
  superseded?: string;
  warning?: string;
}

export interface ServerModeProgress {
  running: boolean;
  // The window's own part: asking permission and waiting, or bringing the
  // right server up afterwards.
  stage: "" | "elevating" | "starting";
  progress?: { phase: string; done_bytes?: number; total_bytes?: number };
  outcome?: {
    ok: boolean;
    to: "service" | "app";
    error?: string;
    server_error?: string;
    result?: ServerModeResult;
  };
}

const POLL_MS = 500;

export function ServerModeSettings({ isAdmin }: { isAdmin: boolean }) {
  const [state, setState] = useState<ServerModeState | null>(null);
  const [progress, setProgress] = useState<ServerModeProgress | null>(null);
  const [confirming, setConfirming] = useState<"service" | "app" | null>(null);
  const [startError, setStartError] = useState("");
  // Set when this page watched a switch run. Only then does its end reload
  // the page: a page that opened afterwards is already talking to the new
  // server, and reloading it would loop.
  const watched = useRef(false);
  const playback = usePlaybackIfAny();
  const supported =
    isAdmin &&
    typeof window.lancastServerMode === "function" &&
    typeof window.lancastServerModeProgress === "function";

  const poll = useCallback(async () => {
    const p = await window.lancastServerModeProgress!().catch(() => null);
    if (!p) return;
    setProgress(p);
    if (p.running) {
      watched.current = true;
      return;
    }
    if (watched.current) {
      watched.current = false;
      // Everything else on the page was read from the server that has just
      // gone; the outcome survives the reload because the window holds it.
      window.location.reload();
      return;
    }
    const fresh = await window.lancastServerMode!().catch(() => null);
    if (fresh) setState(fresh);
  }, []);

  useEffect(() => {
    if (!supported) return;
    window.lancastServerMode!().then(setState).catch(() => setState(null));
    void poll();
  }, [supported, poll]);

  const running = !!progress?.running;
  useEffect(() => {
    if (!running) return;
    const t = window.setInterval(() => void poll(), POLL_MS);
    return () => window.clearInterval(t);
  }, [running, poll]);

  if (!supported || !state) return null;

  const start = async (to: "service" | "app") => {
    setConfirming(null);
    setStartError("");
    // The server goes away for the whole copy; stopping playback first ends
    // it cleanly rather than in a player nothing can control.
    playback?.stop();
    const res = await window.lancastSwitchServerMode!(to).catch((e) => ({
      ok: false,
      error: String(e),
    }));
    if (!res.ok) {
      setStartError(res.error ?? "the switch could not start");
      return;
    }
    watched.current = true;
    setProgress({ running: true, stage: "elevating" });
  };

  const isService = state.mode === "service";
  const outcome = !running ? progress?.outcome : undefined;

  return (
    <section className="settings__section">
      <span className="section-label">How the server runs</span>

      <div className={"set-row desktop-opt" + (state.blocked || running ? "" : " is-live")}>
        <div className="set-row__main">
          <label className="desktop-opt__label">
            <input
              type="checkbox"
              checked={isService}
              disabled={!!state.blocked || running}
              onChange={(e) => setConfirming(e.target.checked ? "service" : "app")}
            />
            <span className="set-row__title">Run as a Windows service</span>
          </label>
          <div className="set-row__sub">
            {isService
              ? "The server starts with Windows and keeps your libraries available to your other devices, whether or not anyone is signed in."
              : "The server runs only while LANcast is open on this computer. Turn this on to keep your libraries available whenever the computer is on."}
          </div>
          {state.blocked && <p className="desktop-note">{state.blocked}</p>}
        </div>
      </div>

      {state.leftover_service && !running && (
        <div className="set-row" role="status">
          <div className="set-row__main">
            <div className="set-row__title">A service is still registered</div>
            <div className="set-row__sub">
              An earlier switch moved your library but didn&rsquo;t finish removing the service. It
              won&rsquo;t start at boot, and it holds no library.
            </div>
          </div>
          <button className="set-btn" type="button" onClick={() => void start("app")}>
            Remove it
          </button>
        </div>
      )}

      {confirming && (
        <div className="set-row" role="alertdialog" aria-label="Change how the server runs">
          <div className="set-row__main">
            <div className="set-row__title">
              {confirming === "service" ? "Run LANcast as a service?" : "Stop running LANcast as a service?"}
            </div>
            <div className="set-row__sub">{confirmText(confirming, state.addr)}</div>
            <div className="set-row">
              <button className="set-btn" type="button" onClick={() => void start(confirming)}>
                {confirming === "service" ? "Switch to a service" : "Switch to the app"}
              </button>
              <button className="set-btn" type="button" onClick={() => setConfirming(null)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {running && progress && <SwitchProgress progress={progress} />}
      {startError && <p className="set-error" role="alert">{startError}</p>}
      {outcome && <SwitchOutcome outcome={outcome} />}
    </section>
  );
}

/*
 * What the confirmation says. Every sentence is something the person would
 * otherwise discover afterwards: the interruption, the prompt, where the
 * library ends up, and that the old copy is kept rather than removed.
 */
export function confirmText(to: "service" | "app", addr: string): string {
  const common =
    "LANcast stops, copies your library and checks the copy opens, then starts again. " +
    "Anyone watching is interrupted, and with a large library this takes a few minutes. " +
    "Windows asks for permission. Your current copy is kept until you remove it.";
  if (to === "service") {
    return (
      common +
      " The library moves to the computer-wide folder, and the server keeps running after you close the app."
    );
  }
  const port = addr.slice(addr.lastIndexOf(":") + 1);
  const portNote =
    port && port !== "8080"
      ? ` The server keeps port ${port} until LANcast is next started, then goes back to port 8080.`
      : "";
  return (
    common +
    " The library moves to your own user folder, and the server stops when you exit LANcast. " +
    "Turn on Start LANcast at login if you want it there whenever you're signed in." +
    portNote
  );
}

const PHASES: Record<string, string> = {
  stopping: "Stopping the server",
  copying: "Copying your library",
  verifying: "Checking the copy opens",
  installing: "Installing the service",
  starting: "Starting the service",
  finishing: "Finishing",
};

export function SwitchProgress({ progress }: { progress: ServerModeProgress }) {
  const p = progress.progress;
  let title = "Waiting for permission";
  if (progress.stage === "starting") title = "Starting the server";
  else if (p?.phase) title = PHASES[p.phase] ?? p.phase;
  const fraction =
    p?.phase === "copying" && p.total_bytes ? Math.min(1, (p.done_bytes ?? 0) / p.total_bytes) : null;
  return (
    <div className="set-row" role="status">
      <div className="set-row__main">
        <div className="set-row__title">{title}&hellip;</div>
        {fraction !== null && p && (
          <>
            <div className="set-row__sub">
              {formatBytes(p.done_bytes ?? 0)} of {formatBytes(p.total_bytes ?? 0)}
            </div>
            <div className="set-conv__bar" aria-hidden="true">
              <span style={{ width: `${Math.round(fraction * 100)}%` }} />
            </div>
          </>
        )}
        <div className="set-row__sub">Keep this window open. The page reloads when the server is back.</div>
      </div>
    </div>
  );
}

export function SwitchOutcome({ outcome }: { outcome: NonNullable<ServerModeProgress["outcome"]> }) {
  if (!outcome.ok) {
    return (
      <div className="set-row" role="alert">
        <div className="set-row__main">
          <div className="set-row__title">LANcast runs the way it did before</div>
          <div className="set-row__sub">{outcome.error}</div>
          {outcome.server_error && <p className="desktop-note desktop-note--warn">{outcome.server_error}</p>}
        </div>
      </div>
    );
  }
  const r = outcome.result;
  return (
    <div className="set-row" role="status">
      <div className="set-row__main">
        <div className="set-row__title">
          {outcome.to === "service" ? "LANcast now runs as a service" : "LANcast now runs only while the app is open"}
        </div>
        {r?.old_folder && (
          <div className="set-row__sub">
            Your old copy is still in {r.old_folder}
            {r.old_bytes ? ` (about ${formatBytes(r.old_bytes)})` : ""}. Remove it once you&rsquo;re happy
            everything is here.
          </div>
        )}
        {r?.superseded && (
          <div className="set-row__sub">
            An older library that was already in {r.library} was kept as {basename(r.superseded)}.
          </div>
        )}
        {r?.warning && <p className="desktop-note desktop-note--warn">{r.warning}</p>}
        {outcome.server_error && <p className="desktop-note desktop-note--warn">{outcome.server_error}</p>}
      </div>
    </div>
  );
}

function basename(p: string): string {
  return p.slice(Math.max(p.lastIndexOf("\\"), p.lastIndexOf("/")) + 1);
}
