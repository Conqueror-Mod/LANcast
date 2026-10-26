/*
 * On-screen controls for a phone (ADR 0077 Decision 6), the part that needs no
 * screen: which buttons each console gets, which key each one sends, and how a
 * thumb on the d-pad becomes directions.
 *
 * # How a touch reaches the game
 *
 * The web core is RetroArch, and RetroArch reads the keyboard itself, through
 * Emscripten's key listeners, which take any KeyboardEvent the page dispatches
 * (there is no isTrusted check). So a button sends the key RetroArch's default
 * bindings already give its RetroPad button, and nothing in RetroArch's
 * configuration changes. Measured on the installed service on 2026-10-25: a
 * dispatched F1 opened RetroArch's quick menu.
 *
 * RetroArch reads key state once a frame, so a press is held from the touch
 * landing to the touch lifting, never a tap of a set length: a tap shorter
 * than a frame would be lost.
 */

/** A RetroPad button, by RetroArch's name for it. */
export type PadButton =
  | "up"
  | "down"
  | "left"
  | "right"
  | "a"
  | "b"
  | "x"
  | "y"
  | "l"
  | "r"
  | "start"
  | "select";

interface Key {
  key: string;
  code: string;
  keyCode: number;
  location?: number;
}

/*
 * RetroArch's default keyboard bindings for player 1 (input_player1_*): the
 * arrows; x and z for A and B; s and a for X and Y; q and w for L and R;
 * Enter for Start and the right Shift for Select. The game's page already
 * tells keyboard players "X and Z for the main buttons, Enter for Start".
 */
export const PAD_KEYS: Record<PadButton, Key> = {
  up: { key: "ArrowUp", code: "ArrowUp", keyCode: 38 },
  down: { key: "ArrowDown", code: "ArrowDown", keyCode: 40 },
  left: { key: "ArrowLeft", code: "ArrowLeft", keyCode: 37 },
  right: { key: "ArrowRight", code: "ArrowRight", keyCode: 39 },
  a: { key: "x", code: "KeyX", keyCode: 88 },
  b: { key: "z", code: "KeyZ", keyCode: 90 },
  x: { key: "s", code: "KeyS", keyCode: 83 },
  y: { key: "a", code: "KeyA", keyCode: 65 },
  l: { key: "q", code: "KeyQ", keyCode: 81 },
  r: { key: "w", code: "KeyW", keyCode: 87 },
  start: { key: "Enter", code: "Enter", keyCode: 13 },
  select: { key: "Shift", code: "ShiftRight", keyCode: 16, location: 2 },
};

/** Sends one button's key, down or up, to where RetroArch listens. */
export function sendPadKey(target: EventTarget, button: PadButton, down: boolean) {
  const k = PAD_KEYS[button];
  target.dispatchEvent(
    new KeyboardEvent(down ? "keydown" : "keyup", {
      key: k.key,
      code: k.code,
      keyCode: k.keyCode,
      which: k.keyCode,
      location: k.location ?? 0,
      bubbles: true,
      cancelable: true,
    }),
  );
}

export interface PadControl {
  /** What the button says: the console's own name for it. */
  label: string;
  button: PadButton;
}

export interface PadLayout {
  /** Face buttons, left to right as the console has them. */
  face: PadControl[];
  /** Shoulder buttons, left then right; empty when the console has none. */
  shoulders: PadControl[];
  /** Start, Select and their kin, in a small row of their own. */
  system: PadControl[];
}

/*
 * padLayout: the buttons a console's controller had, each named as the
 * console named it and sent as the RetroPad button its core reads it from.
 *
 * - NES and Game Boy: B and A, Select and Start.
 * - GBA: the same, with L and R.
 * - Master System: buttons 1 and 2, and Start. Gearsystem reads 1 from the
 *   RetroPad's B and 2 from its A, so they sit where B and A do.
 * - Mega Drive: A, B and C, and Start. clownmdemu reads them, by the libretro
 *   convention for the console, from the RetroPad's Y, B and A.
 *
 * The three-button Mega Drive pad, not the six: the six-button one came late,
 * and three is what most of its library was made for. A console this does not
 * know gets the NES pad, which every console here can at least be steered with.
 */
export function padLayout(platform: string | null | undefined): PadLayout {
  const startSelect: PadControl[] = [
    { label: "Select", button: "select" },
    { label: "Start", button: "start" },
  ];
  const ba: PadControl[] = [
    { label: "B", button: "b" },
    { label: "A", button: "a" },
  ];
  switch (platform) {
    case "gba":
      return {
        face: ba,
        shoulders: [
          { label: "L", button: "l" },
          { label: "R", button: "r" },
        ],
        system: startSelect,
      };
    case "sms":
      return {
        face: [
          { label: "1", button: "b" },
          { label: "2", button: "a" },
        ],
        shoulders: [],
        system: [{ label: "Start", button: "start" }],
      };
    case "genesis":
      return {
        face: [
          { label: "A", button: "y" },
          { label: "B", button: "b" },
          { label: "C", button: "a" },
        ],
        shoulders: [],
        system: [{ label: "Start", button: "start" }],
      };
    default:
      return { face: ba, shoulders: [], system: startSelect };
  }
}

/*
 * dpadDirections: which directions a thumb at (dx, dy) holds, measured from
 * the pad's centre in units of its radius (so the rim is 1), y down.
 *
 * Eight ways, as a real d-pad rocks: each direction owns a 135° wedge, so the
 * diagonals are where two wedges overlap. A dead zone at the centre holds
 * nothing, so resting a thumb in the middle does not walk the character.
 * Past the rim still counts: a thumb drifts outward while playing, and a pad
 * that let go when it did would drop a run mid-jump.
 */
export const DPAD_DEAD_ZONE = 0.25;

export function dpadDirections(dx: number, dy: number): Set<PadButton> {
  const held = new Set<PadButton>();
  if (Math.hypot(dx, dy) < DPAD_DEAD_ZONE) return held;
  const deg = (Math.atan2(-dy, dx) * 180) / Math.PI; // 0 = right, 90 = up
  const within = (centre: number) => {
    const d = Math.abs((((deg - centre) % 360) + 540) % 360 - 180);
    return d <= 67.5;
  };
  if (within(0)) held.add("right");
  if (within(90)) held.add("up");
  if (within(180)) held.add("left");
  if (within(270)) held.add("down");
  return held;
}
