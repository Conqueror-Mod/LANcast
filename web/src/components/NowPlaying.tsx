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
  prefsForVizKey,
  resolveMilkdrop,
  stageTier,
  vizChoices,
  vizKeyOf,
  type Prefs,
  type Stage,
  type StageTier,
} from "@/playback/prefs";
import { elementFXSupported } from "@/playback/elementEngine";
import { webgl2Supported } from "@/playback/trails";
import {
  markMilkdropPicked,
  milkdropStatusText,
  useMilkdropOffer,
  useMilkdropStatus,
  useShaderFallback,
  type MilkdropOffer,
} from "@/playback/milkdropStage";
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
  // On a phone the column starts under the switcher's rows, which live in
  // another box; it is told how many there are (NowPlaying.css).
  const rows = useStageRows();

  return (
    <>
      <StageSwitch />
      <section
        className={"np" + (rows.fx ? " np--switch" : "") + (rows.step ? " np--step" : "")}
        aria-label="Now playing"
      >
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
 * (a tier), when the stepper is the trails' or MilkDrop's row and should stay
 * in it. `milkdrop` is the stored presets' ids (useMilkdropOffer), each its
 * own stop, after the built-in ones.
 */
export function stepVisualizer(dir: 1 | -1, only?: StageTier, milkdrop: string[] = []) {
  const offered = vizChoices(webgl2Supported(), milkdrop).filter(
    (k) => !only || stageTier(prefsForVizKey(k).stage ?? "cover") === only,
  );
  const next = prefsForVizKey(cycleStage(vizKeyOf(getPrefs(), milkdrop), dir, offered));
  // A step onto MilkDrop is somebody choosing it, which is what lets it start
  // under reduced motion (milkdropStage.ts).
  if (next.stage === "milkdrop") markMilkdropPicked();
  setPrefs(next);
}

function stageLabel(p: Pick<Prefs, "stage" | "milkdrop">, milkdrop: MilkdropOffer): string {
  if (p.stage === "milkdrop") {
    const id = resolveMilkdrop(p.milkdrop, milkdrop.ids);
    return milkdrop.presets.find((m) => m.id === id)?.name ?? "MilkDrop";
  }
  return STAGES.find((s) => s.id === p.stage)?.label ?? p.stage;
}

/*
 * The stage switcher, under the stage (docs/now-playing-plan.md, steps 2 and
 * 3): the cover, one of the classic visualizers, the trails or MilkDrop, and a
 * way into the full-screen visualizer. Remembered per device with the other
 * "This device" playback preferences. Positioned from --np-art like the
 * column, because the stage it sits under is drawn in another layer.
 *
 * The five trail presets are one choice here, Trails, with the preset named
 * on a row of its own under it: eight buttons do not fit under a square that
 * is 46% of a phone's width, and which trail is a smaller question than
 * whether there are trails at all. Trails is offered only with WebGL2.
 * MilkDrop is the same shape, its presets being the server's uploads
 * (ADR 0091), and is offered only with WebGL2 and at least one stored.
 *
 * Offered only where music can be routed through Web Audio, since a
 * visualizer is an analyser on that graph. The chosen one is marked the
 * neutral way the column's tabs are: gold is focus only.
 */
/*
 * Which of the switcher's rows show: the choices wherever Web Audio can take
 * the music, and the preset's stepper under them for the trails or MilkDrop.
 * One answer for the switcher and for the column that has to start below it.
 */
export function useStageRows() {
  const [prefs] = usePrefs();
  const fx = elementFXSupported();
  const milkdrop = useMilkdropOffer(fx);
  const trails = webgl2Supported();
  const tier = stageTier(prefs.stage);
  const step = fx && ((trails && tier === 2) || (milkdrop.offered && tier === 3));
  return { fx, trails, tier, milkdrop, step };
}

/*
 * On a phone the switcher is one row under the stage, the width of the
 * screen's gutters rather than of the stage, with the full-screen button
 * outside the choices. Five choices and that button fit at 375px with ~40px
 * targets; on a narrower screen the choices scroll sideways while the button
 * stays put. Not a wrap: the column under it is in another layer and starts at
 * a height set in CSS, so the row's height must not depend on the screen's
 * width. Not a menu: the choices fit, and a menu would hide four of them
 * behind a tap.
 */
export function StageSwitch() {
  const pb = usePlayback();
  const [prefs, update] = usePrefs();
  const { fx, trails, tier, milkdrop } = useStageRows();
  if (!fx) return null;
  const choices: { id: Stage; label: string; on: boolean }[] = [
    ...STAGES.filter((s) => s.tier < 2).map((s) => ({ id: s.id, label: s.label, on: prefs.stage === s.id })),
    ...(trails ? [{ id: "tunnel" as Stage, label: "Trails", on: tier === 2 }] : []),
    ...(milkdrop.offered ? [{ id: "milkdrop" as Stage, label: "MilkDrop", on: tier === 3 }] : []),
  ];
  const pick = (id: Stage) => {
    if (id === "milkdrop") markMilkdropPicked();
    update({ stage: id });
  };
  return (
    <div className="np__stage">
      <div className="np__stage-row">
        <div className="np__stage-choices" role="radiogroup" aria-label="Stage">
          {choices.map((c) => (
            <button
              key={c.label}
              role="radio"
              aria-checked={c.on}
              className={"np__stage-btn" + (c.on ? " is-on" : "")}
              // MilkDrop remembered from another session reads as chosen but is
              // held on the cover under reduced motion; pressing it is the pick
              // that starts it.
              onClick={() => (!c.on || c.id === "milkdrop") && pick(c.id)}
            >
              {c.label}
            </button>
          ))}
        </div>
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
      {milkdrop.offered && tier === 3 && <VizStep only={3} />}
    </div>
  );
}

/*
 * The preset's name between back and forward. Under the stage it steps
 * between trails, or between MilkDrop presets; over the full-screen
 * visualizer, through all of them, as `,` and `.` do. A MilkDrop preset whose
 * shaders the server could not convert says so, as the API asks clients to:
 * it plays with butterchurn's default shaders, and may not look as its author
 * meant. So does one whose converted shaders would not compile in this
 * browser, which plays the defaults for that stage instead of drawing black.
 */
export function VizStep({ only }: { only?: StageTier }) {
  const pb = usePlayback();
  const [prefs] = usePrefs();
  const milkdrop = useMilkdropOffer(elementFXSupported());
  const status = useMilkdropStatus();
  const onMilkdrop = prefs.stage === "milkdrop";
  const shown = onMilkdrop
    ? milkdrop.presets.find((m) => m.id === resolveMilkdrop(prefs.milkdrop, milkdrop.ids))
    : undefined;
  // Converted on the server but would not compile in this browser, so it is
  // playing butterchurn's defaults for that stage (ADR 0091, step g4).
  const fellBack = useShaderFallback(shown?.id);
  // The engine would not load: no preset to step to, so the row says so and
  // offers the cover back. Picking MilkDrop again tries the download again.
  if (onMilkdrop && status === "failed") {
    return (
      <div className="np__step" role="group" aria-label="Visualizer">
        <span className="np__step-name np__step-name--failed" role="status">
          {milkdropStatusText(status)}
        </span>
        <button
          className="np__step-cover"
          onClick={() => {
            pb.setVizFull(false);
            setPrefs({ stage: "cover" });
          }}
        >
          Show the cover
        </button>
      </div>
    );
  }
  return (
    <div className="np__step" role="group" aria-label="Visualizer">
      <button
        className="np__step-btn"
        onClick={() => stepVisualizer(-1, only, milkdrop.ids)}
        aria-label="Previous visualizer"
      >
        ‹
      </button>
      <span className="np__step-name" aria-live="polite">
        {stageLabel(prefs, milkdrop)}
        {shown?.shaders === "not_converted" && <span className="np__step-note"> · shaders not converted</span>}
        {fellBack && <span className="np__step-note"> · shaders could not run here</span>}
        {onMilkdrop && status === "loading" && <span className="np__step-note"> · loading</span>}
      </span>
      <button
        className="np__step-btn"
        onClick={() => stepVisualizer(1, only, milkdrop.ids)}
        aria-label="Next visualizer"
      >
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
