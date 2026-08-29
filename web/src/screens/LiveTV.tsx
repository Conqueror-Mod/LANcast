import { useEffect, useMemo, useRef, useState } from "react";
import {
  useAuthStatus,
  useChannels,
  useGuide,
  useChannelSchedule,
} from "@/api/hooks";
import { bufferedAhead, shouldHold, shouldStartPlayback } from "@/lib/preroll";
import { catchUpRate, lagBehindEdge } from "@/lib/liveEdge";
import {
  livePath,
  mediaCapability,
  useLiveTransport,
  type LivePath,
} from "@/lib/liveTransport";
import { attachLiveHls, OLD_SERVER } from "@/playback/attachLiveHls";
import { conversionHelp } from "@/playback/conversionAvailable";
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
  /** Whether the picture has ever actually moved. */
  const [started, setStarted] = useState(false);
  /*
   * What the MSE path actually did, said out loud.
   *
   * Three fixes for "the channel does not start on its own" shipped to a real
   * server and changed nothing on screen, because every one of them was a
   * theory about when the element becomes playable and the screen reports the
   * same picture — 0:00 — for every wrong answer. A rejected play(), a play()
   * that was never called, and metadata that never arrived are indistinguishable
   * from the sofa and were indistinguishable from here.
   *
   * This is the roadmap's own lesson from v0.8.23, arriving in a different
   * room: the instruments built that night turned a fault that had cost an
   * evening into one named in minutes. This is the live-TV version of the `i`
   * panel, and it is a feature rather than scaffolding — a channel that will
   * not start is a thing viewers meet, and "the picture never arrived" and
   * "the browser refused to start it" have completely different fixes.
   */
  const [diag, setDiag] = useState<{
    meta: boolean;
    asked: boolean;
    refused: string | null;
    ready: number;
    ahead: number;
  }>({ meta: false, asked: false, refused: null, ready: 0, ahead: 0 });
  // Whether the player is running fast to close a gap. Shown, because a speed
  // change a viewer can hear should not be a secret.
  const [catchingUp, setCatchingUp] = useState(false);
  const [group, setGroup] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const videoRef = useRef<HTMLVideoElement>(null);
  const [transport] = useLiveTransport();
  /*
   * A channel always needs ffmpeg, so this is checked before one is played
   * rather than after it fails.
   *
   * Live TV is the harshest case for the missing-tools failure. A film that
   * direct-plays still works on a server without ffmpeg, so the library looks
   * mostly fine; every channel is an ffmpeg session, so Live TV is uniformly
   * dead with nothing on screen saying why. `needsConversion` is therefore
   * always true here.
   */
  const auth = useAuthStatus().data;
  const toolsHelp = conversionHelp(
    auth?.can_convert,
    auth?.user?.role,
    "channel",
  );
  /*
   * Which path this channel actually takes.
   *
   * The setting asks; the device decides. Resolved once per render rather than
   * inside the effects, so every one of them agrees about what is feeding the
   * element — two effects disagreeing on that is how a stream ends up with both
   * an hls.js instance and a src attribute pointing at different endpoints.
   */
  const path: LivePath = useMemo(
    () => livePath(transport, mediaCapability()),
    [transport],
  );

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
    /*
     * None of this runs on the MSE path, and that is the point of the amendment
     * rather than an optimisation.
     *
     * Every line below compensates for a transport that cannot answer how much
     * media it holds. `MediaSource` answers it, so holding, re-arming and
     * playing 10% fast are not merely unnecessary there — they would fight a
     * buffering policy that is already doing the job, using `bufferedAhead`
     * readings that finally mean something. Step 6 deletes this; step 4 stops
     * it running.
     */
    if (path !== "progressive") {
      setBuffering(false);
      setCatchingUp(false);
      return;
    }
    const el = videoRef.current;
    if (!el) return;

    let timer: number | null = null;
    /*
     * Whether this element has ever got as far as playing.
     *
     * It is what separates "still filling up" from "ran dry", and those want
     * different thresholds (see REBUFFER_SECONDS). `waiting` cannot tell them
     * apart on its own — it fires plenty during the initial fill — and neither
     * can `el.paused`, which is true in both cases because we are the ones who
     * paused it.
     */
    let hasPlayed = false;

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
      /*
       * A `waiting` event is not by itself evidence of a drought.
       *
       * Measured on a real channel: it fires about once a second while the
       * element holds two minutes of media. Pausing on each one kept the player
       * paused 28% of the time and dragged playback to 0.76x — the stutter, and
       * the drift that no catch-up could outrun. The buffer decides, not the
       * event.
       */
      if (!shouldHold(bufferedAhead(el))) return;
      setBuffering(true);
      el.pause();
      const startedAt = Date.now();
      const afterDrought = hasPlayed;
      timer = window.setInterval(() => {
        if (
          !shouldStartPlayback(
            bufferedAhead(el),
            Date.now() - startedAt,
            afterDrought,
          )
        ) {
          return;
        }
        release();
        /*
         * Resumes in place, deliberately.
         *
         * An earlier version seeked to the live edge here, which is the one
         * thing this transport cannot do — see lib/liveEdge.ts. The lag a
         * drought leaves behind is given back by playing slightly faster
         * afterwards instead, which cannot strand the element.
         */
        // A rejection is left alone: a browser refusing autoplay is a policy
        // decision, and the controls are right there.
        void el.play().catch(() => {});
      }, 250);
    };

    /*
     * Close the gap by playing slightly faster, never by seeking.
     *
     * `catching` is tracked here rather than read off the element because a
     * viewer can set their own speed from the player's menu: without it, the
     * first tick after they chose 1.5x would quietly reset them to 1.0. We
     * only ever touch the rate we ourselves engaged.
     *
     * Only while playing. A paused element is somebody's decision, and its lag
     * is about to be whatever it is when they come back.
     */
    let catching = false;
    const trim = () => {
      if (el.paused) return;
      const want = catchUpRate(lagBehindEdge(el), catching);
      if (want === el.playbackRate) return;
      if (!catching && want === 1) return; // never their speed, only ours
      el.playbackRate = want;
      catching = want !== 1;
      /*
       * Say so on screen.
       *
       * Playing faster than normal is something a viewer can hear — 10% is
       * subtle enough to be mistaken for the stream being wrong rather than
       * for a correction being applied, and "the audio sounds slightly fast"
       * is exactly how it was first reported. A player that quietly changes
       * speed and says nothing turns its own fix into somebody else's bug
       * report.
       */
      setCatchingUp(catching);
    };
    // On timeupdate rather than an interval: it fires only while media is
    // actually advancing, so a paused or stalled element costs nothing.
    el.addEventListener("timeupdate", trim);

    /*
     * The element is the authority on whether it has ever played.
     *
     * Set from `playing` rather than from wherever we happen to call `play()`,
     * because the two are not the same: a `play()` can be refused by autoplay
     * policy, and `hold` can decline to hold at all when the buffer is already
     * healthy — a path on which the element is plainly playing and no `play()`
     * of ours was involved. `playing` covers both and cannot be wrong about it.
     */
    const played = () => {
      hasPlayed = true;
    };
    el.addEventListener("playing", played);

    hold();
    el.addEventListener("waiting", hold);

    return () => {
      el.removeEventListener("playing", played);
      el.removeEventListener("timeupdate", trim);
      // Leave the element at normal speed for whatever plays next, and do not
      // let the indicator outlive the channel that raised it.
      if (catching) el.playbackRate = 1;
      setCatchingUp(false);
      el.removeEventListener("waiting", hold);
      if (timer !== null) window.clearInterval(timer);
    };
  }, [playing, path]);

  /*
   * Read the element while it is not yet playing.
   *
   * Polled rather than event-driven on purpose: the question being asked is
   * "what state is it stuck in", and the events that would answer it are
   * exactly the ones that may not be firing. A reading taken on a clock cannot
   * be silent for the same reason the fault is.
   */
  useEffect(() => {
    if (!playing) return;
    setStarted(false);
    const el = videoRef.current;
    if (!el) return;
    const id = window.setInterval(() => {
      const ahead =
        el.buffered.length > 0
          ? el.buffered.end(el.buffered.length - 1) - el.currentTime
          : 0;
      setDiag((d) => ({ ...d, ready: el.readyState, ahead }));
      if (!el.paused && el.currentTime > 0) setStarted(true);
    }, 500);
    return () => window.clearInterval(id);
  }, [playing, path]);

  /*
   * Feed the element through hls.js when the MSE path is chosen.
   *
   * Only on that path. `native-hls` and `progressive` both hand the element a
   * `src` and want no library at all — the first because Safari plays the
   * playlist itself, the second because that is the transport this client
   * shipped with.
   *
   * The import inside is dynamic, so the 618 KB bundle is fetched the first
   * time somebody actually plays a channel this way and never by anybody who
   * does not. See playback/attachLiveHls.ts.
   */
  useEffect(() => {
    if (!playing || path !== "mse") return;
    const el = videoRef.current;
    if (!el) return;

    /*
     * `cancelled` guards the await, and it is load-bearing on a channel list.
     *
     * Loading the library takes a moment, and a viewer changing channel during
     * it unmounts this effect before the promise resolves. Without the guard
     * the resolved attachment binds to an element now showing a different
     * channel, and nothing ever destroys it — an orphaned hls.js pulling
     * segments for a channel nobody is watching, which is the live-TV version
     * of the leaked-session fault the server side is careful about.
     */
    let cancelled = false;
    let attached: { destroy: () => void } | null = null;

    /*
     * Press play, because on this path nothing else does.
     *
     * The element carries no `autoPlay` and nothing calls `play()` on
     * `canplay`, both deliberately — see the comment on the element. What
     * actually starts a channel is the preroll effect, after it has waited for
     * a head start. That effect does not run on the MSE path (step 4 of the
     * ADR 0013 amendment), and nothing took over the half of its job that was
     * not a guess: a channel attached perfectly and sat at 0:00 until somebody
     * pressed play.
     *
     * `loadedmetadata`, and it took two wrong answers to get here. Playing a
     * line after `attachMedia` returns does not work, because the MediaSource
     * reaches the element in a later task. Playing on hls.js's
     * `MANIFEST_PARSED` does not work either, because the manifest is loaded
     * independently of the element and can be parsed before the element has
     * anything at all. Both are milestones in the *library*; neither says
     * anything about the *element*.
     *
     * `loadedmetadata` cannot be wrong about it: it does not fire until the
     * element has media. It is also the one that was observed — the player box
     * resized to the channel's aspect ratio well before anybody pressed play,
     * which is that event, on this path, with nothing acting on it.
     *
     * No head start is added. Waiting for one is the guess hls.js replaces — it
     * holds its own buffer and knows how much it has, which is the whole
     * argument for adopting it, so a cushion measured by us would be the guess
     * coming back through a different door.
     */
    const start = () => {
      /*
       * Every outcome is recorded, including the ones that used to be silent.
       *
       * `NotAllowedError` is not an error in the ordinary sense — a browser
       * refusing autoplay is a policy decision and the controls are right
       * there — but it is emphatically worth *knowing*, because it is
       * indistinguishable on screen from a play() that never happened, and
       * that ambiguity is what let three wrong fixes past.
       */
      setDiag((d) => ({ ...d, meta: true, asked: true }));
      void el.play().catch((e: unknown) => {
        const name = e instanceof Error ? e.name : String(e);
        setDiag((d) => ({ ...d, refused: name }));
      });
    };

    setDiag({ meta: false, asked: false, refused: null, ready: 0, ahead: 0 });
    el.addEventListener("loadedmetadata", start, { once: true });

    void attachLiveHls(el, playing.id, (fatal, detail) => {
      if (!fatal) return;
      if (detail === OLD_SERVER) {
        setPlayError(
          "This server does not have improved live playback yet — it needs a newer version than 0.8.20. Turn the setting off in Settings to play channels the way this server supports.",
        );
        return;
      }
      setPlayError(
        `The channel stopped: ${detail}. It may be off the air, or the server may have run out of streams.`,
      );
    })
      .then((a) => {
        if (cancelled) {
          a.destroy();
          return;
        }
        attached = a;
      })
      .catch((e: unknown) => {
        /*
         * Failing to load the library is not a dead channel.
         *
         * The progressive path still works and is one setting away, so this
         * says which thing broke rather than reporting the channel as
         * unplayable — the two have completely different fixes.
         */
        setPlayError(
          `Could not load the HLS player (${String(e)}). Live TV playback is set to MSE in Settings; switching it back to standard will play this channel the old way.`,
        );
      });

    return () => {
      cancelled = true;
      // `once` only fires it once; it does not remove it on unmount. A channel
      // changed before the first one loaded would otherwise leave a listener
      // armed on an element now showing something else.
      el.removeEventListener("loadedmetadata", start);
      attached?.destroy();
    };
  }, [playing, path]);

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

      {/* Said above the list rather than when a channel fails, because every
          channel needs ffmpeg: without it Live TV is uniformly dead, and a
          viewer who clicks three channels and gets three black rectangles has
          learned nothing except that it does not work. */}
      {toolsHelp && (
        <p className="browse__message" role="status">
          <strong>{toolsHelp.title}.</strong> {toolsHelp.action}
        </p>
      )}

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
            /*
             * No `src` on the MSE path: hls.js owns the element there, and an
             * element with both a `src` and an attached `MediaSource` is one
             * fetching a channel twice — the progressive response and the
             * segments — with the browser free to prefer either.
             *
             * `native-hls` takes the playlist directly, which is the whole
             * reason that path is distinguished from `mse`.
             */
            src={
              path === "mse"
                ? undefined
                : path === "native-hls"
                  ? `/api/channels/${playing.id}/hls/index.m3u8`
                  : `/api/channels/${playing.id}/live`
            }
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
          {/*
           * One line, and it names everything at once.
           *
           * An earlier version gated this on the MSE path, which made its
           * silence ambiguous: "this channel is not on that path" and "play was
           * already asked for" produced exactly the same nothing. That is the
           * same fault the line exists to cure, reproduced inside the cure.
           *
           * So it is unconditional while a channel has not started, and the
           * transport is the first thing it says.
           */}
          {!started && (
            <p className="livetv__diag" role="status">
              {`${path} · metadata ${diag.meta ? "yes" : "no"} · play ${
                diag.asked ? "asked" : "not asked"
              }${diag.refused ? ` · refused ${diag.refused}` : ""} · ready ${
                diag.ready
              } · buffered ${diag.ahead.toFixed(1)}s`}
            </p>
          )}
          {playError && (
            <p className="livetv__error" role="alert">
              {playError}
            </p>
          )}
          <div className="livetv__nowrow">
            <span className="livetv__now">{playing.name}</span>
            {catchingUp && (
              <span
                className="livetv__catchup"
                title="This channel fell behind live, so it is playing slightly faster until it catches up."
              >
                Catching up
              </span>
            )}
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
                <span aria-hidden="true">
                  {c.name.slice(0, 2).toUpperCase()}
                </span>
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
