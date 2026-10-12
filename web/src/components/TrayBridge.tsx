import { useEffect, useRef } from "react";
import { useActivity, useCancelScans } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";

declare global {
  interface Window {
    // Bound by the desktop window (ADR 0080): the page tells the tray whether
    // a scan is running, so its Cancel item is greyed except during one.
    lancastTrayScanning?: (on: boolean) => void;
    // Called by the tray, with a fixed action name. The page owns what each
    // name means and does it as the signed-in person.
    __lancastTray?: (action: string) => void;
  }
}

/*
 * The page's half of the desktop tray (ADR 0080 §3).
 *
 * The tray holds no session, so it cannot ask the server anything. It learns
 * whether a scan is running from here — read off the Activity data the shell
 * already polls, so it costs no request of its own — and asks this page to
 * cancel, which the page does as the signed-in administrator. A member never
 * sees scans in Activity, so their Cancel stays greyed, which is right: they
 * could not cancel one.
 *
 * Renders nothing, and does nothing outside the desktop window: a browser
 * tab has no tray to talk to.
 */
export function TrayBridge() {
  const report = window.lancastTrayScanning;
  const { data } = useActivity();
  const cancel = useCancelScans();
  const scanning = (data?.tasks ?? []).some(
    (t) => t.kind === "scan" && t.state === "running",
  );

  useEffect(() => {
    if (!report) return;
    report(scanning);
  }, [report, scanning]);

  // Leaving the signed-in app (signing out) must not leave Cancel enabled
  // for a page that can no longer cancel anything.
  useEffect(() => {
    if (!report) return;
    return () => report(false);
  }, [report]);

  /*
   * ✕ hides the window, and a film does not play on in a hidden one.
   *
   * Reported as the window "not closing" while something played: the
   * window went, and the film carried on, heard and not seen, with no
   * window to stop it from. Stopping saves the place, so Continue picks
   * it up. Music is different — listening with the window away is the
   * point of a tray — and keeps playing.
   */
  const pb = usePlayback();
  const playingRef = useRef(pb);
  playingRef.current = pb;

  const mutate = cancel.mutate;
  useEffect(() => {
    if (!report) return;
    window.__lancastTray = (action: string) => {
      if (action === "cancel-scan") mutate();
      if (action === "window-hidden") {
        const now = playingRef.current;
        if (now.itemID && !now.isAudio) now.stop();
      }
    };
    return () => {
      delete window.__lancastTray;
    };
  }, [report, mutate]);

  return null;
}
