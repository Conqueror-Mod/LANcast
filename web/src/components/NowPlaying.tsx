import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useItem } from "@/api/hooks";
import { usePlayback } from "@/playback/PlaybackProvider";
import {
  usePrefs,
  getPrefs,
  setPrefs,
  STAGES,
  cycleStage,
  stageTier,
  vizStages,
  type Stage,
} from "@/playback/prefs";
import { elementFXSupported } from "@/playback/elementEngine";
import { webgl2Supported } from "@/playback/trails";
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
    <>
      <StageSwitch />
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
    </>
  );
}

/*
 * stepVisualizer: `,` and `.`, and the arrows beside a preset's name. Round
 * the visualizers this screen can draw, never onto the cover. Within `only`
 * (a tier), when the stepper is the trails row and should stay in it.
 */
export function stepVisualizer(dir: 1 | -1, only?: 1 | 2) {
  const offered = vizStages(webgl2Supported()).filter((s) => !only || stageTier(s) === only);
  setPrefs({ stage: cycleStage(getPrefs().stage, dir, offered) });
}

function stageLabel(id: Stage): string {
  return STAGES.find((s) => s.id === id)?.label ?? id;
}

/*
 * The stage switcher, under the stage (docs/now-playing-plan.md, steps 2 and
 * 3): the cover, one of the classic visualizers, or the trails, and a way into
 * the full-screen visualizer. Remembered per device with the other "This
 * device" playback preferences. Positioned from --np-art like the column,
 * because the stage it sits under is drawn in another layer.
 *
 * The five trail presets are one choice here, Trails, with the preset named
 * on a row of its own under it: eight buttons do not fit under a square that
 * is 46% of a phone's width, and which trail is a smaller question than
 * whether there are trails at all. Trails is offered only with WebGL2.
 *
 * Offered only where music can be routed through Web Audio, since a
 * visualizer is an analyser on that graph. The chosen one is marked the
 * neutral way the column's tabs are: gold is focus only.
 */
function StageSwitch() {
  const pb = usePlayback();
  const [prefs, update] = usePrefs();
  if (!elementFXSupported()) return null;
  const trails = webgl2Supported();
  const tier = stageTier(prefs.stage);
  const choices: { id: Stage; label: string; on: boolean }[] = [
    ...STAGES.filter((s) => s.tier < 2).map((s) => ({ id: s.id, label: s.label, on: prefs.stage === s.id })),
    ...(trails ? [{ id: "tunnel" as Stage, label: "Trails", on: tier === 2 }] : []),
  ];
  return (
    <div className="np__stage">
      <div className="np__stage-row" role="radiogroup" aria-label="Stage">
        {choices.map((c) => (
          <button
            key={c.label}
            role="radio"
            aria-checked={c.on}
            className={"np__stage-btn" + (c.on ? " is-on" : "")}
            onClick={() => !c.on && update({ stage: c.id })}
          >
            {c.label}
          </button>
        ))}
        <button
          className="np__stage-btn np__stage-full"
          onClick={() => pb.setVizFull(true)}
          aria-label="Full-screen visualizer"
          title="Full-screen visualizer (V)"
        >
          ⛶
        </button>
      </div>
      {trails && tier === 2 && <VizStep only={2} />}
    </div>
  );
}

/*
 * The preset's name between back and forward. Under the stage it steps
 * between trails; over the full-screen visualizer, through all of them, as
 * `,` and `.` do.
 */
export function VizStep({ only }: { only?: 1 | 2 }) {
  const [prefs] = usePrefs();
  return (
    <div className="np__step" role="group" aria-label="Visualizer">
      <button className="np__step-btn" onClick={() => stepVisualizer(-1, only)} aria-label="Previous visualizer">
        ‹
      </button>
      <span className="np__step-name" aria-live="polite">
        {stageLabel(prefs.stage)}
      </span>
      <button className="np__step-btn" onClick={() => stepVisualizer(1, only)} aria-label="Next visualizer">
        ›
      </button>
    </div>
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
