import { useState } from "react";
import {
  useSettings,
  useUpdateSettings,
  useClearCache,
  useResetSettings,
  useServerLog,
} from "@/api/hooks";
import { CrashReports } from "@/components/CrashReports";
import { MetricsSettings } from "@/components/MetricsSettings";

// The server log, read from the UI. It has been written beside the database
// since v0.4.2 and until now could only be read by finding the data directory
// in a file manager — which is the wrong ask for the case it exists to serve,
// because the log matters most when the server runs as a service and something
// is wrong.
//
// Collapsed by default and never polled: this is what already happened, and the
// button that opens it is the refresh.
function ServerLogSection() {
  const [open, setOpen] = useState(false);
  const { data, isFetching, error, refetch } = useServerLog(open);
  const lines = data?.lines ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Server log</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{data?.path ?? "lancastd.log"}</div>
          <div className="set-row__sub">
            Written beside the database. A server running as a service has no
            console, so this is the only record of why it stopped.
          </div>
        </div>
        <div className="set-row__actions">
          {open && (
            <button
              className="set-btn"
              onClick={() => refetch()}
              disabled={isFetching}
            >
              {isFetching ? "Reading…" : "Refresh"}
            </button>
          )}
          <button className="set-btn" onClick={() => setOpen((v) => !v)}>
            {open ? "Hide" : "Show log"}
          </button>
        </div>
      </div>
      {open && (
        <div className="set-log">
          {error && <p className="set-log__note">Could not read the log.</p>}
          {!error && !isFetching && lines.length === 0 && (
            <p className="set-log__note">
              The log is empty. A server that has only ever run in a terminal
              writes to the terminal instead.
            </p>
          )}
          {lines.length > 0 && (
            <>
              {/* Saying the view is partial is the difference between "this is
                  the log" and "this is the end of the log". */}
              {data && !data.complete && (
                <p className="set-log__note">
                  Showing the last {lines.length.toLocaleString()} lines. Older
                  entries are in the file.
                </p>
              )}
              <pre className="set-log__body">{lines.join("\n")}</pre>
            </>
          )}
        </div>
      )}
    </section>
  );
}

/*
 * Debug logging, and the two things it is reasonable to throw away.
 *
 * On the Logs pane because that is where somebody already is when they want
 * any of it: they came to read the log because something is wrong, and the next
 * two questions are "can I get more detail" and "can I make it stop by clearing
 * something".
 *
 * Every action here is recoverable by the server itself — cached artwork
 * re-downloads, transcode scratch is rebuilt on the next play, settings return
 * to documented defaults. Nothing touches media, the database, accounts, or
 * anything a person typed. That boundary is the feature, and it is stated on
 * the controls rather than left to be discovered.
 */
function MaintenanceSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const clear = useClearCache();
  const reset = useResetSettings();
  const [confirmReset, setConfirmReset] = useState(false);
  const [freed, setFreed] = useState<string | null>(null);

  return (
    <section className="settings__section">
      <span className="section-label">Diagnostics</span>

      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.debug_logging}
              onChange={(e) =>
                update.mutate({ debug_logging: e.target.checked })
              }
            />
            Write debug detail to the log
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            Takes effect on the next line logged — no restart — and survives
            one, because the faults worth turning this on for are the
            intermittent ones. Leave it off for ordinary running: it is verbose.
          </div>
        </>
      )}

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Clear cached artwork</div>
          <div className="set-row__sub">
            Posters and backgrounds are downloaded again as they are needed.
            Nothing about your library changes; artwork is blank until it
            arrives.
            {freed && <> · {freed} freed</>}
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={clear.isPending}
            onClick={() =>
              clear.mutate("artwork", {
                onSuccess: (r) =>
                  setFreed(
                    `${Math.round((r.freed_bytes / 1048576) * 10) / 10} MB`,
                  ),
              })
            }
          >
            {clear.isPending ? "Clearing…" : "Clear"}
          </button>
        </div>
      </div>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Clear transcode scratch</div>
          <div className="set-row__sub">
            Stops anything being transcoded right now and deletes its working
            files. A few seconds of buffered video, rebuilt the moment somebody
            presses play.
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={clear.isPending}
            onClick={() => clear.mutate("transcode")}
          >
            Clear
          </button>
        </div>
      </div>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Reset settings</div>
          <div className="set-row__sub">
            Puts every setting back to its default. Your password, provider API
            keys, certificate paths and ffmpeg location are kept — a reset
            cannot restore those, and losing the first would lock you out of
            your own server.
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={reset.isPending}
            onClick={() => {
              if (!confirmReset) {
                setConfirmReset(true);
                return;
              }
              reset.mutate(undefined, {
                onSuccess: () => setConfirmReset(false),
              });
            }}
          >
            {reset.isPending
              ? "Resetting…"
              : confirmReset
                ? "Reset everything?"
                : "Reset"}
          </button>
        </div>
      </div>
    </section>
  );
}

export function TroubleshootingPane() {
  return (
    <>
      <ServerLogSection />
      <CrashReports />
      <MetricsSettings />
      <MaintenanceSection />
    </>
  );
}
