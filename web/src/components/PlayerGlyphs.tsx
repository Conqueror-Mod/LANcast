// Player glyphs, drawn.
//
// The player used a handful of emoji — 🔊 🔇 and, once shuffle and repeat
// arrived, 🔀 🔁 🔂 🎧. On a dark chrome Chrome renders those from its colour
// emoji font, so they arrive as blue-and-white boxes: the only saturated colour
// in a monochrome bar, and worse, shuffle and repeat looked permanently
// *engaged* because a coloured glyph reads as an active state. The design has
// exactly one accent and it means focus (design.md).
//
// So they are drawn here, stroked in currentColor at the same weight as the
// skip glyphs. Same reasoning as those and as the rail icons: a handful of
// shapes is cheaper to draw than an icon font is to depend on, and these
// inherit the state colours for free.
type Props = { size?: number };

function Svg({ size = 20, children }: Props & { children: React.ReactNode }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

// Two paths crossing, with arrowheads: the crossing is what says "shuffle"
// rather than "repeat".
export function ShuffleGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M3 6h4l10 12h4" />
      <path d="M3 18h4l3-3.6" />
      <path d="M14.2 8.6 17 6h4" />
      <path d="M18.5 3.5 21.5 6l-3 2.5" />
      <path d="M18.5 15.5 21.5 18l-3 2.5" />
    </Svg>
  );
}

// A loop. `one` puts a 1 in the middle, which is the only difference every
// player uses and therefore the only one people already read.
export function RepeatGlyph({ one, ...p }: Props & { one?: boolean }) {
  return (
    <Svg {...p}>
      <path d="M4 9V8a3 3 0 0 1 3-3h10" />
      <path d="M14.5 2.5 17.5 5l-3 2.5" />
      <path d="M20 15v1a3 3 0 0 1-3 3H7" />
      <path d="M9.5 21.5 6.5 19l3-2.5" />
      {one && (
        <text
          x="12"
          y="15"
          textAnchor="middle"
          fontSize="9"
          stroke="none"
          fill="currentColor"
        >
          1
        </text>
      )}
    </Svg>
  );
}

// A speaker, with waves that disappear when muted — the state is in the waves,
// not in a colour.
export function VolumeGlyph({ muted, ...p }: Props & { muted?: boolean }) {
  return (
    <Svg {...p}>
      <path d="M4 9.5v5h3.5L12 18.5v-13L7.5 9.5H4z" />
      {muted ? (
        <>
          <path d="M16 9.5l4.5 5" />
          <path d="M20.5 9.5l-4.5 5" />
        </>
      ) : (
        <>
          <path d="M15.5 9a4.5 4.5 0 0 1 0 6" />
          <path d="M18.5 6.5a8 8 0 0 1 0 11" />
        </>
      )}
    </Svg>
  );
}

// Headphones for the audio-track picker: a band and two cups, which reads as
// "listening" rather than as "sound level" — the distinction the volume control
// beside it depends on.
export function AudioTrackGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M4 15v-3a8 8 0 0 1 16 0v3" />
      <rect x="2.5" y="14" width="4.5" height="6.5" rx="1.6" />
      <rect x="17" y="14" width="4.5" height="6.5" rx="1.6" />
    </Svg>
  );
}

/* Sliders, not a cog.
 *
 * A cog means "application settings" everywhere else in this app, and the row
 * of controls this opens is what a person came to adjust rather than a
 * configuration screen. Three sliders at different positions says "the things
 * you can change about this playback", which is exactly what is behind it. */
export function SettingsGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M4 7h16M4 12h16M4 17h16" />
      <circle cx="9" cy="7" r="2" />
      <circle cx="15" cy="12" r="2" />
      <circle cx="7" cy="17" r="2" />
    </Svg>
  );
}

// A list, for the queue.
export function QueueGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M4 7h11M4 12h11M4 17h7" />
      <path d="M18 12v6" />
      <path d="M16 15.5 18 17.8l2-2.3" />
    </Svg>
  );
}

// A rectangle with a smaller one inset at the lower right: the shape every
// browser uses for picture-in-picture.
export function PipGlyph(p: Props) {
  return (
    <Svg {...p}>
      <rect x="3" y="5" width="18" height="14" rx="2" />
      <rect x="12" y="11.5" width="7" height="5.5" rx="1" />
    </Svg>
  );
}

// A frame with its corners drawn and its middle missing, which is fullscreen
// everywhere. Replaces ⛶, a character many fonts do not have at all.
export function FullscreenGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M4 9V5h4" />
      <path d="M20 9V5h-4" />
      <path d="M4 15v4h4" />
      <path d="M20 15v4h-4" />
    </Svg>
  );
}

export function PrevGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M18 5.5v13L8.5 12z" />
      <path d="M6 5.5v13" />
    </Svg>
  );
}

export function NextGlyph(p: Props) {
  return (
    <Svg {...p}>
      <path d="M6 5.5v13L15.5 12z" />
      <path d="M18 5.5v13" />
    </Svg>
  );
}

// A square. Rounded to the same corner radius the other glyphs are stroked
// with, so it sits in the row rather than on top of it.
export function StopGlyph(p: Props) {
  return (
    <Svg {...p}>
      <rect x="6" y="6" width="12" height="12" rx="1.5" />
    </Svg>
  );
}
