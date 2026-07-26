import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useItem, useSubtitles } from "@/api/hooks";
import { apiGet, apiSend } from "@/api/client";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { clock } from "@/lib/format";
import { Scrubber } from "@/components/Scrubber";
import { SubtitleMenu } from "@/components/SubtitleMenu";
import "./Player.css";

interface Decision {
  method: "direct" | "remux" | "transcode";
  reason: string;
}

// A direct/remux source is a real file with Range support, so the browser seeks
// natively. A transcode has no length and cannot be range-served: seeking means
// restarting ffmpeg at a new offset, and the displayed time is that offset plus
// the element's own clock.
function sourceURL(id: number, method: Decision["method"], offset: number): string {
  if (method === "direct") return `/api/stream/${id}`;
  const t = offset > 0 ? `?t=${Math.floor(offset)}` : "";
  return `/api/stream/${id}/transcode${t}`;
}

export function Player() {
  const { id } = useParams();
  const itemID = Number(id);
  const navigate = useNavigate();
  const { data: item } = useItem(itemID);
  const { data: subtitles } = useSubtitles(itemID);
  const tracks = subtitles ?? [];

  const videoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const decision = useRef<Decision>({ method: "direct", reason: "" });
  const transcoding = useRef(false);
  // For a transcode, the element's clock is relative to this offset.
  const offset = useRef(0);
  const startedFrom = useRef(0);

  const [playing, setPlaying] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(0);
  const [muted, setMuted] = useState(false);
  const [chromeVisible, setChromeVisible] = useState(true);
  const [note, setNote] = useState("");
  const [subKey, setSubKey] = useState<string | null>(null);
  const [subMenuOpen, setSubMenuOpen] = useState(false);

  const activeSub = tracks.find((t) => t.key === subKey && t.available) ?? null;

  // Cycle Off → each usable track → Off, for the [ and ] keys. Bitmap tracks
  // that cannot become WebVTT are skipped; they are pickable in the menu (with
  // their reason) but not worth cycling to.
  const cycleSub = useCallback(
    (dir: 1 | -1) => {
      const order: (string | null)[] = [
        null,
        ...tracks.filter((t) => t.available).map((t) => t.key),
      ];
      const idx = order.indexOf(subKey);
      const next = (idx + dir + order.length) % order.length;
      setSubKey(order[next]);
    },
    [tracks, subKey],
  );

  const close = useCallback(() => navigate(-1), [navigate]);
  useSuspendFocus();
  useBackHandler(close);

  // Total runtime: the element's own duration when it knows it (direct play),
  // otherwise the probed runtime, since a transcode never reports one.
  const totalDuration =
    duration || (item?.duration_ms ? item.duration_ms / 1000 : 0);

  const displayTime = transcoding.current ? offset.current + current : current;

  // ---- progress persistence -------------------------------------------------
  const lastSaved = useRef(0);
  const saveProgress = useCallback(
    (force = false) => {
      const now = Date.now();
      if (!force && now - lastSaved.current < 5000) return;
      lastSaved.current = now;
      const pos = transcoding.current ? offset.current + current : current;
      if (pos <= 0 || !itemID) return;
      void apiSend(`/api/items/${itemID}/progress`, "PUT", {
        position_ms: Math.floor(pos * 1000),
        watched: totalDuration ? pos / totalDuration > 0.92 : false,
      }).catch(() => {});
    },
    [current, itemID, totalDuration],
  );

  // ---- source selection + resume -------------------------------------------
  useEffect(() => {
    if (!item) return;
    const v = videoRef.current;
    if (!v) return;
    let cancelled = false;

    startedFrom.current = item.progress?.position_ms
      ? item.progress.position_ms / 1000
      : 0;

    (async () => {
      try {
        const pb = await apiGet<{ decision: Decision }>(
          `/api/items/${itemID}/playback`,
        );
        if (cancelled) return;
        decision.current = pb.decision;
      } catch {
        decision.current = { method: "direct", reason: "" };
      }
      transcoding.current = decision.current.method !== "direct";
      if (transcoding.current) {
        setNote(
          `${decision.current.method === "remux" ? "Repackaging" : "Converting"} — ${decision.current.reason}`,
        );
        offset.current = startedFrom.current;
      }
      v.src = sourceURL(itemID, decision.current.method, startedFrom.current);
      v.load();
      void v.play().catch(() => {});
    })();

    return () => {
      cancelled = true;
      // Detaching the source ends a running transcode; leaving it attached keeps
      // ffmpeg alive after the player is gone.
      v.pause();
      v.removeAttribute("src");
      v.load();
    };
    // Re-run only when the item identity changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item?.id]);

  // Final progress write when leaving.
  useEffect(() => {
    return () => saveProgress(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ---- seeking --------------------------------------------------------------
  const seekTo = useCallback(
    (target: number) => {
      const v = videoRef.current;
      if (!v) return;
      const t = Math.max(0, Math.min(target, totalDuration || target));
      if (transcoding.current) {
        offset.current = t;
        setCurrent(0);
        v.src = sourceURL(itemID, decision.current.method, t);
        v.load();
        void v.play().catch(() => {});
      } else {
        v.currentTime = t;
      }
      saveProgress(true);
    },
    [itemID, totalDuration, saveProgress],
  );

  const seekBy = useCallback(
    (delta: number) => seekTo(displayTime + delta),
    [seekTo, displayTime],
  );

  // ---- transport ------------------------------------------------------------
  const togglePlay = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    if (v.paused) void v.play().catch(() => {});
    else v.pause();
  }, []);

  const toggleMute = useCallback(() => {
    const v = videoRef.current;
    if (!v) return;
    v.muted = !v.muted;
    setMuted(v.muted);
  }, []);

  const toggleFullscreen = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen();
    else void el.requestFullscreen().catch(() => {});
  }, []);

  // ---- keyboard (transport surface owns its keys; spatial nav is suspended) --
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // While typing in the subtitle search box, keys are text — not transport.
      const ae = document.activeElement;
      if (
        ae instanceof HTMLElement &&
        (ae.tagName === "INPUT" || ae.tagName === "TEXTAREA" || ae.isContentEditable)
      ) {
        return;
      }
      switch (e.key) {
        case " ":
        case "k":
          e.preventDefault();
          togglePlay();
          break;
        case "f":
          toggleFullscreen();
          break;
        case "m":
          toggleMute();
          break;
        case "ArrowLeft":
          e.preventDefault();
          seekBy(-10);
          break;
        case "ArrowRight":
          e.preventDefault();
          seekBy(10);
          break;
        case "[":
          cycleSub(-1);
          break;
        case "]":
          cycleSub(1);
          break;
      }
      wakeChrome();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [togglePlay, toggleFullscreen, toggleMute, seekBy, cycleSub]);

  // A browser will not swap a <track> src reliably once parsed, so the active
  // track is keyed and remounted on change; here it is switched to showing.
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    for (const tt of v.textTracks) {
      tt.mode = activeSub ? "showing" : "disabled";
    }
  }, [activeSub, subKey]);

  // ---- auto-hide chrome -----------------------------------------------------
  const idleTimer = useRef<number>();
  const wakeChrome = useCallback(() => {
    setChromeVisible(true);
    window.clearTimeout(idleTimer.current);
    idleTimer.current = window.setTimeout(() => {
      if (!videoRef.current?.paused) setChromeVisible(false);
    }, 2500);
  }, []);

  useEffect(() => () => window.clearTimeout(idleTimer.current), []);

  return (
    <div
      ref={containerRef}
      className={"player" + (chromeVisible ? "" : " player--idle")}
      onMouseMove={wakeChrome}
      onClick={(e) => {
        if (e.target === videoRef.current) togglePlay();
      }}
    >
      <video
        ref={videoRef}
        className="player__video"
        playsInline
        onLoadedMetadata={(e) => {
          const v = e.currentTarget;
          if (isFinite(v.duration)) setDuration(v.duration);
          // Resume: for direct play the element carries the offset itself.
          if (!transcoding.current && startedFrom.current > 0) {
            v.currentTime = startedFrom.current;
          }
          // A transcode seek reloads the source, which re-parses the <track> at
          // its default mode. Re-assert the selection so subtitles survive a
          // seek rather than silently switching off.
          for (const tt of v.textTracks) {
            tt.mode = activeSub ? "showing" : "disabled";
          }
        }}
        onTimeUpdate={(e) => {
          setCurrent(e.currentTarget.currentTime);
          saveProgress();
        }}
        onPlay={() => {
          setPlaying(true);
          wakeChrome();
        }}
        onPause={() => {
          setPlaying(false);
          setChromeVisible(true);
          saveProgress(true);
        }}
        onEnded={() => saveProgress(true)}
      >
        {activeSub && (
          <track
            key={activeSub.key}
            kind="subtitles"
            default
            src={`/api/items/${itemID}/subtitles/${encodeURIComponent(activeSub.key)}.vtt`}
            srcLang={activeSub.language || undefined}
            label={activeSub.label}
          />
        )}
      </video>

      <div className="player__chrome">
        <div className="player__top">
          <button className="player__icon" onClick={close} aria-label="Close">
            ←
          </button>
          <span className="player__title">{item?.title}</span>
          {note && <span className="player__note">{note}</span>}
        </div>

        <div className="player__bottom">
          <Scrubber current={displayTime} duration={totalDuration} onSeek={seekTo} />
          <div className="player__controls">
            <button
              className="player__icon player__icon--play"
              onClick={togglePlay}
              aria-label={playing ? "Pause" : "Play"}
            >
              {playing ? "❚❚" : "▶"}
            </button>
            <button className="player__icon" onClick={toggleMute} aria-label="Mute">
              {muted ? "🔇" : "🔊"}
            </button>
            <span className="player__time">
              {clock(displayTime)} <span className="player__time-sep">/</span>{" "}
              {clock(totalDuration)}
            </span>

            <div className="player__subs player__icon--right">
              <button
                className={"player__icon" + (activeSub ? " is-on" : "")}
                onClick={() => setSubMenuOpen((o) => !o)}
                aria-label="Subtitles"
                aria-expanded={subMenuOpen}
              >
                CC
              </button>
              {subMenuOpen && (
                <SubtitleMenu
                  itemID={itemID}
                  itemTitle={item?.title ?? ""}
                  language="en"
                  tracks={tracks}
                  activeKey={subKey}
                  onSelect={(key) => {
                    setSubKey(key);
                    setSubMenuOpen(false);
                  }}
                />
              )}
            </div>
            <button
              className="player__icon"
              onClick={toggleFullscreen}
              aria-label="Fullscreen"
            >
              ⛶
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
