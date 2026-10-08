/*
 * The order of the rail's libraries, and where the Game Hub sits among them.
 *
 * The server lists libraries by name, which is the order a settings table
 * wants and not the order somebody reaches for things in: "Anime" above
 * "Movies", "Zelda Hacks" below "TV". The rail goes by what a library is —
 * Movies, TV Shows, Music, Game Hub, Pictures — and keeps the name order
 * within each kind.
 *
 * The Game Hub (docs/game-hub-plan.md) stands for both kinds of game: the
 * retro libraries on this server and the PC games installed on this
 * computer. So retro libraries are not listed one by one here — the hub lists
 * them — and the hub takes their place in the order. Chris's notes,
 * 2026-10-08: first Retro Games then PC Games, then one Game Hub for both.
 */

export type RailEntry<L> = { type: "library"; lib: L } | { type: "game-hub" };

// Where each kind goes. The hub takes the games slot, between music and
// pictures; a kind not named here (a new one, or one this client does not
// know) goes last rather than vanishing.
const KIND_RANK: Record<string, number> = {
  movie: 0,
  show: 1,
  music: 2,
  picture: 4,
};
const HUB_RANK = 3;
const UNKNOWN_RANK = 5;

export function railOrder<L extends { kind: string }>(libraries: L[], hub: boolean): RailEntry<L>[] {
  // Retro libraries are reached through the hub whenever it is offered. When
  // it is not, they have nowhere else to be, so they keep a place of their
  // own in the hub's slot.
  const listed = hub ? libraries.filter((l) => l.kind !== "retro") : libraries;
  const ranked = listed.map((lib, i) => ({
    lib,
    i,
    rank: lib.kind === "retro" ? HUB_RANK : (KIND_RANK[lib.kind] ?? UNKNOWN_RANK),
  }));
  // Stable on the server's order within a kind.
  ranked.sort((a, b) => a.rank - b.rank || a.i - b.i);
  const out: RailEntry<L>[] = [];
  let placed = !hub;
  for (const r of ranked) {
    if (!placed && r.rank > HUB_RANK) {
      out.push({ type: "game-hub" });
      placed = true;
    }
    out.push({ type: "library", lib: r.lib });
  }
  if (!placed) out.push({ type: "game-hub" });
  return out;
}
