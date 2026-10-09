import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, apiPost, apiSend } from "@/api/client";
import type { TogetherAskAnswer, TogetherSession } from "@/api/types";
import { peerAskURL, peerRequestURL, peerRoomURL } from "@/playback/peerSource";
import "./AskToJoin.css";

/*
 * Asking to watch with somebody on another server (federation Phase 5).
 *
 * Offered beside a person who lets you see them and is watching something.
 * The grant that shows you the title is the right to *ask*, never the right
 * to arrive (ADR 0045 §7): the host answers in the moment, and nobody is put
 * in a room by pressing this.
 *
 * Every no is "not now". A decline, a minute of silence, a host watching a
 * film from somewhere else, and asking again too soon all read the same,
 * because a no that explains itself invites a negotiation about why. The
 * server already says only that; this does not dress it up.
 */

const POLL_MS = 2000;
// Longer than the server's minute, so the server's answer is the one shown.
const GIVE_UP_MS = 75_000;

type Phase = "idle" | "asking" | "waiting" | "joining" | "not_now" | "failed";

export function AskToJoin({
  fingerprint,
  person,
  name,
}: {
  fingerprint: string;
  /** Their account id on that server, as presence reported it. */
  person: string;
  name: string;
}) {
  const navigate = useNavigate();
  const [phase, setPhase] = useState<Phase>("idle");
  const [request, setRequest] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Into the room, then into the film it is playing, on their server.
  const enter = async (room: string) => {
    setPhase("joining");
    try {
      const s = await apiPost<TogetherSession>(`${peerRoomURL(fingerprint, room)}/join`, {});
      navigate(
        `/peers/${encodeURIComponent(fingerprint)}/item/${s.item_id}?room=${encodeURIComponent(room)}`,
      );
    } catch (e) {
      setError((e as Error).message);
      setPhase("failed");
    }
  };

  const ask = async () => {
    setPhase("asking");
    setError(null);
    try {
      const a = await apiPost<TogetherAskAnswer>(peerAskURL(fingerprint), { person });
      if (a.state === "accepted" && a.room_id) return void enter(a.room_id);
      if (a.state === "pending" && a.id) {
        setRequest(a.id);
        setPhase("waiting");
        return;
      }
      setPhase("not_now");
    } catch (e) {
      setError((e as Error).message);
      setPhase("failed");
    }
  };

  /*
   * Changing your mind. The host's server removes the request, so it leaves
   * their prompt on its next poll, and no cooldown starts: asking again is
   * allowed at once. Back to the button whether or not the cancel reached
   * them — a request nobody answers is declined within the minute anyway.
   */
  const cancel = async () => {
    const id = request;
    setRequest(null);
    setPhase("idle");
    if (!id) return;
    try {
      await apiSend(peerRequestURL(fingerprint, id), "DELETE");
    } catch {
      // Unreachable or already answered: the minute ends it either way.
    }
  };

  // Waiting for the answer. Stops when it comes, or when the minute is
  // clearly over, so a tab left open does not ask for ever.
  useEffect(() => {
    if (phase !== "waiting" || !request) return;
    const started = Date.now();
    let done = false;
    const timer = setInterval(() => {
      if (done) return;
      if (Date.now() - started > GIVE_UP_MS) {
        done = true;
        setPhase("not_now");
        return;
      }
      apiGet<TogetherAskAnswer>(peerRequestURL(fingerprint, request))
        .then((a) => {
          if (done) return;
          if (a.state === "accepted" && a.room_id) {
            done = true;
            void enter(a.room_id);
          } else if (a.state !== "pending") {
            done = true;
            setPhase("not_now");
          }
        })
        // A missed poll is a network hiccup, not an answer.
        .catch(() => {});
    }, POLL_MS);
    return () => {
      done = true;
      clearInterval(timer);
    };
    // enter is recreated each render and only ever navigates; the poll is
    // keyed on the request it is waiting for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, request, fingerprint]);

  switch (phase) {
    case "asking":
      return (
        <span className="ask-join__state" role="status">
          Asking {name}…
        </span>
      );
    case "waiting":
      return (
        <span className="ask-join__waiting">
          <span className="ask-join__state" role="status">
            Asking {name}…
          </span>
          <button type="button" className="ask-join ask-join--cancel" onClick={() => void cancel()}>
            Cancel
          </button>
        </span>
      );
    case "joining":
      return (
        <span className="ask-join__state" role="status">
          Joining…
        </span>
      );
    case "not_now":
      return (
        <span className="ask-join__state" role="status">
          Not now
        </span>
      );
    case "failed":
      return (
        <span className="ask-join__state ask-join__state--failed" role="alert">
          {error ?? "That did not go through."}
        </span>
      );
    default:
      return (
        <button type="button" className="ask-join" onClick={() => void ask()}>
          Ask to join
        </button>
      );
  }
}
