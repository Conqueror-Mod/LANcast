import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost, apiSend, ApiFailure } from "@/api/client";
import type { TogetherSession } from "@/api/types";
import { peerRoomURL } from "./peerSource";

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
 * A follower on another server polls faster, and the host reports a pause or
 * a jump the moment it happens rather than on its next three-second beat.
 * Together the two delays were why a guest paused about two seconds after the
 * host did, and could take five (2026-10-09). The poll is relayed through two
 * servers, so once a second is the cost of a pause landing within one.
 */
const PEER_POLL_MS = 1000;
const STATE_WATCH_MS = 250;
// A jump the host makes is a report at once when the clock moves this far
// from where it would have been.
const JUMP_MS = 2000;

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
 *
 * "How long ago" is the server's `age_ms`, measured by the server as it
 * answered, plus the time since this device received the answer. Neither half
 * compares this device's clock with anybody else's. That matters once a room
 * crosses to another household, where the two clocks are two machines' NTP
 * and the error is silent: everybody just drifts.
 */
export function expectedPosition(
  session: Pick<TogetherSession, "position_ms" | "paused" | "updated_at"> & {
    age_ms?: number;
  },
  receivedAtMS: number,
  nowMS: number,
): number {
  if (session.paused) return session.position_ms;
  let elapsed: number;
  if (typeof session.age_ms === "number") {
    elapsed = session.age_ms + (nowMS - receivedAtMS);
  } else {
    // A server from before age_ms: the old arithmetic, which does compare
    // clocks, and is only as good as they agree.
    elapsed = nowMS - session.updated_at * 1000;
  }
  // A negative elapsed means something disagrees about time; trusting it would
  // seek backwards on every poll. The reported position is the safer answer.
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
  /** When this device received `session`, by its own clock (Date.now()). */
  receivedAt: number;
  isHost: boolean;
  error: string | null;
  start: (itemID: number, positionMS: number) => Promise<TogetherSession | null>;
  join: (id: string) => Promise<TogetherSession | null>;
  leave: () => Promise<void>;
  /**
   * Take a room the server just answered with as the current one: accepting
   * a friend into the room answers with the room including them, and the
   * member list should show them now rather than on the next poll.
   */
  adopt: (next: TogetherSession) => void;
}

/**
 * useTogether owns membership of one room and the polling that keeps it alive.
 *
 * The poll doubles as presence: nobody presses "leave", they close the laptop,
 * so the server drops members who stop polling. That means this hook stopping
 * is how the room finds out somebody left.
 */
export function useTogether(userID: string | undefined): TogetherControls {
  const qc = useQueryClient();
  /*
   * Starting, joining and leaving all change what the open-sessions list
   * holds, and somebody may be looking at it: the panel and the People page
   * both show it. So each one invalidates it, rather than leaving a room that
   * ended, or one you are already in, on screen until the next poll.
   */
  const changed = useCallback(
    () => void qc.invalidateQueries({ queryKey: ["together-sessions"] }),
    [qc],
  );
  const [session, setSessionState] = useState<TogetherSession | null>(null);
  const [receivedAt, setReceivedAt] = useState(0);
  // Every answer is stamped as it arrives: the server's age_ms is measured to
  // the moment it answered, and the follower adds what has passed since.
  const setSession = useCallback((next: TogetherSession | null) => {
    setReceivedAt(Date.now());
    setSessionState(next);
  }, []);
  const [error, setError] = useState<string | null>(null);
  // Held in a ref as well as state so the polling effect does not need to be
  // torn down and rebuilt every time the session object changes — which is
  // every two seconds, and would restart the interval each time.
  const idRef = useRef<string | null>(null);

  const stop = useCallback(() => {
    idRef.current = null;
    setSession(null);
  }, [setSession]);

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
        changed();
        return created;
      } catch (e) {
        setError((e as Error).message);
        return null;
      }
    },
    [setSession, changed],
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
      changed();
      return joined;
    } catch (e) {
      setError((e as Error).message);
      return null;
    }
  }, [setSession, changed]);

  const leave = useCallback(async () => {
    const id = idRef.current;
    stop();
    if (!id) return;
    // Best effort: the room drops a silent member within ninety seconds
    // anyway, so a failed leave is untidy rather than broken.
    await apiSend(`/api/together/${id}`, "DELETE").catch(() => {});
    changed();
  }, [stop, changed]);

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

  const adopt = useCallback(
    (next: TogetherSession) => {
      if (idRef.current === next.id) setSession(next);
    },
    [setSession],
  );

  return {
    session,
    receivedAt,
    isHost: !!session && !!userID && session.host_id === userID,
    error,
    start,
    join,
    leave,
    adopt,
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
    let last = { positionMS: 0, paused: false, at: 0 };
    const report = () => {
      const { positionMS, paused } = readRef.current();
      last = { positionMS, paused, at: Date.now() };
      void apiSend(`/api/together/${sessionID}`, "PUT", {
        position_ms: Math.round(positionMS),
        paused,
      }).catch(() => {
        // A dropped report is corrected by the next one two seconds later.
        // Surfacing it would be an error message for a condition that fixes
        // itself before anybody finishes reading it.
      });
    };
    report();
    const timer = setInterval(report, REPORT_MS);
    // Between beats: a pause, a play or a jump is reported at once.
    const watch = setInterval(() => {
      if (last.at === 0) return;
      const { positionMS, paused } = readRef.current();
      const drifted = last.paused
        ? positionMS - last.positionMS
        : positionMS - (last.positionMS + (Date.now() - last.at));
      if (paused !== last.paused || Math.abs(drifted) > JUMP_MS) report();
    }, STATE_WATCH_MS);
    return () => {
      clearInterval(timer);
      clearInterval(watch);
    };
  }, [sessionID, isHost]);
}

/*
 * How a follower of a room on another server decides to seek.
 *
 * The local rule, with one difference that matters on a converted stream. A
 * seek there is not a seek: it asks the far server to start converting again
 * from somewhere else, and for several seconds the clock reads the new
 * starting point while nothing plays. Judged by the ordinary 1.5 s tolerance,
 * every poll in those seconds would find the follower "behind" and seek again,
 * restarting the conversion each time, so the film would never start.
 *
 * So a converted stream gets a wider tolerance, and no seek is judged until
 * the last one has had time to land.
 */
export const CONVERTING_TOLERANCE_MS = 8000;

/*
 * The longest a converted stream is waited on after a seek before the room
 * may move it again. Long enough for a slow first segment, short enough that a
 * conversion which never starts is retried.
 */
export const CONVERTING_START_CAP_MS = 30_000;

/*
 * Two more rules for a converted stream, both found between two real servers
 * (2026-10-09): Georgia joined Chris's room and the conversion restarted at
 * 94 s, 104 s and 114 s, each thrown away ten seconds in with nothing served.
 *
 * **Wait for the picture, not a timer.** A fresh conversion there took longer
 * than the 8 s settle to play its first frame. Eight seconds after the seek
 * the follower was still at its starting point, more than 8 s behind a host
 * who had moved on, so it seeked again — into another restart of the same
 * length. So no seek is judged until the stream has actually started playing
 * since the last one (`startedSinceSeek`), up to CONVERTING_START_CAP_MS.
 *
 * **Aim ahead.** Even once it plays, a stream that took T to start lands T
 * behind the host, and if T is over the tolerance every catch-up seek
 * reproduces the gap it was meant to close. So a converted follower seeks to
 * where the host *will* be when the new stream starts: the expected position
 * plus the startup it last measured (followerSeekTarget).
 */
export function followerShouldSeek(
  localMS: number,
  expectedMS: number,
  converting: boolean,
  msSinceLastSeek: number,
  startedSinceSeek = true,
): boolean {
  if (converting && !startedSinceSeek && msSinceLastSeek < CONVERTING_START_CAP_MS) return false;
  const settle = converting ? CONVERTING_TOLERANCE_MS : 2500;
  if (msSinceLastSeek < settle) return false;
  return shouldResync(
    localMS,
    expectedMS,
    converting ? CONVERTING_TOLERANCE_MS : DRIFT_TOLERANCE_MS,
  );
}

// Where a follower seeks to: ahead by the lead on a converted stream (see
// above), capped so one freak start cannot throw it far forward.
export const CONVERTING_LEAD_CAP_MS = 20_000;

export function followerSeekTarget(expectedMS: number, converting: boolean, leadMS: number): number {
  if (!converting) return expectedMS;
  return expectedMS + Math.min(Math.max(0, leadMS), CONVERTING_LEAD_CAP_MS);
}

/*
 * Landing close, not merely starting once (2026-10-09, after the fix above).
 *
 * With the restarts gone, Georgia's first picture came in four seconds and
 * then sat about eight seconds behind for the whole film: the first seek had
 * no measured lead to aim with, and eight seconds is inside the converted
 * tolerance, so nothing ever corrected it.
 *
 * **The lead is learned from where a start lands**, not from how long it took.
 * When a converted stream starts playing, the gap between where the host is
 * now and where the stream began is everything the lead failed to cover —
 * the start-up, the poll that was already old, the keyframe the conversion
 * had to begin at. The next lead is the last one plus that gap (nextLead), so
 * it converges on whatever this pair of machines actually costs. It starts at
 * CONVERTING_DEFAULT_LEAD_MS, the start-up measured that evening.
 *
 * **A close landing is worth one short pause.** Once a converted stream is
 * playing, a gap over CONVERTING_CORRECT_MS is corrected with one more seek
 * at the learned lead — a few seconds' pause, against watching eight seconds
 * behind for two hours. At most MAX_CORRECTIONS per join, so a lead that will
 * not settle costs two pauses and then stops trying; the 8 s rule above still
 * catches anything that drifts far later.
 */
export const CONVERTING_DEFAULT_LEAD_MS = 4000;
export const CONVERTING_CORRECT_MS = 3000;
export const MAX_CORRECTIONS = 2;
// Played for this long before a gap is judged, so the first second of a fresh
// stream (which can stall once while it fills) is not mistaken for drift.
export const CORRECT_AFTER_PLAYING_MS = 2000;

export function nextLead(leadUsedMS: number, landingGapMS: number): number {
  return Math.min(Math.max(0, leadUsedMS + landingGapMS), CONVERTING_LEAD_CAP_MS);
}

export function followerShouldCorrect(
  localMS: number,
  expectedMS: number,
  converting: boolean,
  msSincePlaying: number,
  correctionsSoFar: number,
): boolean {
  if (!converting || correctionsSoFar >= MAX_CORRECTIONS) return false;
  if (msSincePlaying < CORRECT_AFTER_PLAYING_MS) return false;
  return Math.abs(expectedMS - localMS) > CONVERTING_CORRECT_MS;
}

export interface PeerRoomState {
  session: TogetherSession | null;
  /** When this device received `session`, by its own clock. */
  receivedAt: number;
  /** The host's server says the room is over, or was never open to us. */
  ended: boolean;
}

/*
 * usePeerRoom follows a room on a paired server, through this one.
 *
 * Joins on arrival (rejoining is harmless, which is what a reload is), polls
 * while mounted, and leaves on the way out. Leaving is best effort for the
 * same reason as locally: the host's server drops a member who stops polling
 * within ninety seconds anyway.
 *
 * Only a 404 ends it. That is the host's server saying the room is gone or
 * this person is not in it. A gateway error is a network having a bad moment,
 * and giving up on the room over one would be this side deciding the evening
 * was over.
 */
export function usePeerRoom(fingerprint: string, roomID: string | null): PeerRoomState {
  const [state, setState] = useState<PeerRoomState>({
    session: null,
    receivedAt: 0,
    ended: false,
  });

  useEffect(() => {
    if (!roomID || !fingerprint) return;
    const url = peerRoomURL(fingerprint, roomID);
    let cancelled = false;
    let over = false;

    const take = (s: TogetherSession) => {
      if (!cancelled) setState({ session: s, receivedAt: Date.now(), ended: false });
    };
    const fail = (e: unknown) => {
      if (cancelled) return;
      if (e instanceof ApiFailure && e.status === 404) {
        over = true;
        setState((prev) => ({ ...prev, ended: true }));
      }
    };

    void apiPost<TogetherSession>(`${url}/join`, {}).then(take).catch(fail);
    const timer = setInterval(() => {
      if (over) return;
      apiGet<TogetherSession>(url).then(take).catch(fail);
    }, PEER_POLL_MS);

    return () => {
      cancelled = true;
      clearInterval(timer);
      if (!over) void apiSend(`${url}/members/me`, "DELETE").catch(() => {});
    };
  }, [fingerprint, roomID]);

  return state;
}

/*
 * useOpenSessions lists the rooms on this server, for joining one.
 *
 * The join half of Watch Together had a hook and an endpoint from the start
 * and nothing that called them: the panel told people to join "from the list
 * of open sessions", and there was no list. Polled while somebody is looking,
 * because a room opens and ends without anybody here doing anything.
 */
export function useOpenSessions(enabled: boolean) {
  return useQuery({
    queryKey: ["together-sessions"],
    enabled,
    refetchInterval: enabled ? 5000 : false,
    queryFn: ({ signal }) =>
      apiGet<{ sessions: TogetherSession[] }>("/api/together", signal),
  });
}
