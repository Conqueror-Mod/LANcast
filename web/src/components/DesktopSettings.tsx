import { useEffect, useState } from "react";
import "./DesktopSettings.css";

// The desktop lifecycle section (docs/desktop-lifecycle-plan.md).
//
// These preferences belong to one window on one machine, so they never travel
// through /api/settings — that endpoint is machine-wide and shared, and one
// person's tray preference has no business changing everyone else's server. The
// native window binds host functions instead, and this section renders only
// when they exist.
//
// Feature detection rather than a flag from the server, because the server
// genuinely cannot answer it: the same server serves this window, a browser on
// the same machine, and a phone in the kitchen, and only one of those has a
// tray. A setting that appeared in a browser tab and silently governed a
// different process would be worse than no setting at all.

declare global {
  interface Window {
    lancastDesktopState?: () => Promise<DesktopState>;
    lancastDesktopSet?: (
      closeToTray: boolean,
      openAtLogin: boolean,
    ) => Promise<{ ok: boolean; error?: string }>;
  }
}

interface DesktopState {
  close_to_tray: boolean;
  open_at_login: boolean;
  // Whether this window started the server it is showing. Only the client
  // process knows: from the server's side, a window that launched it and a
  // window that attached to a running service look identical.
  owns_server: boolean;
  // Who holds the server when this window does not: the installed service, or
  // something else — another launch, or a build running in a terminal. Claiming
  // "service" for both would be an assertion about the machine that nothing
  // checked.
  holder: "self" | "service" | "other";
  error?: string;
}

export function DesktopSettings() {
  const [state, setState] = useState<DesktopState | null>(null);
  const supported = typeof window.lancastDesktopState === "function";

  useEffect(() => {
    if (!supported) return;
    window.lancastDesktopState!().then(setState).catch(() => setState(null));
  }, [supported]);

  // A browser tab has no tray to reduce to and no close button LANcast owns, so
  // there is nothing here to offer.
  if (!supported || !state) return null;

  return (
    <section className="settings__section">
      <span className="section-label">This computer</span>

      {/* The live, useful part: what closing this window actually does. Three
          things have been called "closing LANcast" and none of them was wrong;
          this is the one that says which one you are looking at. */}
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {state.owns_server
              ? "Closing this window stops the server"
              : "Closing this window leaves the server running"}
          </div>
          <div className="set-row__sub">{closingExplanation(state)}</div>
        </div>
        <div className="set-row__actions">
          <span
            className={
              "desktop-owner" + (state.owns_server ? " is-owned" : "")
            }
          >
            {holderLabel(state.holder)}
          </span>
        </div>
      </div>

      {state.error && (
        <p className="desktop-note desktop-note--warn">
          Your preferences could not be read: {state.error}
        </p>
      )}

      {/* Present but not yet live. Shown rather than hidden because the
          question "can LANcast do this?" deserves an answer, and disabled with
          a reason beats a tickbox that quietly does nothing. */}
      <LifecycleOption
        title="Close to tray"
        sub="Keep LANcast running in the notification area when you close the window."
        checked={state.close_to_tray}
        reason="Not yet available. Without a tray icon this would leave a server running with nothing on screen to show for it — the background process this is meant to prevent."
      />
      <LifecycleOption
        title="Open when Windows starts"
        sub="Start LANcast automatically when you sign in."
        checked={state.open_at_login}
        reason="Not yet available."
      />
    </section>
  );
}

function LifecycleOption({
  title,
  sub,
  checked,
  reason,
}: {
  title: string;
  sub: string;
  checked: boolean;
  reason: string;
}) {
  return (
    <div className="set-row desktop-opt">
      <div className="set-row__main">
        <label className="desktop-opt__label">
          <input type="checkbox" checked={checked} disabled readOnly />
          <span className="set-row__title">{title}</span>
        </label>
        <div className="set-row__sub">{sub}</div>
        <p className="desktop-note">{reason}</p>
      </div>
    </div>
  );
}

// The sentence under the heading. Each case names something the user can act
// on, which is the whole reason the holder is reported rather than assumed.
function closingExplanation(state: DesktopState): string {
  if (state.owns_server) {
    return "This window started the server, so it stops when you close it — nothing keeps running in the background.";
  }
  if (state.holder === "service") {
    return "The LANcast service owns this server, so it keeps running and anyone else watching is unaffected. Stop it from Windows Services.";
  }
  return "Another LANcast server was already running, so this window is only a view of it. Closing this window does not stop it — stop it where it was started.";
}

function holderLabel(holder: DesktopState["holder"]): string {
  switch (holder) {
    case "self":
      return "started here";
    case "service":
      return "service";
    default:
      return "already running";
  }
}
