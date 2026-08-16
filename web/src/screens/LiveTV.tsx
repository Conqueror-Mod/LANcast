import { useMemo, useRef, useState } from "react";
import { useChannels } from "@/api/hooks";
import type { Channel } from "@/api/types";
import "./LiveTV.css";

/*
 * Live TV.
 *
 * Channels are not library items and this page is not a library page. There is
 * no A–Z rail, no filters and no detail view, because none of those mean
 * anything here: a channel has no year, no genre, no runtime and no synopsis —
 * it is a name, a logo and whatever happens to be on.
 *
 * What it does have is **groups**, which is the one attribute in an IPTV
 * playlist that makes six hundred channels navigable. They are the organising
 * idea of the page for that reason and not because the data happened to carry
 * them.
 *
 * The player is a plain <video> rather than the app's PlaybackProvider. That is
 * deliberate: the provider exists to keep one element alive across navigation
 * so a record keeps playing while you browse, and it is built around items with
 * positions, durations and queues. A channel has none of those, and pushing it
 * through would mean teaching the whole playback stack that some things cannot
 * be resumed, queued or shuffled. This is the smaller, honest surface.
 */
export function LiveTV() {
  const { data, isLoading } = useChannels();
  const [playing, setPlaying] = useState<Channel | null>(null);
  const [group, setGroup] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const videoRef = useRef<HTMLVideoElement>(null);

  const channels = useMemo(() => data?.channels ?? [], [data]);

  const groups = useMemo(() => {
    const seen = new Set<string>();
    for (const c of channels) if (c.group) seen.add(c.group);
    // Source order decides the group order too — an IPTV list puts its
    // interesting groups first, and alphabetising them buries them.
    return [...seen];
  }, [channels]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return channels.filter(
      (c) =>
        (!group || c.group === group) &&
        (!q || c.name.toLowerCase().includes(q)),
    );
  }, [channels, group, query]);

  return (
    <div className="browse livetv">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Live TV</h1>
        <span className="browse__count">{channels.length || ""}</span>
      </div>

      {!isLoading && channels.length === 0 && (
        <p className="browse__message">
          No channels yet. An administrator can add a channel list — an M3U from
          an IPTV provider, or from a tuner on this network — in{" "}
          <strong>Settings → Live TV</strong>.
        </p>
      )}

      {playing && (
        <div className="livetv__player">
          <video
            ref={videoRef}
            className="livetv__video"
            src={`/api/channels/${playing.id}/stream`}
            autoPlay
            controls
            playsInline
          />
          <div className="livetv__nowrow">
            <span className="livetv__now">{playing.name}</span>
            <button
              className="livetv__stop"
              onClick={() => {
                // Paused and cleared, in that order: dropping the element while
                // it is still pulling a live stream leaves the connection open
                // long enough to be noticed by a provider counting streams.
                videoRef.current?.pause();
                setPlaying(null);
              }}
            >
              Stop
            </button>
          </div>
        </div>
      )}

      {channels.length > 0 && (
        <div className="livetv__filters">
          <input
            className="livetv__search"
            placeholder="Find a channel"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Find a channel"
          />
          <div className="livetv__groups">
            <button
              className={"livetv__group" + (group === null ? " is-on" : "")}
              onClick={() => setGroup(null)}
            >
              All
            </button>
            {groups.map((g) => (
              <button
                key={g}
                className={"livetv__group" + (group === g ? " is-on" : "")}
                onClick={() => setGroup(group === g ? null : g)}
              >
                {g}
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="livetv__grid">
        {shown.map((c) => (
          <button
            key={c.id}
            className={
              "livetv__channel" + (playing?.id === c.id ? " is-playing" : "")
            }
            onClick={() => setPlaying(c)}
          >
            <span className="livetv__logo">
              {c.logo_url ? (
                // Referrer withheld: a logo lives on the provider's CDN, and
                // sending the page URL tells them which server is watching.
                <img
                  src={c.logo_url}
                  alt=""
                  loading="lazy"
                  referrerPolicy="no-referrer"
                />
              ) : (
                <span aria-hidden="true">{c.name.slice(0, 2).toUpperCase()}</span>
              )}
            </span>
            <span className="livetv__name">{c.name}</span>
            {c.group && <span className="livetv__grouptag">{c.group}</span>}
          </button>
        ))}
      </div>

      {channels.length > 0 && shown.length === 0 && (
        <p className="browse__message">No channels match that.</p>
      )}
    </div>
  );
}
