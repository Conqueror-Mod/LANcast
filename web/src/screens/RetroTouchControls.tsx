import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { dpadDirections, padLayout, sendPadKey, type PadButton, type PadControl } from "@/playback/touchPad";
import "./RetroTouchControls.css";

/*
 * The on-screen controls a phone plays with (ADR 0077 Decision 6): a d-pad on
 * the left, the console's face buttons on the right, its shoulders above them
 * and Start and Select between. Portrait puts them under the game; landscape
 * lays them over the black bars either side of it (RetroTouchControls.css).
 *
 * Each control holds its key from the touch landing to the touch lifting
 * (touchPad.ts says why). Pointer capture keeps a press with the thumb that
 * made it, so sliding off a button does not leave its key held, and several
 * thumbs press several controls at once.
 *
 * Anything that interrupts a touch (the browser cancelling it, the page being
 * hidden, the window losing focus) lets go of every key. A key left down is a
 * character that walks off a ledge on its own while nobody is touching the
 * screen.
 */
export function RetroTouchControls({
  platform,
  target,
}: {
  platform: string | null | undefined;
  /** Where the keys are sent: the game's canvas. */
  target: () => EventTarget | null;
}) {
  const layout = padLayout(platform);
  const held = useRef(new Set<PadButton>());
  // The d-pad's directions, kept apart so its moves update only them.
  const dpadHeld = useRef(new Set<PadButton>());
  // Read through a ref: the parent hands a new function every render, and
  // press must stay the same function, or the effect below would let go of
  // every held key whenever the game screen re-rendered (a notice appearing,
  // say) with a thumb still down.
  const targetRef = useRef(target);
  targetRef.current = target;

  const press = useCallback((button: PadButton, down: boolean) => {
    const t = targetRef.current();
    if (!t || held.current.has(button) === down) return;
    if (down) held.current.add(button);
    else held.current.delete(button);
    sendPadKey(t, button, down);
    if (down) navigator.vibrate?.(8);
  }, []);

  const releaseAll = useCallback(() => {
    for (const b of [...held.current]) press(b, false);
    dpadHeld.current.clear();
  }, [press]);

  useEffect(() => {
    const onHidden = () => {
      if (document.visibilityState === "hidden") releaseAll();
    };
    document.addEventListener("visibilitychange", onHidden);
    window.addEventListener("blur", releaseAll);
    return () => {
      document.removeEventListener("visibilitychange", onHidden);
      window.removeEventListener("blur", releaseAll);
      releaseAll();
    };
  }, [releaseAll]);

  const dpadMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const box = e.currentTarget.getBoundingClientRect();
    const r = Math.min(box.width, box.height) / 2 || 1;
    const dx = (e.clientX - (box.left + box.width / 2)) / r;
    const dy = (e.clientY - (box.top + box.height / 2)) / r;
    const want = dpadDirections(dx, dy);
    for (const d of [...dpadHeld.current]) {
      if (!want.has(d)) {
        dpadHeld.current.delete(d);
        press(d, false);
      }
    }
    for (const d of want) {
      if (!dpadHeld.current.has(d)) {
        dpadHeld.current.add(d);
        press(d, true);
      }
    }
  };
  const dpadEnd = () => {
    for (const d of [...dpadHeld.current]) press(d, false);
    dpadHeld.current.clear();
  };

  return (
    <div className="rtouch" role="group" aria-label="Game controls" onContextMenu={(e) => e.preventDefault()}>
      <div className="rtouch__side rtouch__side--left">
        {layout.shoulders[0] && <PadKey control={layout.shoulders[0]} press={press} shape="shoulder" />}
        <div
          className="rtouch__dpad"
          data-testid="dpad"
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture?.(e.pointerId);
            dpadMove(e);
          }}
          onPointerMove={(e) => {
            if (e.currentTarget.hasPointerCapture?.(e.pointerId) ?? e.buttons !== 0) dpadMove(e);
          }}
          onPointerUp={dpadEnd}
          onPointerCancel={dpadEnd}
          onLostPointerCapture={dpadEnd}
        >
          <span className="rtouch__arrow rtouch__arrow--up" />
          <span className="rtouch__arrow rtouch__arrow--down" />
          <span className="rtouch__arrow rtouch__arrow--left" />
          <span className="rtouch__arrow rtouch__arrow--right" />
        </div>
      </div>

      <div className="rtouch__system">
        {layout.system.map((c) => (
          <PadKey key={c.button} control={c} press={press} shape="pill" />
        ))}
      </div>

      <div className="rtouch__side rtouch__side--right">
        {layout.shoulders[1] && <PadKey control={layout.shoulders[1]} press={press} shape="shoulder" />}
        <div className={`rtouch__face rtouch__face--${layout.face.length}`}>
          {layout.face.map((c) => (
            <PadKey key={c.label} control={c} press={press} shape="round" />
          ))}
        </div>
      </div>
    </div>
  );
}

function PadKey({
  control,
  press,
  shape,
}: {
  control: PadControl;
  press: (b: PadButton, down: boolean) => void;
  shape: "round" | "pill" | "shoulder";
}) {
  const [down, setDown] = useState(false);
  const up = () => {
    setDown(false);
    press(control.button, false);
  };
  return (
    <button
      type="button"
      // Touch only: the keyboard and a pad already reach the game, and a
      // focusable button would take Enter for itself.
      tabIndex={-1}
      className={`rtouch__key rtouch__key--${shape}${down ? " rtouch__key--down" : ""}`}
      data-button={control.button}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture?.(e.pointerId);
        setDown(true);
        press(control.button, true);
      }}
      onPointerUp={up}
      onPointerCancel={up}
      onLostPointerCapture={up}
    >
      {control.label}
    </button>
  );
}

/*
 * Whether a controller is connected, kept current. The on-screen controls
 * step aside for a real pad and come back when it goes (ADR 0077 Decision 6,
 * "how they hand over"). A browser shows a pad only once a button on it has
 * been pressed, which is also when somebody has started using it.
 */
export function useGamepadConnected(): boolean {
  const count = () => {
    try {
      return (navigator.getGamepads?.() ?? []).filter(Boolean).length;
    } catch {
      return 0;
    }
  };
  const [connected, setConnected] = useState(() => count() > 0);
  useEffect(() => {
    const update = () => setConnected(count() > 0);
    window.addEventListener("gamepadconnected", update);
    window.addEventListener("gamepaddisconnected", update);
    return () => {
      window.removeEventListener("gamepadconnected", update);
      window.removeEventListener("gamepaddisconnected", update);
    };
  }, []);
  return connected;
}
