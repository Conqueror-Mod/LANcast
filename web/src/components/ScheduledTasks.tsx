import { useScheduledTasks, useRunTask, type ScheduledTask } from "@/api/hooks";
import "./ScheduledTasks.css";

/*
 * Status › Scheduled tasks (docs/settings-plan.md, step 3): every background
 * job, what sets it off, when it last ran and how that went, and Run now.
 *
 * Read-only about schedules for now. Only the library scan has one a person
 * can set, and it stays on Libraries beside the libraries it scans; editing
 * schedules here is the plan's next step for this pane.
 */
export function ScheduledTasks() {
  const { data: list, isError } = useScheduledTasks();
  const run = useRunTask();

  return (
    <section className="settings__section">
      <span className="section-label">Scheduled tasks</span>
      <p className="set-row__sub set-row__sub--standalone">
        Everything this server does in the background. Run now starts a job straight
        away; it never starts a second copy of one already running.
      </p>
      {isError && <p className="set-error">The server did not answer. Try again in a moment.</p>}
      {list && (
        <div className="tasks" role="table" aria-label="Scheduled tasks">
          <div className="tasks__row tasks__row--head" role="row">
            <span role="columnheader">Task</span>
            <span role="columnheader">Runs</span>
            <span role="columnheader">Last run</span>
            <span role="columnheader">Next</span>
            <span role="columnheader" className="tasks__action" aria-label="Action" />
          </div>
          {list.map((t) => (
            <div className={"tasks__row" + (t.enabled ? "" : " is-off")} role="row" key={t.id}>
              <span role="cell" className="tasks__name">
                {t.name}
                <span className="tasks__desc">{t.description}</span>
              </span>
              <span role="cell">{runsLabel(t)}</span>
              <span role="cell">
                {t.running ? (
                  <span className="tasks__running">Running now</span>
                ) : t.last_finished_at ? (
                  <>
                    {when(t.last_finished_at)}
                    {t.last_error ? (
                      <span className="tasks__error" title={t.last_error}>
                        Failed: {t.last_error}
                      </span>
                    ) : null}
                  </>
                ) : (
                  <span className="tasks__muted">Not since the server started</span>
                )}
              </span>
              <span role="cell">{nextLabel(t)}</span>
              <span role="cell" className="tasks__action">
                {/* Off is off: a switched-off job checks its own setting and
                    would do nothing, so the button does not pretend. */}
                <button
                  className="set-btn"
                  disabled={!t.enabled || t.running || (run.isPending && run.variables === t.id)}
                  title={t.enabled ? undefined : "Switched off in Settings"}
                  onClick={() => run.mutate(t.id)}
                >
                  {t.running ? "Running…" : "Run now"}
                </button>
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

// When a job is next due. One that is past due is due now: the timer checks
// every half hour or so, and "today 01:41" after 01:41 reads as a mistake.
function nextLabel(t: ScheduledTask, now = new Date()) {
  if (!t.next_at) return <span className="tasks__muted">—</span>;
  if (new Date(t.next_at).getTime() <= now.getTime()) return "Due now";
  return when(t.next_at);
}

// What sets a job off, in words.
function runsLabel(t: ScheduledTask): string {
  if (!t.enabled) return "Off — set in Settings";
  switch (t.trigger) {
    case "after-scan":
      return "After each scan";
    case "interval":
      return t.every_hours ? everyLabel(t.every_hours) : "Only when asked";
    default:
      return "Only when asked";
  }
}

function everyLabel(hours: number): string {
  if (hours === 24) return "Daily";
  if (hours === 168) return "Weekly";
  if (hours === 1) return "Hourly";
  if (hours % 24 === 0) return `Every ${hours / 24} days`;
  return `Every ${hours} hours`;
}

/*
 * A time, said the way a person reads it: "today 03:00", "yesterday 21:14",
 * or a date. Built from local components on purpose — the server sends UTC,
 * and a date taken from the ISO string is tomorrow every evening in the US.
 */
export function when(iso: string, now = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(d) - day(now)) / 86_400_000);
  if (diff === 0) return `Today ${time}`;
  if (diff === -1) return `Yesterday ${time}`;
  if (diff === 1) return `Tomorrow ${time}`;
  return `${d.toLocaleDateString(undefined, { day: "numeric", month: "short" })} ${time}`;
}
