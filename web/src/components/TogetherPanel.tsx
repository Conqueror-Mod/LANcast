import { useEffect } from "react";
import { usePlayback } from "@/playback/PlaybackProvider";
import { useCurrentUser } from "@/api/hooks";
import {
  useTogether,
  useHostReporting,
  expectedPosition,
  shouldResync,
} from "@/playback/together";
import "./TogetherPanel.css";

/*
 * Watch together, from the player.
 *
 * Started here rather than from the detail page because the thing you want to
 * share is the thing you are already watching — and because the host's position
 * at the moment of starting is what everybody else joins at. Starting from a
 * poster would mean starting a room at zero for a film somebody is forty
 * minutes into.
 *
 * The asymmetry between host and follower is the whole design and it is visible
 * in this component: the host's player is the clock and this panel never
 * touches it, while a follower's player is corrected towards the room and its
 * transport controls become somebody else's.
 */
export function TogetherPanel({ onClose }: { onClose: () => void }) {
  const pb = usePlayback();
  const user = useCurrentUser();
  const t = useTogether(user?.id);

  // The host reports where they are; nothing corrects them.
  useHostReporting(t.session?.id ?? null, t.isHost, () => ({
    positionMS: pb.displayTime * 1000,
    paused: !pb.playing,
  }));

  /*
   * A follower converges on the room.
   *
   * Only when out of step by more than the tolerance: a video element seeking
   * is a visible stutter, and stuttering every two seconds to correct a quarter
   * of a second nobody can perceive is worse than the drift.
   */
  useEffect(() => {
    if (!t.session || t.isHost) return;
    const target = expectedPosition(t.session, Date.now());
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

  const members = t.session?.members ?? [];

  return (
    <div className="together" role="dialog" aria-label="Watch together">
      <div className="together__head">
        <span className="section-label">Watch together</span>
        <button className="together__x" onClick={onClose} aria-label="Close">
          ✕
        </button>
      </div>

      {!t.session && (
        <div className="together__start">
          <p className="together__lead">
            Play this with other people on this server. Everyone follows your
            position, and only you control playback.
          </p>
          <button
            className="together__go"
            onClick={() => t.start(pb.itemID, pb.displayTime * 1000)}
          >
            Start a session
          </button>
          {/* The code is what somebody reads out across a room or types into
              another device. There is no link to send: on a household server
              the other person is already signed in and looking at the list. */}
          <p className="together__hint">
            Others can join from their own player, or from the list of open
            sessions.
          </p>
        </div>
      )}

      {t.session && (
        <div className="together__live">
          <div className="together__code">
            <span className="together__codelabel">Session code</span>
            <code className="together__codevalue">{t.session.id}</code>
          </div>

          <div className="together__role">
            {t.isHost
              ? "You are hosting — your player is the one everybody follows."
              : "Following the host. Your transport controls are theirs while this is on."}
          </div>

          <ul className="together__members">
            {members.map((m) => (
              <li className="together__member" key={m.user_id}>
                <span className="together__name">{m.name}</span>
                {m.host && <span className="together__host">host</span>}
              </li>
            ))}
          </ul>

          <button className="together__leave" onClick={() => void t.leave()}>
            {t.isHost ? "End session" : "Leave session"}
          </button>
          {t.isHost && (
            // Said plainly, because it is surprising: the alternative — handing
            // the room to somebody nobody chose — is worse, and people should
            // know which one this does before they press it.
            <span className="together__note">
              Ending it stops the session for everyone.
            </span>
          )}
        </div>
      )}

      {t.error && (
        <p className="together__error" role="alert">
          {t.error}
        </p>
      )}
    </div>
  );
}
