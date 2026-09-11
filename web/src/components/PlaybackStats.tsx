import { useEffect, useRef, useState } from "react";

import {
  format,
  read,
  summarise,
  type Sample,
  type Stats,
} from "@/playback/stats";
import {
  describeIncident,
  lastHLSIncident,
  type HLSIncident,
} from "@/playback/hlsIncident";

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
  /*
   * Why the segmented path was abandoned, if it was.
   *
   * Polled with everything else rather than pushed, because the fallback
   * happens in the provider and the panel may be opened minutes afterwards —
   * which is exactly when somebody goes looking for it.
   */
  const [incident, setIncident] = useState<HLSIncident | null>(null);
  const prev = useRef<Sample | null>(null);

  useEffect(() => {
    if (!video) return;
    prev.current = null;

    const tick = () => {
      setIncident(lastHLSIncident());
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
      {incident && (
        /*
         * Kept visually distinct and last. It is not a reading of what is
         * happening now — it is a record of something that already happened,
         * and reading it as a live measurement would be worse than not showing
         * it at all.
         */
        <div className="pstats__incident">
          {describeIncident(incident).map((line, i) => (
            <div className="pstats__line" key={i}>
              {line}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
