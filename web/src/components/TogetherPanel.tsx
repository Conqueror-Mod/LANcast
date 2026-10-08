import { usePlayback } from "@/playback/PlaybackProvider";
import { useTogetherRoom } from "@/playback/TogetherProvider";
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
 * A view of the room, not its owner. The room, the host's reports and the
 * follower's convergence live in TogetherProvider, so closing this panel no
 * longer ends a session, which matters now that a host can admit a friend
 * from another server without it being open.
 */
export function TogetherPanel({ onClose }: { onClose: () => void }) {
  const pb = usePlayback();
  const t = useTogetherRoom();

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
                {/* Where a friend from a paired server is watching from, so a
                    stranger's name in the list is never a mystery. */}
                {m.server && <span className="together__server">{m.server}</span>}
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
