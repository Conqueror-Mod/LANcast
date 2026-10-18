import { useCallback, useEffect, useRef, useState } from "react";
import { useFocusable } from "@/focus/FocusController";
import { onRetroEvent, type RetroEvent } from "@/playback/retro";

/*
 * The game menu's Controls page (2026-10-18): which pad button and which key
 * do each thing, for this console, and changing them.
 *
 * Choosing a row asks the desktop app to wait for the next press. Whatever is
 * pressed, a pad button or a key, becomes that row's: the app takes it from
 * any other row that had it, so one control never does two things. Escape or
 * the Guide button cancels. Those two always open the menu and cannot be
 * assigned, so nobody can map themselves out of the way back.
 */

export interface ControlMapping {
  pad: Record<string, string>;
  keys: Record<string, string>;
}

declare global {
  interface Window {
    lancastRetroControls?: (platform: string) => Promise<ControlMapping>;
    lancastRetroSetControl?: (platform: string, target: string, kind: string, name: string) => Promise<ControlMapping>;
    lancastRetroResetControls?: (platform: string) => Promise<ControlMapping>;
    lancastRetroCapture?: () => Promise<boolean>;
  }
}

const LABELS: Record<string, string> = {
  up: "Up",
  down: "Down",
  left: "Left",
  right: "Right",
  b: "B (bottom button)",
  a: "A (right button)",
  y: "Y (left button)",
  x: "X (top button)",
  l: "L",
  r: "R",
  l2: "L2",
  r2: "R2",
  l3: "L3",
  r3: "R3",
  start: "Start",
  select: "Select",
  "fast-forward": "Fast-forward (hold)",
  rewind: "Rewind (hold)",
};

const DPAD = ["up", "down", "left", "right"];
const HOLD = ["fast-forward", "rewind"];

/** The buttons a console's controller has, in the order they are listed. */
export function targetsFor(platform: string): string[] {
  switch (platform) {
    case "nes":
    case "gb":
    case "gbc":
    case "sms":
      return [...DPAD, "b", "a", "start", "select", ...HOLD];
    case "gba":
      return [...DPAD, "b", "a", "l", "r", "start", "select", ...HOLD];
    case "snes":
    case "genesis":
      return [...DPAD, "b", "a", "y", "x", "l", "r", "start", "select", ...HOLD];
    default:
      return [...DPAD, "b", "a", "y", "x", "l", "r", "l2", "r2", "l3", "r3", "start", "select", ...HOLD];
  }
}

export function ControlsPage({ platform, onBack }: { platform: string; onBack: () => void }) {
  const [mapping, setMapping] = useState<ControlMapping | null>(null);
  const [waiting, setWaiting] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const waitingFor = useRef<string | null>(null);

  useEffect(() => {
    let live = true;
    void window.lancastRetroControls?.(platform).then((m) => live && setMapping(m));
    return () => {
      live = false;
    };
  }, [platform]);

  // The answer to a capture arrives as an event, not as the call's result:
  // the desktop app cannot wait on its UI thread.
  useEffect(
    () =>
      onRetroEvent((e: RetroEvent & { control?: string; name?: string }) => {
        if (e.kind !== "captured") return;
        const target = waitingFor.current;
        waitingFor.current = null;
        setWaiting(null);
        if (!target || !e.control || !e.name) return; // cancelled
        void window
          .lancastRetroSetControl?.(platform, target, e.control, e.name)
          .then(setMapping)
          .catch((err: Error) => setProblem(err.message || "That could not be assigned."));
      }),
    [platform],
  );

  const change = useCallback((target: string) => {
    setProblem(null);
    waitingFor.current = target;
    setWaiting(target);
    void window.lancastRetroCapture?.();
  }, []);

  if (!window.lancastRetroControls) {
    return (
      <>
        <div className="retro-play__heading">Controls</div>
        <p className="retro-play__hint">Controls can be changed in the LANcast desktop app.</p>
        <Row label="Back" onSelect={onBack} autoFocus />
      </>
    );
  }

  if (waiting) {
    return (
      <>
        <div className="retro-play__heading">Controls</div>
        <p className="retro-play__waiting" role="status">
          Press a button or key for {LABELS[waiting]}…
        </p>
        <p className="retro-play__hint">Escape or the Guide button cancels.</p>
      </>
    );
  }

  return (
    <>
      <div className="retro-play__heading">Controls</div>
      {mapping &&
        targetsFor(platform).map((t, i) => (
          <Row
            key={t}
            autoFocus={i === 0}
            label={`${LABELS[t]}: ${shown(mapping.pad[t])} · ${shown(mapping.keys[t])}`}
            onSelect={() => change(t)}
          />
        ))}
      {problem && (
        <p className="retro-play__hint" role="alert">
          {problem}
        </p>
      )}
      <Row
        label="Reset to defaults"
        onSelect={() => void window.lancastRetroResetControls?.(platform).then(setMapping)}
      />
      <Row label="Back" onSelect={onBack} />
      <p className="retro-play__hint">
        Pad button · key. Choose one, then press the button or key you want. Escape and the Guide button
        always open this menu.
      </p>
    </>
  );
}

function shown(name: string | undefined): string {
  return name ? name : "none";
}

function Row({ label, onSelect, autoFocus }: { label: string; onSelect: () => void; autoFocus?: boolean }) {
  const f = useFocusable(onSelect);
  const ref = useCallback(
    (el: HTMLButtonElement | null) => {
      f.ref(el);
      if (el && autoFocus) el.focus();
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [f.ref, autoFocus],
  );
  return (
    <button
      type="button"
      className="retro-play__button"
      ref={ref}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
      onClick={onSelect}
    >
      {label}
    </button>
  );
}
