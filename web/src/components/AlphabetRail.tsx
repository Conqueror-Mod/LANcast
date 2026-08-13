/*
 * The A–Z rail.
 *
 * A jump list, not a scrollbar. A grid pages in as you scroll, so "jump to S"
 * cannot mean "scroll to a row that has not loaded" — it means "ask for the S
 * titles", which is the same gesture with an answer that exists, and it behaves
 * the same on a library of nine hundred as on one of nine.
 *
 * Its own component because three grids use it now. It lived in LibraryView,
 * and copying it into the collections and playlists pages would have been three
 * implementations that agree until one of them is edited.
 *
 * Only the letters that are actually there: a strip of twenty-six where
 * nineteen do nothing is a control that lies about what is in the library.
 * Fewer than two and there is nothing to jump between, so it renders nothing.
 */
export function AlphabetRail({
  initials,
  selected,
  onPick,
}: {
  initials: string[];
  selected: string;
  /** Called with the letter, or "" when the current one is pressed again. */
  onPick: (initial: string) => void;
}) {
  if (initials.length < 2) return null;
  return (
    <div className="browse__az" role="group" aria-label="Jump to a letter">
      {initials.map((c) => (
        <button
          key={c}
          className={"browse__az-key" + (c === selected ? " is-on" : "")}
          aria-pressed={c === selected}
          // Pressing the selected letter again clears it: a filter you can
          // enter and not leave is a trap.
          onClick={() => onPick(c === selected ? "" : c)}
        >
          {c}
        </button>
      ))}
    </div>
  );
}

/**
 * The initials present in a list of things, for pages that hold their items in
 * memory rather than asking the server for facets.
 *
 * "#" collects everything that does not start with a Latin letter, matching the
 * server's bucketing so the two never disagree about where a title lives.
 */
export function initialsOf(titles: string[]): string[] {
  const seen = new Set<string>();
  for (const t of titles) {
    const c = (t ?? "").trim().charAt(0).toUpperCase();
    seen.add(c >= "A" && c <= "Z" ? c : "#");
  }
  const out: string[] = [];
  if (seen.has("#")) out.push("#");
  for (let c = 65; c <= 90; c++) {
    const letter = String.fromCharCode(c);
    if (seen.has(letter)) out.push(letter);
  }
  return out;
}

/** Does this title belong under that letter? */
export function matchesInitial(title: string, initial: string): boolean {
  if (!initial) return true;
  const c = (title ?? "").trim().charAt(0).toUpperCase();
  const bucket = c >= "A" && c <= "Z" ? c : "#";
  return bucket === initial;
}
