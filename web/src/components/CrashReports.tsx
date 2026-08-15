import { useState } from "react";
import { useCrashes, useClearCrashes } from "@/api/hooks";
import "./CrashReports.css";

/*
 * Crash reports, in Settings.
 *
 * The server used to lose these. A panic unwound through net/http, which closes
 * the connection without a response — the client saw a network error, and the
 * operator saw nothing at all unless they happened to be reading the log at the
 * moment it happened. That is the recurring failure shape in this project: a
 * fault with nowhere to appear.
 *
 * Collapsed by default, and never polled. This is a screen whose ideal state is
 * empty, and a background request every few seconds to be told "still none"
 * is a cost paid forever for the rare case.
 */
export function CrashReports() {
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const { data, isLoading } = useCrashes(open);
  const clear = useClearCrashes();

  const crashes = data?.crashes ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Crash reports</span>
      <p className="crash__intro">
        Faults the server caught and recovered from. Recorded as files beside
        the database — nothing is sent anywhere.
      </p>

      <button className="crash__toggle" onClick={() => setOpen(!open)}>
        {open ? "Hide" : "Show crash reports"}
      </button>

      {open && isLoading && <p className="crash__empty">Loading…</p>}

      {open && !isLoading && crashes.length === 0 && (
        <p className="crash__empty">
          None. That is the answer this screen wants to give.
        </p>
      )}

      {open && crashes.length > 0 && (
        <>
          <div className="crash__list">
            {crashes.map((c) => (
              <div className="crash__row" key={c.id}>
                <button
                  className="crash__head"
                  onClick={() =>
                    setExpanded(expanded === c.id ? null : c.id)
                  }
                  aria-expanded={expanded === c.id}
                >
                  <span className="crash__when">
                    {new Date(c.at).toLocaleString()}
                  </span>
                  {/* The route pattern, which is what somebody fixes. */}
                  <span className="crash__where">{c.where}</span>
                  <span className="crash__value">{c.value}</span>
                  <span className="crash__version">v{c.version}</span>
                </button>

                {expanded === c.id && (
                  // The stack is behind a click because it is thirty lines and
                  // the list is the part that gets read. Selectable, because
                  // the next thing anybody does with it is paste it somewhere.
                  <pre className="crash__stack">{c.stack}</pre>
                )}
              </div>
            ))}
          </div>

          <button
            className="crash__clear"
            onClick={() => clear.mutate()}
            disabled={clear.isPending}
          >
            Clear reports
          </button>
        </>
      )}
    </section>
  );
}
