import { useState } from "react";
import { usePlayback } from "./PlaybackProvider";
import { Scrubber } from "@/components/Scrubber";
import { SubtitleMenu } from "@/components/SubtitleMenu";
import { QueuePanel } from "@/components/QueuePanel";
import {
  PrevGlyph,
  NextGlyph,
  VolumeGlyph,
  QueueGlyph,
  StopGlyph,
} from "@/components/PlayerGlyphs";

/*
 * The controls inside the pop-out window (ADR 0029).
 *
 * The point of the whole decision is that this window is ours, so the things
 * that vanished under browser picture-in-picture are here: a real scrubber over
 * the true runtime, our subtitle tracks, the queue.
 *
 * These are the *same components* the player screen uses — Scrubber,
 * SubtitleMenu, QueuePanel — not a second set that agrees with the first by
 * coincidence. The ADR names that as the cost of owning this chrome, and
 * sharing the components rather than duplicating them is how the cost is paid.
 *
 * What is deliberately not here: fullscreen (a window that is already floating
 * on top has nothing to go full-screen *into*), and picture-in-picture itself
 * (you are in it). Both would be dead controls, and the control bar's own rule
 * is that a control which cannot act is not shown.
 */
export function PopoutPlayer({ onClose }: { onClose: () => void }) {
  const pb = usePlayback();
  const [panel, setPanel] = useState<"none" | "subs" | "queue">("none");

  return (
    <div className="popout__ui">
      {/*
       * The stage. React renders it empty and never puts anything in it: the
       * media element is moved here imperatively, and the slot rule from ADR
       * 0029 applies to this container exactly as it applies to the provider's.
       * Mount a conditional sibling inside here and the next render throws in
       * the commit phase.
       */}
      <div className="popout__stage" data-popout-stage />

      <div className="popout__bar">
        <Scrubber
          current={pb.displayTime}
          duration={pb.totalDuration}
          onSeek={pb.seekTo}
        />

        <div className="popout__row">
          <span className="popout__title" title={pb.item?.title}>
            {pb.item?.title ?? "Playing"}
          </span>

          <div className="popout__controls">
            {pb.hasPrev && (
              <button
                className="popout__icon"
                onClick={pb.playPrev}
                aria-label="Previous"
              >
                <PrevGlyph size={16} />
              </button>
            )}
            <button
              className="popout__icon popout__icon--play"
              onClick={pb.togglePlay}
              aria-label={pb.playing ? "Pause" : "Play"}
            >
              {pb.playing ? "❚❚" : "▶"}
            </button>
            {pb.hasNext && (
              <button
                className="popout__icon"
                onClick={pb.playNext}
                aria-label="Next"
              >
                <NextGlyph size={16} />
              </button>
            )}

            <button
              className="popout__icon"
              onClick={pb.toggleMute}
              aria-label={pb.muted ? "Unmute" : "Mute"}
            >
              <VolumeGlyph size={16} muted={pb.muted || pb.volume === 0} />
            </button>

            {/* Subtitles are the reason this window exists at all: under
                browser PiP the cues render in the parent tab, and the CC
                button offers guessed transcription instead of the real
                tracks. */}
            {!pb.isAudio && pb.subtitles.length > 0 && (
              <button
                className={
                  "popout__icon" + (pb.activeSub ? " is-on" : "")
                }
                onClick={() =>
                  setPanel((p) => (p === "subs" ? "none" : "subs"))
                }
                aria-label="Subtitles"
                aria-expanded={panel === "subs"}
              >
                CC
              </button>
            )}

            {pb.queue.length > 1 && (
              <button
                className={"popout__icon" + (panel === "queue" ? " is-on" : "")}
                onClick={() =>
                  setPanel((p) => (p === "queue" ? "none" : "queue"))
                }
                aria-label="Queue"
                aria-expanded={panel === "queue"}
              >
                <QueueGlyph size={16} />
              </button>
            )}

            {/* Closing the window is the browser's button in the title bar,
                but stopping playback is ours, and they mean different things:
                one puts the picture back in the page, the other ends it. */}
            <button
              className="popout__icon"
              onClick={onClose}
              aria-label="Return to the page"
              title="Return to the page"
            >
              ⇱
            </button>
            <button
              className="popout__icon"
              onClick={pb.stop}
              aria-label="Stop"
            >
              <StopGlyph size={16} />
            </button>
          </div>
        </div>

        {panel === "subs" && pb.item && (
          <div className="popout__panel">
            <SubtitleMenu
              itemID={pb.itemID}
              itemTitle={pb.item.title}
              // Same default the docked subtitle picker uses; the search
              // language is a preference neither surface owns yet.
              language="en"
              tracks={pb.subtitles}
              activeKey={pb.subKey}
              onSelect={(key) => {
                pb.selectSub(key);
                setPanel("none");
              }}
            />
          </div>
        )}

        {panel === "queue" && (
          <div className="popout__panel">
            <QueuePanel
              ids={pb.playOrder}
              currentID={pb.itemID}
              onPick={(id, at) => {
                pb.playFromQueue(id, at);
                setPanel("none");
              }}
            />
          </div>
        )}
      </div>
    </div>
  );
}
