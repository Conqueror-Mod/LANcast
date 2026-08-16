import { useCallback, useEffect, useRef, useState } from "react";
import { apiGet, apiPost, apiSend } from "@/api/client";
import type { TogetherSession } from "@/api/types";

/*
 * Watching the same thing at the same time.
 *
 * The server owns the truth — what is playing, where it is, whether it is
 * paused — and this follows it. The alternative, where every client broadcasts
 * its own position, makes the last writer win, and on a lossy connection that
 * is whoever lagged worst.
 *
 * Two roles, deliberately asymmetric:
 *
 *   - The **host** reports. Their player is the clock, and nothing corrects it.
 *   - A **follower** polls and converges. They never report, so a follower who
 *     pauses to answer the door does not pause the film in three houses.
 */

const POLL_MS = 2000;
const REPORT_MS = 3000;

/*
 * How far out of step is worth correcting.
 *
 * Below this, seeking would be more disruptive than the drift: a video element
 * seeking is a visible stutter, and a stutter every two seconds to fix a
 * quarter of a second nobody can perceive is a worse experience than the drift
 * it cures. Above it, people notice they are behind.
 */
const DRIFT_TOLERANCE_MS = 1500;

/**
 * expectedPosition works out where the film should be *now*, given what the
 * host said and how long ago they said it.
 *
 * Without this, every correction would land a client one poll-interval behind
 * and it would never catch up — it would seek to a position that was already
 * two seconds stale at the moment it arrived, then do it again.
 */
export function expectedPosition(
  session: Pick<TogetherSession, "position_ms" | "paused" | "updated_at">,
  nowMS: number,
): number {
  if (session.paused) return session.position_ms;
  const elapsed = nowMS - session.updated_at * 1000;
  // A negative elapsed means the clocks disagree; trusting it would seek
  // backwards on every poll. The reported position is the safer answer.
  if (elapsed < 0) return session.position_ms;
  return session.position_ms + elapsed;
}

/** True when a follower is far enough out of step to be worth a seek. */
export function shouldResync(
  localMS: number,
  expectedMS: number,
  tolerance = DRIFT_TOLERANCE_MS,
): boolean {
  return Math.abs(localMS - expectedMS) > tolerance;
}

export interface TogetherControls {
  session: TogetherSession | null;
  isHost: boolean;
  error: string | null;
  start: (itemID: number, positionMS: number) => Promise<TogetherSession | null>;
  join: (id: string) => Promise<TogetherSession | null>;
  leave: () => Promise<void>;
}

/**
 * useTogether owns membership of one room and the polling that keeps it alive.
 *
 * The poll doubles as presence: nobody presses "leave", they close the laptop,
 * so the server drops members who stop polling. That means this hook stopping
 * is how the room finds out somebody left.
 */
export function useTogether(userID: string | undefined): TogetherControls {
  const [session, setSession] = useState<TogetherSession | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Held in a ref as well as state so the polling effect does not need to be
  // torn down and rebuilt every time the session object changes — which is
  // every two seconds, and would restart the interval each time.
  const idRef = useRef<string | null>(null);

  const stop = useCallback(() => {
    idRef.current = null;
    setSession(null);
  }, []);

  const start = useCallback(
    async (itemID: number, positionMS: number) => {
      try {
        const created = await apiPost<TogetherSession>("/api/together", {
          item_id: itemID,
          position_ms: Math.round(positionMS),
        });
        idRef.current = created.id;
        setSession(created);
        setError(null);
        return created;
      } catch (e) {
        setError((e as Error).message);
        return null;
      }
    },
    [],
  );

  const join = useCallback(async (id: string) => {
    try {
      const joined = await apiPost<TogetherSession>(
        `/api/together/${id}/join`,
        {},
      );
      idRef.current = joined.id;
      setSession(joined);
      setError(null);
      return joined;
    } catch (e) {
      setError((e as Error).message);
      return null;
    }
  }, []);

  const leave = useCallback(async () => {
    const id = idRef.current;
    stop();
    if (!id) return;
    // Best effort: the room drops a silent member within ninety seconds
    // anyway, so a failed leave is untidy rather than broken.
    await apiSend(`/api/together/${id}`, "DELETE").catch(() => {});
  }, [stop]);

  useEffect(() => {
    if (!session) return;
    let cancelled = false;

    const tick = async () => {
      const id = idRef.current;
      if (!id) return;
      try {
        const next = await apiGet<TogetherSession>(`/api/together/${id}`);
        if (!cancelled) setSession(next);
      } catch {
        // A 404 means the room ended — the host left, or went quiet. Stopping
        // is the honest response: there is nothing left to follow, and
        // retrying would poll a room that no longer exists forever.
        if (!cancelled) {
          setError("That session has ended.");
          stop();
        }
      }
    };

    const timer = setInterval(tick, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [session, stop]);

  return {
    session,
    isHost: !!session && !!userID && session.host_id === userID,
    error,
    start,
    join,
    leave,
  };
}

/**
 * useHostReporting pushes the host's position to the server on an interval.
 *
 * Separated from the polling hook because only one participant does it, and
 * because a follower accidentally reporting is the failure that turns a
 * synchronised room into a tug of war.
 */
export function useHostReporting(
  sessionID: string | null,
  isHost: boolean,
  read: () => { positionMS: number; paused: boolean },
): void {
  const readRef = useRef(read);
  readRef.current = read;

  useEffect(() => {
    if (!sessionID || !isHost) return;
    const timer = setInterval(() => {
      const { positionMS, paused } = readRef.current();
      void apiSend(`/api/together/${sessionID}`, "PUT", {
        position_ms: Math.round(positionMS),
        paused,
      }).catch(() => {
        // A dropped report is corrected by the next one two seconds later.
        // Surfacing it would be an error message for a condition that fixes
        // itself before anybody finishes reading it.
      });
    }, REPORT_MS);
    return () => clearInterval(timer);
  }, [sessionID, isHost]);
}
