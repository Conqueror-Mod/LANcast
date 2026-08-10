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
