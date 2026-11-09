import type { components } from "@/api/schema";

type KindTotals = components["schemas"]["KindTotals"];

/*
 * The profile's totals, by kind, as cards (profile-2-plan §4c, step 4).
 *
 * The server splits by media kind; a person thinks in a few groups. Films and
 * episodes are watching, songs are listening, audiobook chapters are books,
 * and anything else that was marked played (a comic, so far) is counted
 * without a time, because it has none.
 */

export interface KindCard {
  id: "watched" | "listened" | "books" | "read" | "earlier";
  label: string;
  /** Time spent, when the group has any; null for a group with no time. */
  ms: number | null;
  /** Finished titles, for the note. */
  finished: number;
  started: number;
  /** The note under the number, already worded. */
  note: string;
}

function plural(n: number, one: string, many: string): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

function sum(rows: KindTotals[]) {
  return rows.reduce(
    (a, k) => ({
      started: a.started + k.started,
      finished: a.finished + k.finished,
      ms: a.ms + k.watched_ms,
    }),
    { started: 0, finished: 0, ms: 0 },
  );
}

/**
 * Cards in the order the account spends its time, most first, then anything
 * without a time, then what a reset banked. A group nobody used is left out.
 */
export function kindCards(byKind: KindTotals[], earlier?: KindTotals | null): KindCard[] {
  const of = (...kinds: string[]) => byKind.filter((k) => kinds.includes(k.kind ?? ""));
  const cards: KindCard[] = [];

  const films = of("movie");
  const episodes = of("episode");
  if (films.length || episodes.length) {
    const t = sum([...films, ...episodes]);
    const f = sum(films).finished;
    const e = sum(episodes).finished;
    const parts = [
      f ? plural(f, "film", "films") : "",
      e ? plural(e, "episode", "episodes") : "",
    ].filter(Boolean);
    cards.push({
      id: "watched",
      label: "Watched",
      ms: t.ms,
      finished: t.finished,
      started: t.started,
      note: parts.length ? `${parts.join(" · ")} finished` : "nothing finished yet",
    });
  }

  const songs = of("track");
  if (songs.length) {
    const t = sum(songs);
    cards.push({
      id: "listened",
      label: "Listened",
      ms: t.ms,
      finished: t.finished,
      started: t.started,
      note: `${plural(t.started, "song", "songs")} played`,
    });
  }

  const chapters = of("book_part", "book");
  if (chapters.length) {
    const t = sum(chapters);
    cards.push({
      id: "books",
      label: "Audiobooks",
      ms: t.ms,
      finished: t.finished,
      started: t.started,
      note: `${plural(t.finished, "chapter", "chapters")} finished`,
    });
  }

  cards.sort((a, b) => (b.ms ?? 0) - (a.ms ?? 0));

  const known = new Set(["movie", "episode", "track", "book_part", "book"]);
  const other = byKind.filter((k) => !known.has(k.kind ?? ""));
  if (other.length) {
    const t = sum(other);
    // A comic has no running time; counting it as "0m" would say it took none.
    cards.push({
      id: "read",
      label: "Read",
      ms: null,
      finished: t.finished,
      started: t.started,
      note: t.finished === t.started ? "finished" : `of ${t.started.toLocaleString()} started`,
    });
  }

  if (earlier && (earlier.started > 0 || earlier.watched_ms > 0)) {
    cards.push({
      id: "earlier",
      label: "Before you cleared your history",
      ms: earlier.watched_ms,
      finished: earlier.finished,
      started: earlier.started,
      note: "kept when you cleared it; not split by kind",
    });
  }
  return cards;
}
