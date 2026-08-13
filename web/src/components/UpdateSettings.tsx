import {
  useUpdateStatus,
  useCheckForUpdate,
  useDownloadUpdate,
  useRestartForUpdate,
  useSettings,
  useUpdateSettings,
} from "@/api/hooks";
import { useEffect, useRef, useState } from "react";
import { useHealth } from "@/api/hooks";
import "./UpdateSettings.css";

// downloadLabel shows how far along a download is when the server says, and
// falls back to the plain word when it does not.
//
// A percentage matters here more than it looks: the download is ~16MB over a
// link nobody chose, and a button that says only "Downloading…" for a minute is
// indistinguishable from one that is stuck — which is exactly how this was
// reported.
function downloadLabel(status: { downloading?: { done: number; total: number } }): string {
  const p = status.downloading;
  if (!p || !p.total) return "Downloading…";
  const pct = Math.min(100, Math.round((p.done / p.total) * 100));
  return `Downloading ${pct}%`;
}

// Updates, in Settings. The activity indicator carries the same fact when an
// update is waiting; this is the place to ask on purpose, and the place that
// explains why automatic installation is or is not available.
export function UpdateSettings() {
  // Set the moment a download is accepted and cleared when the server reports
  // something staged (or a failure). Local, because the server has no idea a
  // *this client* is waiting on it, and because the first status fetch after
  // the POST can still arrive before the download has registered as active.
  const [downloading, setDownloading] = useState(false);
  const { data: status } = useUpdateStatus(true, downloading);
  const { data: settings } = useSettings();
  const check = useCheckForUpdate();
  const download = useDownloadUpdate();
  const restart = useRestartForUpdate();
  const save = useUpdateSettings();

  /*
   * The four states this panel used to collapse into one.
   *
   * "Downloaded and verified. Restart to finish." was the last thing anybody
   * was told. What happened next — whether the swap worked, whether the server
   * came back, what version it came back as — was left to the user to discover
   * by starting the server and reading the version, which is the complaint this
   * addresses. The application knows; it should say.
   *
   * `installing` is held in the component rather than read from the server for
   * the obvious reason: during it, there is no server to ask.
   */
  const [installing, setInstalling] = useState(false);
  const [installedTo, setInstalledTo] = useState<string | null>(null);
  // The version that was staged when the restart began, so the confirmation can
  // name it after the server comes back as that version.
  const target = useRef<string>("");

  // Polled only while installing: the server is expected to stop answering and
  // then answer again, which is the one time a failing health check is the
  // normal course of events rather than something to report.
  const { data: health } = useHealth(installing);

  // The download is over when there is something staged, or when it failed.
  // Both are the server's word rather than a timer.
  useEffect(() => {
    if (!downloading) return;
    if (status?.staged || status?.download_error) setDownloading(false);
  }, [downloading, status?.staged, status?.download_error]);

  useEffect(() => {
    if (!installing || !health?.version) return;
    if (target.current && health.version === target.current) {
      setInstalling(false);
      setInstalledTo(health.version);
    }
  }, [installing, health?.version]);

  if (!status?.supported) return null;

  const enabled = settings?.update_check ?? true;

  return (
    <section className="settings__section">
      <span className="section-label">Updates</span>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {installing ? (
              <>
                Installing LANcast {target.current}
                <span className="upd-badge">restarting</span>
              </>
            ) : installedTo ? (
              <>
                Updated to LANcast {installedTo}
                <span className="upd-badge upd-badge--done">done</span>
              </>
            ) : status.staged ? (
              <>
                LANcast {status.staged} is ready to install
                <span className="upd-badge">restart</span>
              </>
            ) : status.available ? (
              <>
                LANcast {status.latest} is available
                <span className="upd-badge">new</span>
              </>
            ) : (
              <>You are up to date</>
            )}
          </div>
          <div className="set-row__sub">
            {installing
              ? "LANcast is restarting to finish the update. This takes a few seconds — the server will come back on its own."
              : installedTo
                ? "The server restarted and is running the new version."
                : status.staged
                  ? "Downloaded and verified. Installing restarts the server: playback stops for a few seconds and LANcast starts itself again."
                  : describe(status)}
          </div>
          {status.error && (
            <p className="upd-note upd-note--warn">
              The last check failed: {status.error}
            </p>
          )}
          {/*
            A failed download used to be visible only in the server log, so the
            panel kept saying "Downloading…" and stopped meaning anything. It
            reads separately from a failed check because the two ask different
            things of the reader: a check that fails is worth retrying later, a
            download that fails is worth reading.
          */}
          {status.download_error && (
            <p className="upd-note upd-note--warn">
              The last download failed: {status.download_error}
            </p>
          )}
        </div>
        <div className="set-row__actions">
          {status.available && status.url && (
            <a
              className="set-btn"
              href={status.url}
              target="_blank"
              rel="noreferrer noopener"
            >
              Release notes
            </a>
          )}
          {status.staged && (
            <button
              className="set-btn"
              disabled={restart.isPending || installing}
              onClick={() => {
                target.current = status.staged ?? "";
                restart.mutate(undefined, {
                  // Only once the server has accepted it: a failed request
                  // leaves a server that is still running and still staged, and
                  // showing "Installing…" over it would be the same lie in a
                  // new place.
                  onSuccess: () => setInstalling(true),
                });
              }}
            >
              {installing ? "Installing…" : restart.isPending ? "Starting…" : "Install and restart"}
            </button>
          )}
          {status.available && !status.staged && status.can_verify && (
            <button
              className="set-btn"
              disabled={download.isPending || downloading || status.downloading?.active}
              onClick={() =>
                download.mutate(undefined, {
                  // Watch from the moment the server accepts it. The POST
                  // returns immediately and downloads in the background, so
                  // without this the panel has nothing telling it to look
                  // again — which is how it sat on "Downloading…" while the
                  // activity indicator already said the update was ready.
                  onSuccess: () => setDownloading(true),
                })
              }
            >
              {downloading || status.downloading?.active
                ? downloadLabel(status)
                : "Download and install"}
            </button>
          )}
          <button
            className="set-btn"
            disabled={check.isPending || status.checking}
            onClick={() => check.mutate()}
          >
            {check.isPending || status.checking ? "Checking…" : "Check now"}
          </button>
        </div>
      </div>

      <div className="set-row">
        <div className="set-row__main">
          <label className="upd-toggle">
            <input
              type="checkbox"
              checked={enabled}
              disabled={save.isPending}
              onChange={(e) => save.mutate({ update_check: e.target.checked })}
            />
            <span className="set-row__title">
              Check for updates automatically
            </span>
          </label>
          <div className="set-row__sub">
            Asks the project once a day whether a newer version exists. Nothing
            about your library or your machine is sent, and Check now works
            either way.
          </div>
          {/* Stated rather than left to be discovered. Until releases are
              signed, an update can be installed by hand and never
              automatically — and a button that cannot work should not be
              offered as though it can. */}
          {download.isError && (
            <p className="upd-note upd-note--warn">
              The download failed: {(download.error as Error).message}
            </p>
          )}
          {status.can_verify === false && (
            <p className="upd-note">
              Automatic installation is unavailable in this build: it can only
              install a release whose signature it can verify.
            </p>
          )}
        </div>
      </div>
    </section>
  );
}

function describe(status: {
  current?: string;
  latest?: string;
  available?: boolean;
  checked_at?: number;
}): string {
  const running = `Running ${status.current ?? "an unknown build"}`;
  if (status.current === "dev") {
    return "Running a development build, which cannot be compared against a release.";
  }
  if (!status.checked_at) {
    return `${running}. Not checked yet.`;
  }
  // Local time, deliberately. A UTC-derived date reads as tomorrow all evening
  // in this timezone.
  const when = new Date(status.checked_at * 1000).toLocaleString();
  return status.available
    ? `${running}. Checked ${when}.`
    : `${running} — the newest release. Checked ${when}.`;
}
