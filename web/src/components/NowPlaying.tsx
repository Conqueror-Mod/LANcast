import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useItem } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import { LyricsPanel } from "@/components/LyricsPanel";
import { QueuePanel } from "@/components/QueuePanel";
import "./NowPlaying.css";

type Tab = "queue" | "lyrics";

/*
 * Now Playing: the column beside a track's cover (docs/now-playing-plan.md,
 * step 1).
 *
 * The cover itself is the provider's surface and is drawn underneath this
 * screen, so the two only line up because both size themselves from the same
 * --np-art (playback.css). This is everything the screen says about the
 * record that is not a control: what is playing, in content type rather than
 * the corner of a toolbar, and what comes next, on the page rather than behind
 * a button.
 *
 * The queue and the lyrics were popovers over the cover, and each covered the
 * other's half of the screen when opened. Here they share one column as tabs.
 * The tab is called Queue, not Up next: inside the queue, "Up next" is already
 * the lane of tracks queued by hand, and one name for two lists is the gold
 * rule's problem in words.
 */
export function NowPlaying() {
  const pb = usePlayback();
  const navigate = useNavigate();
  const item = pb.item;
  // Already in the cache: the provider fetched the album for its cover.
  const { data: album } = useItem(item?.parent_id ?? 0);

  // A single track put on with nothing behind it has no queue to show, and an
  // empty tab is a promise the column cannot keep.
  const hasQueue = pb.queue.length > 1 || pb.upNext.length > 0;
  const [chosen, setChosen] = useState<Tab>("queue");
  const tab: Tab = hasQueue ? chosen : "lyrics";

  const year = item?.year ?? album?.year ?? null;
  const albumTitle = item?.series ?? album?.title ?? null;

  return (
    <section className="np" aria-label="Now playing">
      <div className="np__head">
        <span className="section-label">Now playing</span>
        <h1 className="np__title">{item?.title ?? "Playing"}</h1>
        <p className="np__meta">
          {item?.artist && <span>{item.artist}</span>}
          {item?.artist && albumTitle && <span className="np__sep"> — </span>}
          {albumTitle &&
            (album ? (
              // To the album, leaving this playing in the corner: the same
              // thing the back arrow does, with somewhere to go.
              <button
                className="np__album"
                onClick={() => navigate(`/item/${album.id}`)}
                title={`Go to ${albumTitle}`}
              >
                {albumTitle}
              </button>
            ) : (
              <span>{albumTitle}</span>
            ))}
          {year != null && <span className="np__year"> · {year}</span>}
        </p>
      </div>

      <div className="np__tabs" role="tablist" aria-label="Beside the track">
        {hasQueue && (
          <TabButton id="queue" current={tab} onPick={setChosen}>
            Queue
          </TabButton>
        )}
        <TabButton id="lyrics" current={tab} onPick={setChosen}>
          Lyrics
        </TabButton>
      </div>

      <div className="np__panel" role="tabpanel" id={`np-panel-${tab}`}>
        {tab === "queue" ? (
          <QueuePanel
            inline
            // The order that will play, as the popover always showed it.
            ids={pb.playOrder}
            currentID={pb.itemID}
            upNext={pb.upNext}
            onDrop={pb.removeFromUpNext}
            onPick={(id, at) => pb.playFromQueue(id, at)}
          />
        ) : (
          <LyricsPanel
            inline
            itemID={pb.itemID}
            // The clock the chrome trusts; see the panel.
            atMS={Math.max(0, Math.floor(pb.displayTime * 1000))}
          />
        )}
      </div>
    </section>
  );
}

function TabButton({
  id,
  current,
  onPick,
  children,
}: {
  id: Tab;
  current: Tab;
  onPick: (t: Tab) => void;
  children: React.ReactNode;
}) {
  const on = id === current;
  return (
    <button
      role="tab"
      className={"np__tab" + (on ? " is-on" : "")}
      aria-selected={on}
      aria-controls={`np-panel-${id}`}
      onClick={() => onPick(id)}
    >
      {children}
    </button>
  );
}
