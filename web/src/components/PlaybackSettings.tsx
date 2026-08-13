import { useEffect, useState } from "react";
import { usePlayback } from "@/playback/PlaybackProvider";
import { QUALITIES, DEFAULTS } from "@/playback/prefs";
import { SubtitleMenu } from "./SubtitleMenu";
import { audioLabel } from "./QueuePanel";
import "./PlaybackSettings.css";

/*
 * One panel for everything about *how* this is playing.
 *
 * It replaces five separate popovers hanging off the control strip — speed,
 * audio track, subtitles, and the two that were going to be added for quality
 * and subtitle appearance. Five buttons that each open a small menu is not a
 * settings surface, it is a row of unlabelled glyphs you have to open to find
 * out what they are, and it gets worse with every one added.
 *
 * The transport stays outside it. Play, seek, next and volume are things you do
 * while watching, and burying them one click deep to tidy the strip would be
 * tidying at the cost of the thing the strip is for.
 *
 * Rows are absent rather than disabled when the content cannot offer them: a
 * mono file has no audio track to choose, a song has no subtitles, and an
 * engine without setSinkId has no output to route. A control that is present
 * and does nothing reads as broken; one that is absent reads as unavailable,
 * which is the truth.
 */

const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];

// Deliberately few, and all high-contrast against the dark cue background.
// A full colour picker for subtitles is a control whose useful range is about
// six values and whose failure mode is dark grey text on a dark grey box.
const COLORS = [
  { value: "#ffffff", label: "White" },
  { value: "#f4e2b8", label: "Cream" },
  { value: "#ffe066", label: "Yellow" },
  { value: "#9fe6a0", label: "Green" },
  { value: "#8fd3ff", label: "Blue" },
];

const SIZES = [
  { value: 0.75, label: "Small" },
  { value: 1, label: "Normal" },
  { value: 1.35, label: "Large" },
  { value: 1.8, label: "Huge" },
];

interface Device {
  id: string;
  label: string;
}

/**
 * useAudioOutputs lists the output devices, or nothing at all where the engine
 * cannot route audio.
 *
 * Deliberately does not ask for microphone permission. Device *labels* are
 * hidden until a media permission has been granted — a privacy measure, since
 * the list of attached hardware is a fingerprint — so without it the entries
 * are anonymous and get numbered names. Prompting for a microphone in order to
 * name a pair of speakers is a trade this player is not going to ask anyone to
 * make; if the browser already has the permission for another reason, the real
 * names appear.
 */
function useAudioOutputs(): { devices: Device[]; supported: boolean } {
  const [devices, setDevices] = useState<Device[]>([]);
  const [supported, setSupported] = useState(false);

  useEffect(() => {
    const el = document.createElement("video") as HTMLVideoElement & {
      setSinkId?: unknown;
    };
    const can =
      typeof el.setSinkId === "function" && !!navigator.mediaDevices?.enumerateDevices;
    setSupported(can);
    if (!can) return;

    let live = true;
    const load = async () => {
      try {
        const all = await navigator.mediaDevices.enumerateDevices();
        if (!live) return;
        setDevices(
          all
            .filter((d) => d.kind === "audiooutput")
            .map((d, i) => ({
              id: d.deviceId,
              label: d.label || `Output ${i + 1}`,
            })),
        );
      } catch {
        // Enumeration refused. The row disappears rather than showing an empty
        // picker, which is the same rule the rest of this panel follows.
        if (live) setSupported(false);
      }
    };
    void load();
    // Speakers get plugged in mid-film. Without this the list is whatever was
    // attached when the panel first mounted, for the life of the page.
    navigator.mediaDevices.addEventListener?.("devicechange", load);
    return () => {
      live = false;
      navigator.mediaDevices.removeEventListener?.("devicechange", load);
    };
  }, []);

  return { devices, supported };
}

export function PlaybackSettings({ onClose }: { onClose: () => void }) {
  const pb = usePlayback();
  const { prefs, setPrefs } = pb;
  const { devices, supported: canRoute } = useAudioOutputs();
  // The subtitle picker is the existing menu, shown in place of this panel's
  // body rather than reimplemented: it carries online search and deletion, and
  // a second, simpler track list would be a second thing to keep correct.
  const [showSubs, setShowSubs] = useState(false);

  // Same rule as the standalone picker had: null means "whatever the file leads
  // with", which is still a real track and still has to show a tick.
  const defaultAudio = pb.audioTracks.find((t) => t.default) ?? pb.audioTracks[0];
  const currentAudio = pb.audioIndex ?? defaultAudio?.index;

  // Subtitle styling can only describe subtitles that are on screen. Offered
  // against "Off" it is four controls that visibly do nothing.
  const subsOn = !!pb.activeSub;

  if (showSubs) {
    return (
      <div className="pbset" role="dialog" aria-label="Subtitles">
        <SubtitleMenu
          itemID={pb.itemID}
          itemTitle={pb.item?.title ?? ""}
          language="en"
          tracks={pb.subtitles}
          activeKey={pb.subKey}
          onSelect={(key) => {
            pb.selectSub(key);
            setShowSubs(false);
          }}
        />
      </div>
    );
  }

  return (
    <div className="pbset" role="dialog" aria-label="Playback settings">
      <div className="pbset__head">
        <span className="section-label">Playback settings</span>
        <button
          className="pbset__close"
          onClick={onClose}
          aria-label="Close playback settings"
        >
          ×
        </button>
      </div>

      <div className="pbset__rows">
        {/* Quality first, because it is the one setting that changes what the
            server does rather than what this browser does with what it is
            sent — and the one whose wrong value explains a stuttering film. */}
        {!pb.isAudio && (
          <Row label="Quality">
            <select
              className="pbset__select"
              value={prefs.quality}
              onChange={(e) => setPrefs({ quality: e.target.value })}
            >
              {QUALITIES.map((q) => (
                <option key={q.id} value={q.id}>
                  {q.label}
                </option>
              ))}
            </select>
          </Row>
        )}
        {/* Said once, under the control, rather than as a warning every time it
            moves: changing quality reconnects the stream. It resumes where you
            were, so the honest description is a pause, not a restart. */}
        {!pb.isAudio && prefs.quality !== DEFAULTS.quality && (
          <p className="pbset__note">
            Capped quality re-encodes on the server. Playback reconnects at the
            current position when this changes.
          </p>
        )}

        {pb.audioTracks.length > 1 && (
          <Row label="Audio stream">
            <select
              className="pbset__select"
              value={String(currentAudio ?? "")}
              onChange={(e) => pb.selectAudio(Number(e.target.value))}
            >
              {pb.audioTracks.map((t) => (
                <option key={t.index} value={t.index}>
                  {audioLabel(t)}
                </option>
              ))}
            </select>
          </Row>
        )}

        {canRoute && devices.length > 0 && (
          <Row label="Audio device">
            <select
              className="pbset__select"
              value={prefs.audioDevice}
              onChange={(e) => setPrefs({ audioDevice: e.target.value })}
            >
              <option value="">Auto select device</option>
              {devices.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.label}
                </option>
              ))}
            </select>
          </Row>
        )}

        <Row label="Speed">
          <select
            className="pbset__select"
            value={String(pb.speed)}
            onChange={(e) => pb.setSpeed(Number(e.target.value))}
          >
            {SPEEDS.map((r) => (
              <option key={r} value={r}>
                {r === 1 ? "Normal" : `${r}×`}
              </option>
            ))}
          </select>
        </Row>

        {!pb.isAudio && (
          <>
            <Row label="Subtitles">
              <button
                className="pbset__link"
                onClick={() => setShowSubs(true)}
                aria-haspopup="menu"
              >
                {pb.activeSub?.label ?? "None"} ▾
              </button>
            </Row>

            <Row label="Subtitle colour" disabled={!subsOn}>
              <select
                className="pbset__select"
                value={prefs.subColor}
                disabled={!subsOn}
                onChange={(e) => setPrefs({ subColor: e.target.value })}
              >
                {COLORS.map((c) => (
                  <option key={c.value} value={c.value}>
                    {c.label}
                  </option>
                ))}
              </select>
            </Row>

            <Row label="Subtitle size" disabled={!subsOn}>
              <select
                className="pbset__select"
                value={String(prefs.subSize)}
                disabled={!subsOn}
                onChange={(e) => setPrefs({ subSize: Number(e.target.value) })}
              >
                {SIZES.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
            </Row>

            <Row label="Subtitle position" disabled={!subsOn}>
              <span className="pbset__slider">
                <input
                  type="range"
                  min={0}
                  max={40}
                  step={1}
                  value={prefs.subPosition}
                  disabled={!subsOn}
                  onChange={(e) =>
                    setPrefs({ subPosition: Number(e.target.value) })
                  }
                  aria-label="Subtitle position"
                />
                <span className="pbset__value">{prefs.subPosition}%</span>
              </span>
            </Row>

            {/* In seconds, signed, and shown with its sign: "subtitles are two
                seconds early" is the complaint, and a control that reads -2.0 s
                answers it directly where an unlabelled slider does not. */}
            <Row label="Subtitle offset" disabled={!subsOn}>
              <span className="pbset__slider">
                <input
                  type="range"
                  min={-10}
                  max={10}
                  step={0.25}
                  value={prefs.subOffset}
                  disabled={!subsOn}
                  onChange={(e) =>
                    setPrefs({ subOffset: Number(e.target.value) })
                  }
                  aria-label="Subtitle offset"
                />
                <span className="pbset__value">
                  {prefs.subOffset > 0 ? "+" : ""}
                  {prefs.subOffset.toFixed(2)} s
                </span>
              </span>
            </Row>
          </>
        )}

        <Row label="Auto play">
          <input
            type="checkbox"
            className="pbset__check"
            checked={prefs.autoPlay}
            onChange={(e) => setPrefs({ autoPlay: e.target.checked })}
            aria-label="Auto play the next item in the queue"
          />
        </Row>
      </div>
    </div>
  );
}

function Row({
  label,
  disabled,
  children,
}: {
  label: string;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className={"pbset__row" + (disabled ? " is-disabled" : "")}>
      <span className="pbset__label">{label}</span>
      <span className="pbset__control">{children}</span>
    </label>
  );
}
