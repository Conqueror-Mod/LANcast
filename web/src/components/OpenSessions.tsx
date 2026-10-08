import { useNavigate } from "react-router-dom";
import { useCurrentUser, useItem } from "@/api/hooks";
import type { TogetherSession } from "@/api/types";
import { usePlayback } from "@/playback/PlaybackProvider";
import { useTogetherRoom, useTogetherRoomIfAny } from "@/playback/TogetherProvider";
import { useOpenSessions } from "@/playback/together";
import "./OpenSessions.css";

/*
 * The rooms on this server that you could join.
 *
 * Watch Together shipped with a join endpoint, a join hook, and a panel telling
 * people to join "from the list of open sessions", and no list: nothing called
 * join, so a second person in the house could see a session code and do
 * nothing with it. This is that list, on the People page and in the panel.
 *
 * Rooms you host or are already in are left out, and so is any room whose
 * film you could not open yourself. The item is read through the ordinary
 * route, which applies your rating ceiling, so a limited profile is never
 * offered a film above it by way of somebody else's evening.
 */
export function OpenSessions({ heading }: { heading?: string }) {
  // Null only where there is no player to join with; nothing to offer there.
  const t = useTogetherRoomIfAny();
  const me = useCurrentUser();
  const { data } = useOpenSessions(!!t && !t.session);

  if (!t || t.session) return null;
  const rooms = (data?.sessions ?? []).filter(
    (r) => r.host_id !== me?.id && !r.members.some((m) => m.user_id === me?.id),
  );
  if (rooms.length === 0) return null;

  return (
    <section className="open-sessions" aria-label="Sessions you can join">
      {heading && <h2 className="open-sessions__title">{heading}</h2>}
      <ul className="open-sessions__list">
        {rooms.map((r) => (
          <OpenSession key={r.id} room={r} />
        ))}
      </ul>
    </section>
  );
}

function OpenSession({ room }: { room: TogetherSession }) {
  const t = useTogetherRoom();
  const pb = usePlayback();
  const navigate = useNavigate();
  const item = useItem(room.item_id);

  // Not a film you could open: not offered. See the component comment.
  if (!item.data) return null;

  const host = room.members.find((m) => m.host)?.name ?? "Someone";
  const title = item.data.series ? `${item.data.series} — ${item.data.title}` : item.data.title;
  const others = room.members.length - 1;

  /*
   * Open the room's film, then join. The two land in either order, which the
   * room is built to tolerate: it leaves only when a window moves *away* from
   * the room's film, never while one is still arriving at it.
   */
  const join = async () => {
    if (pb.itemID !== room.item_id) navigate(`/watch/${room.item_id}`);
    await t.join(room.id);
  };

  return (
    <li className="open-sessions__room">
      <div className="open-sessions__what">
        <span className="open-sessions__line">
          <strong>{host}</strong> is watching {title}
        </span>
        {others > 0 && (
          <span className="open-sessions__who">
            with {others === 1 ? "1 other" : `${others} others`}
          </span>
        )}
      </div>
      <button type="button" className="open-sessions__join" onClick={() => void join()}>
        Join
      </button>
    </li>
  );
}
