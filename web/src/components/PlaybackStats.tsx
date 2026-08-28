import { useEffect, useRef, useState } from "react";

import {
  format,
  read,
  summarise,
  type Sample,
  type Stats,
} from "@/playback/stats";

/*
 * A small readout of what the picture is doing, over the video.
 *
 * Off unless asked for. This is a diagnostic, not a feature: it exists so that
 * "it looks laggy" can be answered in a glance instead of an evening, and a
 * permanently visible frame counter would be clutter on every film for the
 * benefit of the rare one that misbehaves.
 *
 * Polled at 1Hz. The numbers it shows change slowly and a person reads them
 * slowly; sampling per frame would cost more than the thing being measured.
 */

const POLL_MS = 1000;

export function PlaybackStats({
  video,
  onClose,
}: {
  video: HTMLVideoElement | null;
  onClose: () => void;
}) {
  const [stats, setStats] = useState<Stats | null>(null);
  const [unsupported, setUnsupported] = useState(false);
  const prev = useRef<Sample | null>(null);

  useEffect(() => {
    if (!video) return;
    prev.current = null;

    const tick = () => {
      const now = read(video);
      if (!now) {
        // The browser will not report quality. Said plainly rather than shown
        // as zeroes: "no dropped frames" is a claim, and this is its absence.
        setUnsupported(true);
        return;
      }
      setUnsupported(false);
      setStats(summarise(now, prev.current));
      prev.current = now;
    };

    tick();
    const timer = window.setInterval(tick, POLL_MS);
    return () => window.clearInterval(timer);
  }, [video]);

  return (
    <div className="pstats" role="status" aria-live="off">
      <div className="pstats__head">
        <span>Playback</span>
        <button
          className="pstats__close"
          onClick={onClose}
          aria-label="Hide statistics"
        >
          ×
        </button>
      </div>
      {unsupported ? (
        <div className="pstats__line">
          This browser does not report frame statistics.
        </div>
      ) : stats === null ? (
        <div className="pstats__line">Reading…</div>
      ) : (
        format(stats).map((line, i) => (
          <div
            className={
              "pstats__line" +
              (stats.losing && i === format(stats).length - 1
                ? " pstats__line--bad"
                : "")
            }
            key={i}
          >
            {line}
          </div>
        ))
      )}
    </div>
  );
}
