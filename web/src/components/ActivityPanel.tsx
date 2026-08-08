import { useEffect, useRef, useState } from "react";
import { useActivity } from "@/api/hooks";
import type { Activity } from "@/api/types";
import "./ActivityPanel.css";

// The nav's activity indicator: a pulsing dot while the server is working, a
// popover listing what it is working on. Scans, metadata, probing, artwork and
// live transcodes all arrive in one shape from /api/activity, so this renders a
// list rather than knowing about five workers.
//
// It is deliberately quiet when idle — a status light that is always lit tells
// you nothing. Nothing here is gold: this is *what the server is doing*, not
// *where you are*.
export function ActivityPanel() {
  const { data } = useActivity();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const tasks = data?.tasks ?? [];
  const active = data?.active ?? false;
  const failed = tasks.some((t) => t.state === "failed");

  // Close on an outside click or Escape. A popover that traps you is worse than
  // no popover.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // A failure stays visible until it is looked at; a quiet idle server does not
  // need a control at all.
  if (!active && !open) return null;

  return (
    <div className="activity" ref={ref}>
      <button
        type="button"
        className={"activity__trigger" + (failed ? " is-failed" : "")}
        aria-expanded={open}
        aria-label={`Server activity: ${tasks.length} running`}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="activity__dot" aria-hidden="true" />
        <span className="activity__label">
          {failed ? "Attention" : summary(tasks)}
        </span>
      </button>
      {open && (
        <div className="activity__panel" role="dialog" aria-label="Server activity">
          {tasks.length === 0 && (
            <p className="activity__idle">Nothing running.</p>
          )}
          {tasks.map((t) => (
            <ActivityRow key={t.id} task={t} />
          ))}
        </div>
      )}
    </div>
  );
}

function ActivityRow({ task }: { task: Activity }) {
  // Total 0 means the worker cannot know how much is left — a scan discovers
  // its own size. Showing a bar at 0% would be a lie, so it shows a count.
  const determinate = task.total > 0;
  const pct = determinate
    ? Math.min(100, Math.round((task.done / task.total) * 100))
    : 0;

  return (
    <div className={"activity__row" + (task.state === "failed" ? " is-failed" : "")}>
      <div className="activity__row-head">
        <span className="activity__title">{task.title}</span>
        <span className="activity__count">
          {determinate ? `${task.done} of ${task.total}` : countOnly(task.done)}
        </span>
      </div>
      {determinate ? (
        <div className="activity__bar">
          <div className="activity__bar-fill" style={{ width: `${pct}%` }} />
        </div>
      ) : (
        <div className="activity__bar activity__bar--indeterminate">
          <div className="activity__bar-sweep" />
        </div>
      )}
      {(task.error || task.detail) && (
        <p className="activity__detail">{task.error || task.detail}</p>
      )}
    </div>
  );
}

function countOnly(done: number): string {
  if (done <= 0) return "starting";
  return `${done.toLocaleString()} so far`;
}

function summary(tasks: Activity[]): string {
  if (tasks.length === 0) return "Idle";
  if (tasks.length === 1) return tasks[0].title;
  return `${tasks.length} tasks`;
}
