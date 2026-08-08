import { useState } from "react";
import { useAuditLog } from "@/api/hooks";
import type { AuditEvent } from "@/api/types";
import "./AuditLog.css";

const PAGE = 50;

// The audit log (ADR 0026): who changed what, and when.
//
// Collapsed by default and never polled. It records deliberate acts, which do
// not happen while you are looking at the page — the button that opens it and
// the Refresh beside it are the only two things that should fetch.
//
// Summaries arrive already resolved from the server and are rendered verbatim.
// The client must not reconstruct them: an event about a deleted library has
// nothing left to join to, which is the whole reason the server writes the
// sentence rather than the ids.
export function AuditLog() {
  const [open, setOpen] = useState(false);
  const [action, setAction] = useState("");
  const [limit, setLimit] = useState(PAGE);
  const { data, isFetching, error, refetch } = useAuditLog(open, action, limit);

  const events = data?.events ?? [];
  const total = data?.total ?? 0;
  const actions = data?.actions ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Audit log</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            Who changed what
            {open && total > 0 && (
              <span className="audit__total">
                {total.toLocaleString()} event{total === 1 ? "" : "s"}
              </span>
            )}
          </div>
          <div className="set-row__sub">
            Libraries added or removed, titles deleted, matches overridden,
            accounts changed, add-ons trusted. Recorded on the server, so it is
            not something a client can rewrite.
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
        <div className="audit">
          {actions.length > 1 && (
            <div className="audit__filters">
              <FilterChip
                label="All"
                active={action === ""}
                onClick={() => {
                  setAction("");
                  setLimit(PAGE);
                }}
              />
              {actions.map((a) => (
                <FilterChip
                  key={a}
                  label={a}
                  active={action === a}
                  onClick={() => {
                    setAction(a);
                    setLimit(PAGE);
                  }}
                />
              ))}
            </div>
          )}

          {error && <p className="audit__note">Could not read the audit log.</p>}

          {!error && !isFetching && events.length === 0 && (
            <p className="audit__note">
              {action
                ? "Nothing recorded for that action yet."
                : "Nothing recorded yet. Events appear here the first time something is changed."}
            </p>
          )}

          {events.length > 0 && (
            <ol className="audit__list">
              {events.map((e) => (
                <AuditRow key={e.id} event={e} />
              ))}
            </ol>
          )}

          {events.length < total && (
            <button
              className="set-btn audit__more"
              disabled={isFetching}
              onClick={() => setLimit((n) => n + PAGE)}
            >
              Show {Math.min(PAGE, total - events.length)} more of{" "}
              {total.toLocaleString()}
            </button>
          )}
        </div>
      )}
    </section>
  );
}

function FilterChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={"audit__chip" + (active ? " is-active" : "")}
      aria-pressed={active}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function AuditRow({ event }: { event: AuditEvent }) {
  const [showDetail, setShowDetail] = useState(false);
  return (
    <li className="audit__row">
      <div className="audit__row-head">
        <span className="audit__when" title={fullTime(event.at)}>
          {shortTime(event.at)}
        </span>
        <span className="audit__actor">{event.actor_name}</span>
        <span className="audit__action">{event.action}</span>
      </div>
      <p className="audit__summary">{event.summary}</p>
      {event.detail && (
        <>
          <button
            type="button"
            className="audit__detail-toggle"
            onClick={() => setShowDetail((v) => !v)}
          >
            {showDetail ? "Hide details" : "Details"}
          </button>
          {showDetail && <pre className="audit__detail">{pretty(event.detail)}</pre>}
        </>
      )}
    </li>
  );
}

// Both timestamps are built from local components. A UTC-derived date reads as
// tomorrow all evening in this timezone, and an audit log that misdates events
// is worse than none.
function shortTime(unix: number): string {
  const d = new Date(unix * 1000);
  const today = new Date();
  const sameDay =
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate();
  const time = d.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
  return sameDay ? time : `${d.toLocaleDateString()} ${time}`;
}

function fullTime(unix: number): string {
  return new Date(unix * 1000).toLocaleString();
}

// The detail blob is JSON the server wrote. Pretty-print it when it parses and
// show it raw when it does not — a detail that cannot be parsed is still
// evidence, and swallowing it would hide the one row worth reading.
function pretty(detail: string): string {
  try {
    return JSON.stringify(JSON.parse(detail), null, 2);
  } catch {
    return detail;
  }
}
