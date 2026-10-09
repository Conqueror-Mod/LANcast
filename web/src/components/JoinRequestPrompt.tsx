import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost, apiSend } from "@/api/client";
import type { TogetherRequest, TogetherSession } from "@/api/types";
import { usePlayback } from "@/playback/PlaybackProvider";
import { useTogetherRoom } from "@/playback/TogetherProvider";
import { clientNote } from "@/lib/clientNote";
import "./JoinRequestPrompt.css";

/*
 * Somebody on a paired server asking to watch what you are watching
 * (federation Phase 5, ADR 0045 §7).
 *
 * The host answers **in the moment**, which is the consent the whole design
 * rests on: being seen is not being joinable, and nobody arrives in a room
 * because a setting was left on. So this is a prompt, not a notification, and
 * it appears only while something of yours is playing, on whatever screen you
 * are on.
 *
 * Silence is a no. The server declines a request nobody answered within its
 * minute, and this shows the time left so the prompt does not simply vanish.
 * The countdown is measured from when this client first saw the request,
 * against the server's own stated length: comparing this device's clock with
 * the server's `expires_at` is exactly the mistake the room's `age_ms` exists
 * to avoid.
 */

const POLL_MS = 3000;

export function JoinRequestPrompt() {
  const pb = usePlayback();
  const t = useTogetherRoom();
  const qc = useQueryClient();
  // Only a film or an episode of yours: presence names nothing else, so there
  // is nothing anybody could have asked to join.
  const watching = pb.itemID > 0 && !pb.isAudio;

  const requests = useQuery({
    queryKey: ["together-requests"],
    enabled: watching,
    refetchInterval: watching ? POLL_MS : false,
    queryFn: ({ signal }) =>
      apiGet<{ requests: TogetherRequest[] }>("/api/together/requests", signal),
  });

  const req = watching ? requests.data?.requests?.[0] : undefined;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // When this client first saw each request, for the countdown.
  const seen = useRef(new Map<string, number>());
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!req) return;
    if (!seen.current.has(req.id)) {
      seen.current.set(req.id, Date.now());
      // Into the desktop log, so "I saw nothing" can be told apart from
      // "it was drawn and something covered it".
      clientNote("info", "together", `join request shown: ${req.name}${req.server ? " on " + req.server : ""}`);
    }
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [req]);

  // A new request starts with no stale error from the last one.
  useEffect(() => setError(null), [req?.id]);

  if (!req) return null;

  const length = Math.max(0, req.expires_at - req.created_at);
  const since = (now - (seen.current.get(req.id) ?? now)) / 1000;
  const left = Math.max(0, Math.round(length - since));

  // The prompt answers a list; after answering, the list is what changed.
  const settle = () => qc.invalidateQueries({ queryKey: ["together-requests"] });

  /*
   * Yes, into this window's room, opening one around the film when there is
   * none. That is the common case: a friend sees you watching alone.
   *
   * A window following somebody else's room cannot admit anybody to it; only
   * the host's answer counts, and this person is not the host.
   */
  const accept = async () => {
    setBusy(true);
    setError(null);
    try {
      if (t.session && !t.isHost) {
        setError("You are following someone else's session, so only its host can let them in.");
        return;
      }
      const room = t.session ?? (await t.start(pb.itemID, pb.displayTime * 1000));
      if (!room) {
        setError("A session could not be started for this film.");
        return;
      }
      const joined = await apiPost<TogetherSession>(
        `/api/together/requests/${encodeURIComponent(req.id)}/accept`,
        { room_id: room.id },
      );
      t.adopt(joined);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
      void settle();
    }
  };

  const decline = async () => {
    setBusy(true);
    try {
      await apiSend(`/api/together/requests/${encodeURIComponent(req.id)}/decline`, "POST");
    } catch {
      // Already answered or timed out: either way the answer is no, which is
      // what was pressed.
    } finally {
      setBusy(false);
      void settle();
    }
  };

  return (
    <div className="join-prompt" role="alertdialog" aria-labelledby="join-prompt-title">
      <p className="join-prompt__title" id="join-prompt-title">
        <strong>{req.name}</strong>
        {req.server ? ` on ${req.server}` : ""} would like to watch this with you
      </p>
      <p className="join-prompt__detail">
        They will follow your playback. Only you control it.
      </p>
      <div className="join-prompt__actions">
        <button
          type="button"
          className="join-prompt__yes"
          disabled={busy}
          onClick={() => void accept()}
        >
          Let them join
        </button>
        <button
          type="button"
          className="join-prompt__no"
          disabled={busy}
          onClick={() => void decline()}
        >
          Not now
        </button>
        <span className="join-prompt__left" aria-label={`${left} seconds to answer`}>
          {left}s
        </span>
      </div>
      {error && (
        <p className="join-prompt__error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
