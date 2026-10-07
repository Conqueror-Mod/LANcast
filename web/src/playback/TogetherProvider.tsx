import { createContext, useContext, useEffect, type ReactNode } from "react";
import { useCurrentUser } from "@/api/hooks";
import { usePlayback } from "./PlaybackProvider";
import {
  useTogether,
  useHostReporting,
  expectedPosition,
  shouldResync,
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
   * A host who stops, or moves to another film, ends the room.
   *
   * While the room lived in the panel this happened by accident: leaving the
   * player unmounted it. Now that the room outlives every screen it has to be
   * said, or a room would go on advertising a film nobody is playing, and a
   * friend admitted to it would go on being allowed that film.
   */
  const sessionItem = t.session?.item_id ?? 0;
  const { isHost, leave } = t;
  useEffect(() => {
    if (isHost && sessionItem > 0 && pb.itemID !== sessionItem) void leave();
  }, [isHost, sessionItem, pb.itemID, leave]);

  /*
   * A follower converges on the room.
   *
   * Only when out of step by more than the tolerance: a video element seeking
   * is a visible stutter, and stuttering every two seconds to correct a quarter
   * of a second nobody can perceive is worse than the drift.
   */
  useEffect(() => {
    if (!t.session || t.isHost) return;
    const target = expectedPosition(t.session, t.receivedAt, Date.now());
    if (shouldResync(pb.displayTime * 1000, target)) {
      pb.seekTo(target / 1000);
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

/** The room this window is in. Must be used inside TogetherProvider. */
export function useTogetherRoom(): TogetherControls {
  const t = useContext(TogetherContext);
  if (!t) throw new Error("useTogetherRoom outside TogetherProvider");
  return t;
}
