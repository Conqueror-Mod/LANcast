// The 10-second skip glyphs: a circular arrow with the number inside, which is
// the shape every player has taught people to read as "jump ten seconds".
//
// Drawn rather than typed. There is no character for it — the nearest are ⏪ and
// ⏩, which mean *rewind* and *fast forward*, a different control that most of
// these players also have. Using them here would say the wrong thing in the one
// place a player must be unambiguous.
export function SkipGlyph({ dir }: { dir: "back" | "forward" }) {
  const back = dir === "back";
  return (
    <svg
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <g transform={back ? "" : "scale(-1,1) translate(-24,0)"}>
        {/* Three quarters of a circle, open at the top left, with the arrow
            head closing it — the gap is what makes it read as a return rather
            than as a loop. */}
        <path d="M5.5 8.5A8 8 0 1 0 12 4" />
        <path d="M12 1.5 8.6 4.2 12 6.8" />
      </g>
      <text
        x="12"
        y="16.5"
        textAnchor="middle"
        fontSize="8.5"
        stroke="none"
        fill="currentColor"
      >
        10
      </text>
    </svg>
  );
}
