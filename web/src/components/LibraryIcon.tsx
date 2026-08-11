// Line glyphs for the collapsed rail.
//
// Drawn here rather than pulled from an icon font, for the reason hls.js is not
// vendored and Tailwind is not used: a font is a dependency, a download and a
// licence for five shapes that take twenty lines. They are stroked rather than
// filled so they sit at the same visual weight as the wide-tracked labels beside
// them, and they inherit currentColor so the gold focus rule needs no special
// case here.
//
// A kind with no glyph gets the generic one rather than nothing. The kind set is
// open (ADR 0018), and a rail with a hole in it where an unfamiliar library
// should be is worse than a rail with a plain square.
export function LibraryIcon({ kind }: { kind: string }) {
  return (
    <svg
      className="rail-icon"
      viewBox="0 0 20 20"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {glyph(kind)}
    </svg>
  );
}

function glyph(kind: string) {
  switch (kind) {
    case "movie":
      // A strip of film: the frame, with perforations down both edges.
      return (
        <>
          <rect x="2.5" y="4" width="15" height="12" rx="1.5" />
          <path d="M6 4v12M14 4v12" />
        </>
      );
    case "show":
      // A screen on a stand, which is a television everywhere.
      return (
        <>
          <rect x="2.5" y="4.5" width="15" height="10" rx="1.5" />
          <path d="M7 17.5h6" />
        </>
      );
    case "music":
      // A single note: stem and head, no beam. A beamed pair reads as busy at
      // 18px and stops being legible.
      return (
        <>
          <path d="M8 15.5V5l7-1.5v10" />
          <circle cx="6" cy="15.5" r="2" />
          <circle cx="13" cy="13.5" r="2" />
        </>
      );
    case "picture":
      // A frame with a horizon and a sun, which is the one arrangement everyone
      // reads as "photograph" rather than "document".
      return (
        <>
          <rect x="2.5" y="4" width="15" height="12" rx="1.5" />
          <circle cx="7" cy="8" r="1.5" />
          <path d="M3 14l4.5-4 3.5 3 2.5-2 3.5 3" />
        </>
      );
    default:
      return <rect x="3" y="4" width="14" height="12" rx="1.5" />;
  }
}

// HomeIcon marks the brand link when the rail is collapsed, where "LANCAST" has
// no room to be read.
export function HomeIcon() {
  return (
    <svg
      className="rail-icon"
      viewBox="0 0 20 20"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M3 9l7-5.5L17 9" />
      <path d="M5 8.5V16h10V8.5" />
    </svg>
  );
}

// The foot of the rail: settings, who you are, and the way out. Same 20-unit
// grid and same stroke weight as the library glyphs above, because they sit in
// the same column and any difference in weight reads as a mistake.
function RailGlyph({ children }: { children: React.ReactNode }) {
  return (
    <svg
      className="rail-icon"
      viewBox="0 0 20 20"
      width="18"
      height="18"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

// A gear, reduced to a ring and six teeth. More teeth than that turns to mush
// at 18px, which is the only size this is ever drawn at.
export function SettingsIcon() {
  return (
    <RailGlyph>
      <circle cx="10" cy="10" r="2.6" />
      <path d="M10 2.6v2.1M10 15.3v2.1M17.4 10h-2.1M4.7 10H2.6M15.2 4.8l-1.5 1.5M6.3 13.7l-1.5 1.5M15.2 15.2l-1.5-1.5M6.3 6.3L4.8 4.8" />
    </RailGlyph>
  );
}

// Head and shoulders. Deliberately not a filled avatar: this is a person, not
// a photograph, and a filled shape here would out-weigh the gear beside it.
export function AccountIcon() {
  return (
    <RailGlyph>
      <circle cx="10" cy="7" r="3" />
      <path d="M4.5 16.5c0-3 2.5-4.8 5.5-4.8s5.5 1.8 5.5 4.8" />
    </RailGlyph>
  );
}

// A door with the arrow leaving it. The arrow points out of the frame, which is
// the half people actually read.
export function SignOutIcon() {
  return (
    <RailGlyph>
      <path d="M12 3.5H5.5v13H12" />
      <path d="M9.5 10h8" />
      <path d="M15 7.2 17.8 10 15 12.8" />
    </RailGlyph>
  );
}
