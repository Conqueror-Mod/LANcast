/*
 * The two app-wide moments worth writing to the window's log, kept out of
 * main.tsx so they can be tested (main.tsx runs them as side effects at
 * start-up, where a test cannot reach).
 */
import { clientNote, whereFrom } from "./clientNote";

/**
 * A query was answered 401: this window believed it was signed in and the
 * server says otherwise. 2 October was a cookie the browser dropped at day
 * thirty while the server still held the session, and nothing on this side
 * recorded the moment. Repeats are collapsed by the binding, so a page full
 * of failing queries is one line.
 */
export function noteSignedOut(queryKeyHead: unknown): void {
  clientNote(
    "warn",
    "auth",
    `server said this window is not signed in (401 on ${String(queryKeyHead)}); returning to sign-in`,
  );
}

/** Errors nothing caught, which used to exist only in a console nobody had open. */
export function installErrorNotes(target: Pick<Window, "addEventListener">): void {
  target.addEventListener("error", (e: Event) => {
    const ev = e as ErrorEvent;
    clientNote("error", "error", `uncaught: ${ev.message} at ${ev.filename}:${ev.lineno}:${ev.colno}`);
  });
  target.addEventListener("unhandledrejection", (e: Event) => {
    const reason = (e as PromiseRejectionEvent).reason;
    const message = reason instanceof Error ? reason.message : String(reason);
    clientNote("error", "error", `unhandled rejection: ${message}${whereFrom(reason)}`);
  });
}
