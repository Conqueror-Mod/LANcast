import { useEffect, useMemo, useRef, useState } from "react";
import { useChannels, useGuide, useChannelSchedule } from "@/api/hooks";
import { bufferedAhead, shouldStartPlayback } from "@/lib/preroll";
import {
  catchUpTarget,
  lagBehindEdge,
  liveEdge,
  resumeTarget,
} from "@/lib/liveEdge";
import type { Channel, Program } from "@/api/types";
import "./LiveTV.css";

/*
 * Clock formatting for a schedule.
 *
 * Built from the local components of a Date the browser made from a unix
 * second, never from an ISO string sliced up — an ISO string is UTC, and a
 * guide rendered in UTC puts the evening's television at the wrong hour for
 * most of the world and shifts by one more every summer.
 */
function clock(unix: number): string {
  return new Date(unix * 1000).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

// How far through a programme we are, 0–1. Live television's only progress bar:
// there is no position to resume, just how much of this you have missed.
function progressOf(p: Program, now: number): number {
  const span = p.stop_at - p.start_at;
  if (span <= 0) return 0;
  return Math.min(1, Math.max(0, (now - p.start_at) / span));
}

function episodeLabel(p: Program): string | null {
  if (!p.season && !p.episode) return null;
  if (p.season && p.episode) {
    return `S${String(p.season).padStart(2, "0")}E${String(p.episode).padStart(2, "0")}`;
  }
  return p.episode ? `Episode ${p.episode}` : `Series ${p.season}`;
}

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
  /*
   * The guide, for the whole page in one request.
   *
   * `now`/`next` for every channel that has listings, keyed by channel id —
   * which is why a tile can say what is on without the page knowing anything
   * about schedules. A channel absent from this map has no guide at all, and
   * that is shown as nothing rather than as "no information": a tile that says
   * "unknown" six hundred times is noise, and the absence is already legible.
   */
  const guide = useGuide();
  const [playing, setPlaying] = useState<Channel | null>(null);
  const [playError, setPlayError] = useState<string | null>(null);
  const [buffering, setBuffering] = useState(false);
  const [group, setGroup] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const videoRef = useRef<HTMLVideoElement>(null);

  /*
   * Hold playback until there is a head start — at the start, and again every
   * time the stream runs dry.
   *
   * Polled rather than driven by `progress` events: those fire on the source's
   * schedule, and the source here goes silent for seconds at a time — which is
   * exactly when the deadline needs to be able to fire. A quarter-second timer
   * is cheap and cannot be starved by the thing it is measuring.
   *
   * The re-arm on `waiting` is the half that is easy to leave out, and leaving
   * it out is why a channel stutters for ever once it has stuttered once. The
   * cushion is spent, not borrowed: playback runs at the rate the source
   * arrives, so nothing rebuilds it. Chromium resumes on its own at
   * HAVE_FUTURE_DATA — the exact "first burst arrived" condition preroll.ts
   * measured as too little — and every later gap then reaches the decoder,
   * which reads as judder rather than as buffering because no spinner appears.
   *
   * Pausing while it refills is load-bearing. An element left playing drains
   * the little it has, fires `waiting` again, and holds against a buffer that
   * the play head is eating as fast as it fills.
   */
  useEffect(() => {
    if (!playing) {
      setBuffering(false);
      return;
    }
    const el = videoRef.current;
    if (!el) return;

    let timer: number | null = null;

    /*
     * Move the play head, and never let failing to do so matter.
     *
     * Setting currentTime on a media element can throw — a stream the browser
     * considers unseekable raises rather than declining — and the resume below
     * does this immediately before calling play(). An unguarded assignment
     * therefore turns "the catch-up did not work" into "the channel does not
     * start", which is a far worse failure than the drift it was fixing.
     *
     * Staying near the live edge is an improvement on watching from a minute
     * back. It is not worth playback.
     */
    const seekTo = (v: HTMLVideoElement, to: number | null) => {
      if (to === null) return;
      try {
        v.currentTime = to;
      } catch {
        // Unseekable, or seeking somewhere it will not go. Keep playing.
      }
    };

    const release = () => {
      if (timer !== null) {
        window.clearInterval(timer);
        timer = null;
      }
      setBuffering(false);
    };

    /*
     * hold waits for the cushion, then plays.
     *
     * Re-entrant by design: `waiting` can fire while a hold is already running,
     * and restarting the clock then would push the deadline out for ever on a
     * channel that keeps stalling.
     */
    const hold = () => {
      if (timer !== null) return;
      setBuffering(true);
      el.pause();
      const startedAt = Date.now();
      timer = window.setInterval(() => {
        if (!shouldStartPlayback(bufferedAhead(el), Date.now() - startedAt)) {
          return;
        }
        release();
        /*
         * Resume near the edge, not where the drought stopped us.
         *
         * This is the line that stops the drift accumulating. Resuming in
         * place meant every stall cost lag permanently, and on a bursty
         * provider that is several times a minute — which is how a channel
         * ends up a minute behind reality without anything ever being slow.
         */
        seekTo(el, resumeTarget(el.currentTime, liveEdge(el)));
        // A rejection is left alone: a browser refusing autoplay is a policy
        // decision, and the controls are right there.
        void el.play().catch(() => {});
      }, 250);
    };

    /*
     * Catch up when the play head has drifted, stall or no stall.
     *
     * The resume above handles the common cause; this handles the rest — a
     * burst that outruns playback, a tab throttled in the background, a
     * machine that slept. Without it the gap only ever grows, because a
     * progressive fMP4 has no window and nothing else has an opinion about
     * where the play head should be.
     *
     * Only while playing. A paused element is somebody's decision, and yanking
     * it forward would override that; the correction happens on the next tick
     * after they press play, which is the right moment for a *live* channel to
     * return to live.
     */
    const catchUp = () => {
      if (el.paused || el.seeking) return;
      seekTo(el, catchUpTarget(el.currentTime, liveEdge(el), lagBehindEdge(el)));
    };
    // On timeupdate rather than an interval: it fires only while media is
    // actually advancing, so a paused or stalled element costs nothing.
    el.addEventListener("timeupdate", catchUp);

    hold();
    el.addEventListener("waiting", hold);

    return () => {
      el.removeEventListener("timeupdate", catchUp);
      el.removeEventListener("waiting", hold);
      if (timer !== null) window.clearInterval(timer);
    };
  }, [playing]);

  const channels = useMemo(() => data?.channels ?? [], [data]);
  const nowNext = guide.data?.channels ?? {};
  // The server's idea of "now", not the browser's. A machine with a skewed
  // clock would otherwise draw every progress bar in the wrong place, and the
  // server is the one that decided which programme is current.
  const at = guide.data?.at ?? Math.floor(Date.now() / 1000);

  const groups = useMemo(() => {
    const seen = new Set<string>();
    for (const c of channels) if (c.group) seen.add(c.group);
    // Source order decides the group order too — an IPTV list puts its
    // interesting groups first, and alphabetising them buries them.
    return [...seen];
  }, [channels]);

  /*
   * Search covers what is on as well as what the channel is called.
   *
   * "Is the football on anywhere" is the question a guide exists to answer, and
   * a search that only reads channel names cannot answer it. Limited to the
   * current and next programme because that is what the client holds — a search
   * across the whole fortnight is a server query, and a different feature.
   */
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return channels.filter((c) => {
      if (group && c.group !== group) return false;
      if (!q) return true;
      if (c.name.toLowerCase().includes(q)) return true;
      const entry = nowNext[String(c.id)];
      return (
        !!entry &&
        (entry.now.title.toLowerCase().includes(q) ||
          !!entry.next?.title.toLowerCase().includes(q))
      );
    });
  }, [channels, group, query, nowNext]);

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
            /*
             * The ffmpeg route, not the raw relay.
             *
             * `/stream` hands the provider's bytes through untouched, which
             * plays in Safari and nowhere else: most channels are HLS carrying
             * MPEG-TS and Chromium decodes neither. `/live` puts the same
             * source through the pipeline that already exists for files and
             * emits fragmented MP4, which every browser plays — usually by
             * copying both streams rather than re-encoding them, because
             * nearly every channel is already H.264 and AAC.
             */
            src={`/api/channels/${playing.id}/live`}
            /*
             * No `autoPlay`, and no play() on `canplay`.
             *
             * `canplay` fires at HAVE_FUTURE_DATA, which on a bursty live source
             * means only "the first burst arrived" — so playback began with
             * under a second in hand and ran dry at the next silence. A real
             * source measured five-second gaps between bursts (see
             * lib/preroll.ts), which is HLS segment pacing arriving verbatim.
             *
             * The effect below waits for a head start instead.
             */
            preload="auto"
            controls
            playsInline
            /*
             * A failed channel says why.
             *
             * This is not a rare case and it is worth naming rather than
             * leaving as a black rectangle: most IPTV channels are HLS carrying
             * MPEG-TS segments, and Chromium decodes neither natively —
             * `canPlayType("video/mp2t")` answers with an empty string. Safari
             * plays HLS; nothing else does without hls.js, which ADR 0013
             * deliberately refuses to vendor.
             *
             * So the honest interface is one that explains the failure instead
             * of implying the channel is broken.
             */
            onError={() =>
              setPlayError(
                "That channel could not be played. The server converts channels for the browser, so this usually means the source is unreachable, the subscription has lapsed, or ffmpeg is missing.",
              )
            }
            onLoadedData={() => setPlayError(null)}
          />
          {buffering && (
            <p className="livetv__buffering" role="status">
              Buffering…
            </p>
          )}
          {playError && (
            <p className="livetv__error" role="alert">
              {playError}
            </p>
          )}
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
          <ChannelSchedule channel={playing} at={at} />
        </div>
      )}

      {channels.length > 0 && (
        <div className="livetv__filters">
          <input
            className="livetv__search"
            placeholder="Find a channel or programme"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Find a channel or programme"
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
            onClick={() => {
              setPlayError(null);
              setPlaying(c);
            }}
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
            <span className="livetv__body">
              <span className="livetv__name">{c.name}</span>
              {(() => {
                const entry = nowNext[String(c.id)];
              // No listings: nothing, rather than "no information". A tile that
              // says "unknown" six hundred times is noise, and the absence of a
              // strapline already reads as absence.
                if (!entry) {
                  return c.group ? (
                    <span className="livetv__grouptag">{c.group}</span>
                  ) : null;
                }
                return (
                  <>
                    <span className="livetv__on">{entry.now.title}</span>
                    <span className="livetv__bar" aria-hidden="true">
                      <span
                        className="livetv__barfill"
                        style={{ width: `${progressOf(entry.now, at) * 100}%` }}
                      />
                    </span>
                    <span className="livetv__times">
                      {clock(entry.now.start_at)}–{clock(entry.now.stop_at)}
                      {entry.next && ` · then ${entry.next.title}`}
                    </span>
                  </>
                );
              })()}
            </span>
          </button>
        ))}
      </div>

      {channels.length > 0 && shown.length === 0 && (
        <p className="browse__message">No channels match that.</p>
      )}
    </div>
  );
}

/*
 * The schedule for the channel being watched.
 *
 * Under the player rather than in a grid across every channel, and that is the
 * design decision here. A full EPG grid — channels down, hours across — is what
 * a television does, and it is the right shape for a remote control and the
 * wrong one for a browser on a laptop: it needs horizontal scrolling, a time
 * ruler, and a column width that makes a three-minute news bulletin unreadable.
 *
 * What somebody actually asks a guide, in front of a channel they are already
 * watching, is "what is this, and what is after it". That is a list, and a list
 * costs one request for one channel rather than a fortnight for six hundred.
 */
function ChannelSchedule({ channel, at }: { channel: Channel; at: number }) {
  /*
   * Not asked for at all when the channel has no `tvg-id`.
   *
   * The hook has to be called — hooks cannot sit behind the early return below
   * — so the condition goes into its argument instead. Without it the page
   * requests a schedule it already knows is empty, every time somebody starts a
   * channel from a playlist that carries no ids, which on a six-hundred channel
   * list is most of them.
   */
  const { data, isLoading } = useChannelSchedule(
    channel.tvg_id ? channel.id : null,
  );
  const programs = data?.programs ?? [];

  // A channel with no tvg-id can never have listings, and saying so is the
  // difference between a feature that looks broken and one that is explaining
  // a limit of the playlist.
  if (!channel.tvg_id) {
    return (
      <p className="livetv__nolistings">
        No listings: this channel carries no <code>tvg-id</code>, so the guide
        cannot say which channel it is.
      </p>
    );
  }
  if (isLoading) return null;
  if (programs.length === 0) {
    return <p className="livetv__nolistings">No listings for this channel.</p>;
  }

  return (
    <ol className="livetv__schedule">
      {programs.map((p) => {
        const onNow = p.start_at <= at && p.stop_at > at;
        const ep = episodeLabel(p);
        return (
          <li
            key={p.id}
            className={"livetv__slot" + (onNow ? " is-now" : "")}
            aria-current={onNow ? "true" : undefined}
          >
            <span className="livetv__slottime">{clock(p.start_at)}</span>
            <span className="livetv__slotbody">
              <span className="livetv__slottitle">
                {p.title}
                {ep && <span className="livetv__slotep">{ep}</span>}
              </span>
              {p.description && (
                <span className="livetv__slotdesc">{p.description}</span>
              )}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
