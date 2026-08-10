import {
  useUpdateStatus,
  useCheckForUpdate,
  useDownloadUpdate,
  useSettings,
  useUpdateSettings,
} from "@/api/hooks";
import "./UpdateSettings.css";

// Updates, in Settings. The activity indicator carries the same fact when an
// update is waiting; this is the place to ask on purpose, and the place that
// explains why automatic installation is or is not available.
export function UpdateSettings() {
  const { data: status } = useUpdateStatus();
  const { data: settings } = useSettings();
  const check = useCheckForUpdate();
  const download = useDownloadUpdate();
  const save = useUpdateSettings();

  if (!status?.supported) return null;

  const enabled = settings?.update_check ?? true;

  return (
    <section className="settings__section">
      <span className="section-label">Updates</span>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {status.staged ? (
              <>
                LANcast {status.staged} is ready
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
            {status.staged
              ? "Downloaded and verified. It takes effect the next time the server starts."
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
          {status.available && !status.staged && status.can_verify && (
            <button
              className="set-btn"
              disabled={download.isPending || status.downloading?.active}
              onClick={() => download.mutate()}
            >
              {status.downloading?.active ? "Downloading…" : "Download and install"}
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
            <span className="set-row__title">Check for updates automatically</span>
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
