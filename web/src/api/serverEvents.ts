import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";

/*
 * The server saying that something changed
 * ([ADR 0079](../../../docs/adr/0079-the-server-says-when-something-changed.md)).
 *
 * Before this, a screen learned of change only from its own mutations or a job
 * poll. Anything the server changed by itself stayed stale until the screen
 * remounted: Settings said "Added, not yet mutual" about a pairing that was
 * mutual, and People did not show a friend's film until it was forced to
 * refetch.
 *
 * The stream carries topic names and nothing else. Each topic invalidates the
 * queries below, and they refetch through their usual routes.
 */

/*
 * What each topic makes stale. **The one place a screen opts in.**
 *
 * A new query whose data the server can change by itself needs a row here, and
 * the prefix rule from CLAUDE.md applies: `["items"]` reaches
 * `["items", "infinite", …]`, but `["items-infinite"]` would be missed by it.
 * The topics match `internal/api/events.go` and the table in docs/api.md.
 */
export const topicKeys: Record<string, readonly (readonly string[])[]> = {
  peers: [["peers"], ["peer-presence"], ["peer-shares"], ["peer-libraries"]],
  presence: [["peer-presence"]],
  libraries: [["libraries"], ["items"], ["recently-added"], ["continue"]],
  items: [["items"], ["recently-added"], ["continue"], ["recent-photos"]],
};

/** Invalidates what each named topic makes stale. Unknown topics are ignored:
 * a newer server may announce things this client does not show. */
export function invalidateTopics(qc: QueryClient, topics: readonly string[]) {
  const seen = new Set<string>();
  for (const topic of topics) {
    for (const key of topicKeys[topic] ?? []) {
      const id = key.join("\u0000");
      if (seen.has(id)) continue;
      seen.add(id);
      void qc.invalidateQueries({ queryKey: [...key] });
    }
  }
}

/*
 * How long to wait before reopening a stream the browser gave up on.
 *
 * EventSource reconnects by itself after a dropped connection, using the
 * server's `retry:`. It does **not** after an answer that was not a stream,
 * such as a 401 or a server mid-restart answering 503: it closes for good.
 * Without this a window would go deaf for the rest of its life after one bad
 * answer.
 */
const REOPEN_MS = 15_000;

/*
 * useServerEvents keeps one stream open for as long as it is mounted.
 *
 * Mounted once, inside the signed-in app (App.tsx), so a signed-out window
 * holds no stream and a sign-in opens one.
 *
 * **Every reconnect refetches everything on screen** (ADR 0079 §4). Events sent
 * while the stream was down are gone, and that includes the gap while a
 * minimised window was frozen, so one refetch on the way back is the price of
 * never being stale for ever. The first open is not a reconnect: the screen
 * has just fetched.
 */
export function useServerEvents() {
  const qc = useQueryClient();

  useEffect(() => {
    if (typeof EventSource === "undefined") return;

    let source: EventSource | null = null;
    let reopen: ReturnType<typeof setTimeout> | undefined;
    let opened = false;
    let stopped = false;

    const onChanged = (e: MessageEvent) => {
      try {
        const body = JSON.parse(e.data) as { topics?: unknown };
        if (Array.isArray(body.topics)) {
          invalidateTopics(
            qc,
            body.topics.filter((t): t is string => typeof t === "string"),
          );
        }
      } catch {
        // A malformed event says nothing; the next one may.
      }
    };

    const open = () => {
      if (stopped) return;
      const es = new EventSource("/api/events");
      source = es;
      es.addEventListener("changed", onChanged as EventListener);
      es.onopen = () => {
        if (opened) void qc.invalidateQueries({ type: "active" });
        opened = true;
      };
      es.onerror = () => {
        // CONNECTING means the browser is already retrying. CLOSED means it
        // gave up, and only we can try again.
        if (es.readyState === EventSource.CLOSED && !stopped) {
          es.close();
          reopen = setTimeout(open, REOPEN_MS);
        }
      };
    };

    open();
    return () => {
      stopped = true;
      clearTimeout(reopen);
      source?.close();
    };
  }, [qc]);
}

/** The hook as a component, for mounting where hooks cannot be called
 * conditionally. Renders nothing. */
export function ServerEvents() {
  useServerEvents();
  return null;
}
