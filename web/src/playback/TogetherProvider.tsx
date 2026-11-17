import { createContext, useContext, useEffect, useRef, type ReactNode } from "react";
import { useCurrentUser } from "@/api/hooks";
import { usePlayback } from "./PlaybackProvider";
import {
  useTogether,
  useHostReporting,
  expectedPosition,
  followerShouldSeek,
  followerSeekTarget,
  followerShouldCorrect,
  nextLead,
  CONVERTING_DEFAULT_LEAD_MS,
  type TogetherControls,
} from "./together";

/*
 * The room this window is in, above every screen.
 *
 * It lived inside TogetherPanel, which meant a room existed exactly as long
 * as that panel was open: closing it stopped the polling and the host's
 * reports, and the room died within ninety seconds. That was tolerable while
 * the only way into a room was the panel itself. It stopped being tolerable
 * with federation Phase 5, where the host says yes to a friend from a prompt
 * that can appear on any screen. A yes that depended on a panel being open
 * would admit somebody into a room that was about to end.
 *
 * So the room, the host's reporting and the follower's convergence live here,
 * inside PlaybackProvider because the latter two read the player, and
 * TogetherPanel is only a view of it.
 */

const TogetherContext = createContext<TogetherControls | null>(null);

export function TogetherProvider({ children }: { children: ReactNode }) {
  const pb = usePlayback();
  const user = useCurrentUser();
  const t = useTogether(user?.id);

  // The host reports where they are; nothing corrects them.
  useHostReporting(t.session?.id ?? null, t.isHost, () => ({
    positionMS: pb.displayTime * 1000,
    paused: !pb.playing,
  }));

  /*
   * Playing something other than the room's film is leaving the room.
   *
   * While the room lived in the panel this happened by accident: leaving the
   * player unmounted it. Now that the room outlives every screen it has to be
   * said. For a host, or the room would go on advertising a film nobody is
   * playing and a friend admitted to it would go on being allowed that film
   * (a host leaving ends the room). For a follower, or the convergence below
   * would seek and pause whatever they had moved on to, to the room's position
   * in a different film.
   */
  /*
   * Moving *away*, not merely being elsewhere. Joining from another screen
   * opens the room's film and joins at once, and the two land in either order;
   * judged by mismatch alone, the person would be put out of the room for the
   * moment their player had not yet caught up. So the rule waits until this
   * window has been on the room's film, and fires when it leaves it.
   */
  const sessionItem = t.session?.item_id ?? 0;
  const sessionID = t.session?.id ?? "";
  const { leave } = t;
  const onRoomFilm = useRef(false);
  useEffect(() => {
    onRoomFilm.current = false;
  }, [sessionID]);
  useEffect(() => {
    if (sessionItem <= 0) return;
    if (pb.itemID === sessionItem) {
      onRoomFilm.current = true;
      return;
    }
    if (onRoomFilm.current) void leave();
  }, [sessionItem, pb.itemID, leave]);

  /*
   * A follower converges on the room.
   *
   * Only when out of step by more than the tolerance: a video element seeking
   * is a visible stutter, and stuttering every two seconds to correct a quarter
   * of a second nobody can perceive is worse than the drift.
   *
   * The rules are the ones a follower on another server uses (together.ts,
   * PeerPlayer), because a converted stream here has the same shape as one
   * there. A seek on it restarts the conversion, and until the first frame
   * arrives displayTime reads the seek target while the room moves on. Judged
   * by the bare 1.5 s rule on every poll, a conversion slower than that to
   * start was thrown away and restarted every two seconds, and the film never
   * began (R14-4). So: wait for the picture after a seek, use the wider
   * tolerance while converting, aim ahead by the start-up last measured, and
   * close a landing gap with at most two short corrections.
   *
   * Position is always displayTime — the converted stream's offset plus the
   * element's clock — never the element's clock alone, which restarts at zero
   * with every converted seek.
   */
  const lastSeek = useRef(0);
  const startedSinceSeek = useRef(true);
  const leadMS = useRef(CONVERTING_DEFAULT_LEAD_MS);
  const leadUsed = useRef(0);
  const playingSince = useRef(0);
  const corrections = useRef(0);
  useEffect(() => {
    lastSeek.current = 0;
    startedSinceSeek.current = true;
    leadMS.current = CONVERTING_DEFAULT_LEAD_MS;
    leadUsed.current = 0;
    playingSince.current = 0;
    corrections.current = 0;
  }, [sessionID]);
  // The picture arriving after a seek: loading, which a converted seek puts
  // up, has come down again. (Keyed on loading, so this runs only when it
  // changes; the seek effect below is declared after it and runs after it in
  // a shared commit, so a seek is never judged landed by the render it began.) Where it landed against the room teaches the next lead.
  const sessionRef = useRef({ session: t.session, receivedAt: t.receivedAt });
  sessionRef.current = { session: t.session, receivedAt: t.receivedAt };
  const posRef = useRef(pb.displayTime);
  posRef.current = pb.displayTime;
  useEffect(() => {
    if (startedSinceSeek.current) return;
    if (pb.loading) return;
    startedSinceSeek.current = true;
    playingSince.current = Date.now();
    const { session, receivedAt } = sessionRef.current;
    if (pb.converting && session && lastSeek.current > 0) {
      const gap = expectedPosition(session, receivedAt, Date.now()) - posRef.current * 1000;
      leadMS.current = nextLead(leadUsed.current, gap);
    }
  }, [pb.loading, pb.converting]);

  useEffect(() => {
    if (!t.session || t.isHost) return;
    // Never steer a different film to this room's position.
    if (pb.itemID !== t.session.item_id) return;
    const now = Date.now();
    const local = pb.displayTime * 1000;
    const expected = expectedPosition(t.session, t.receivedAt, now);
    // Converted, for these rules, means a seek here would restart the
    // conversion. Inside a finished remux it moves the element's clock and
    // lands at once, so it is judged like direct play: the narrow tolerance,
    // no wait for a picture, no lead. Before this, every catch-up seek threw
    // away a finished remux and waited ~25 s for another (ledger row 6).
    const converting = !!pb.converting && pb.seekRestarts(expected / 1000);
    const far = followerShouldSeek(
      local,
      expected,
      converting,
      now - lastSeek.current,
      startedSinceSeek.current,
    );
    const close =
      !far &&
      startedSinceSeek.current &&
      followerShouldCorrect(local, expected, converting, now - playingSince.current, corrections.current);
    if (far || close) {
      if (close) corrections.current++;
      lastSeek.current = now;
      // A direct-play seek lands at once; only a conversion is waited on.
      startedSinceSeek.current = !converting;
      leadUsed.current = converting ? leadMS.current : 0;
      pb.seekTo(followerSeekTarget(expected, converting, leadUsed.current) / 1000);
    }
    // Play state follows too, or a follower who was paused when they joined
    // stays paused while everybody else watches.
    if (t.session.paused && pb.playing) pb.togglePlay();
    if (!t.session.paused && !pb.playing) pb.togglePlay();
    // Deliberately keyed on the session only. Including displayTime would run
    // this on every frame of playback and fight the element for control.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [t.session, t.isHost]);

  return <TogetherContext.Provider value={t}>{children}</TogetherContext.Provider>;
}

/*
 * The room, or null outside a TogetherProvider. For a component that only
 * *offers* rooms, like the open-sessions list on People, and is reasonable to
 * render where there is no player: a screen rendered on its own in a test,
 * for one. Anything that acts on the room uses useTogetherRoom.
 */
export function useTogetherRoomIfAny(): TogetherControls | null {
  return useContext(TogetherContext);
}

/** The room this window is in. Must be used inside TogetherProvider. */
export function useTogetherRoom(): TogetherControls {
  const t = useContext(TogetherContext);
  if (!t) throw new Error("useTogetherRoom outside TogetherProvider");
  return t;
}
