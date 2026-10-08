/*
 * The order of the rail's libraries, and where PC Games sits among them.
 *
 * The server lists libraries by name, which is the order a settings table
 * wants and not the order somebody reaches for things in: "Anime" above
 * "Movies", "Zelda Hacks" below "TV". The rail goes by what a library is —
 * Movies, TV Shows, Music, Retro Games, PC Games, Pictures — and keeps the
 * name order within each kind.
 *
 * PC Games is not a library — its games are installed on this machine, not
 * kept by the server (ADR 0066) — but it is somewhere you go to play, so it
 * sits beside Retro Games rather than down with Live TV and Add-ons, where it
 * read as an afterthought. Chris's order, from his notes on 2026-10-08.
 */

export type RailEntry<L> = { type: "library"; lib: L } | { type: "pc-games" };

// Where each kind goes. PC Games takes the slot between retro and pictures;
// a kind not named here (a new one, or one this client does not know) goes
// last rather than vanishing.
const KIND_RANK: Record<string, number> = {
  movie: 0,
  show: 1,
  music: 2,
  retro: 3,
  picture: 5,
};
const PC_GAMES_RANK = 4;
const UNKNOWN_RANK = 6;

export function railOrder<L extends { kind: string }>(libraries: L[], pcGames: boolean): RailEntry<L>[] {
  const ranked = libraries.map((lib, i) => ({ lib, i, rank: KIND_RANK[lib.kind] ?? UNKNOWN_RANK }));
  // Stable on the server's order within a kind: sort by rank, then by where
  // the server put it.
  ranked.sort((a, b) => a.rank - b.rank || a.i - b.i);
  const out: RailEntry<L>[] = [];
  let placed = !pcGames;
  for (const r of ranked) {
    if (!placed && r.rank > PC_GAMES_RANK) {
      out.push({ type: "pc-games" });
      placed = true;
    }
    out.push({ type: "library", lib: r.lib });
  }
  if (!placed) out.push({ type: "pc-games" });
  return out;
}
