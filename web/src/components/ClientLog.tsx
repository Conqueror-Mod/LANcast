import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
// No stylesheet of its own: this is the server log section's twin, and the
// `set-log` rules in Settings.css are what make the two read as one pair.

/*
 * The desktop window's own log, in Settings.
 *
 * The client is a `-H=windowsgui` binary with nowhere to print, so until
 * recently every line it wrote went to a stderr nobody was holding. It now
 * keeps a file — and a file in an application data directory somebody has to be
 * told how to find is most of the way to no log at all. Every time it has been
 * needed so far, the next step was a message explaining where to look.
 *
 * Through a binding rather than the API, and rendered only where that binding
 * exists. The server has never seen this file: it is written by a different
 * process, on this machine, and a phone opening Settings is not looking at this
 * window's log. Feature detection is the honest test, for the same reason the
 * lifecycle section beside it uses one.
 *
 * Not polled. This is read after something has already gone wrong, and a
 * request every few seconds to be told the same four hundred lines is a cost
 * paid for ever for the rare case. There is a Refresh button instead.
 */

declare global {
  interface Window {
    lancastClientLog?: () => Promise<ClientLogAnswer>;
  }
}

export interface ClientLogAnswer {
  path?: string;
  lines?: string[];
  /** False when the view starts part-way into the file. */
  complete?: boolean;
  error?: string;
}

export function ClientLog() {
  const [open, setOpen] = useState(false);
  const { data, isFetching, refetch } = useQuery({
    queryKey: ["client-log"],
    queryFn: () => window.lancastClientLog!(),
    enabled: open && !!window.lancastClientLog,
    staleTime: 0,
  });

  if (!window.lancastClientLog) return null;

  const lines = data?.lines ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">App log</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{data?.path ?? "lancast-client.log"}</div>
          <div className="set-row__sub">
            What this window has done: whether it started a server or waited for
            one, what it made of the autostart setting, and where it put a
            game's window. Written on this machine only — the server never sees
            it, and nothing is sent anywhere.
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
          {data?.error && (
            <p className="set-log__note">Could not read the log: {data.error}</p>
          )}
          {!data?.error && !isFetching && lines.length === 0 && (
            // Not a fault. A window that has only ever run from a terminal may
            // never have written one.
            <p className="set-log__note">
              Nothing written yet. The log starts when this window next opens.
            </p>
          )}
          {lines.length > 0 && (
            <>
              {/* Saying the view is partial is the difference between "this is
                  the log" and "this is the end of the log". */}
              {data && data.complete === false && (
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
