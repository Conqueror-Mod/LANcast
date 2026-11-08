import { artworkURL } from "@/api/client";
import { episodeCode, runtime } from "@/lib/format";
import { isSquareArt } from "@/lib/kind";
import type { Item, ResumeCard } from "@/api/types";

/*
 * What a resume console card says, draws and opens (ADR 0118 §2).
 *
 * Pure, so each kind's wording is tested without rendering anything. Progress
 * is said in the kind's own terms -- a film has time left, an album has a
 * track, a comic has a page -- because "47%" means nothing on a comic and a
 * time left means nothing on a game.
 */

/** The art's own shape. The card is one height; the art keeps its aspect. */
export type ArtShape = "tall" | "square" | "wide";

/** Time left in a thing with a position and a length, or "" when unknown. */
function left(position: number | undefined, duration: number | null | undefined): string {
  if (!position || !duration || duration <= position) return "";
  return `${runtime(duration - position)} left`;
}

/*
 * Whole local days between two moments, for "saved 2 days ago".
 *
 * Built from local calendar days, never from the difference in hours or from
 * an ISO string: a save at 23:50 is "yesterday" at 00:10, and a date read off
 * toISOString is tomorrow every evening in the US.
 */
export function daysBetween(thenMs: number, nowMs: number): number {
  const day = (ms: number) => {
    const d = new Date(ms);
    return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  };
  return Math.round((day(nowMs) - day(thenMs)) / 86_400_000);
}

function savedAgo(touchedAt: number, nowMs: number): string {
  const days = daysBetween(touchedAt * 1000, nowMs);
  if (days <= 0) return "Saved today";
  if (days === 1) return "Saved yesterday";
  return `Saved ${days} days ago`;
}

/** The second line: where you are, in the kind's own terms. */
export function resumeDetail(card: ResumeCard, nowMs = Date.now()): string {
  const it = card.item;
  switch (card.kind) {
    case "video":
      if (it.continues_from) return `Next in ${it.continues_from.collection_title}`;
      return left(it.progress?.position_ms, it.duration_ms) || "In progress";
    case "show": {
      const ep = it.next_episode;
      const code = ep ? episodeCode(ep) : null;
      if (!ep || !code) return "Next episode";
      const at = left(ep.progress?.position_ms, ep.duration_ms);
      return at ? `${code} · ${at}` : `${code} · next`;
    }
    case "music": {
      const tr = card.track;
      if (!tr?.number) return "In progress";
      return tr.of ? `Track ${tr.number} of ${tr.of}` : `Track ${tr.number}`;
    }
    case "book":
      return left(it.progress?.position_ms, it.duration_ms) || "In progress";
    case "comic":
      return card.page ? `Page ${card.page.page + 1} of ${card.page.pages}` : "In progress";
    case "game":
      return savedAgo(card.touched_at, nowMs);
  }
  return "";
}

/** How far through, 0–1, or null where a bar would mean nothing (a game). */
export function resumeFraction(card: ResumeCard): number | null {
  const it = card.item;
  const frac = (a: number | undefined, b: number | null | undefined) =>
    a && b && b > 0 ? Math.min(1, a / b) : null;
  switch (card.kind) {
    case "video":
    case "book":
      return frac(it.progress?.position_ms, it.duration_ms);
    case "show":
      return frac(it.next_episode?.progress?.position_ms, it.next_episode?.duration_ms);
    case "music":
      return card.track?.number && card.track.of ? frac(card.track.number, card.track.of) : null;
    case "comic":
      return card.page && card.page.pages > 0 ? Math.min(1, (card.page.page + 1) / card.page.pages) : null;
  }
  return null;
}

/** The picture and its shape. A show draws the episode it would play next. */
export function resumeArt(card: ResumeCard): { src: string | undefined; shape: ArtShape } {
  const it = card.item;
  if (card.kind === "show") {
    const still = it.next_episode?.artwork?.thumb ?? it.artwork?.fanart;
    return { src: artworkURL(still, "poster"), shape: "wide" };
  }
  const src = artworkURL(it.artwork?.poster, "poster");
  // A game's box is whatever shape its console made it; it sits whole in a
  // square, as it does on a poster tile.
  if (card.kind === "game" || isSquareArt(it)) return { src, shape: "square" };
  return { src, shape: "tall" };
}

/** Where pressing the card goes, for the kinds that do not need the server. */
export function resumeHref(card: ResumeCard): string {
  // A comic opens at your page: the reader resumes from the saved place.
  if (card.kind === "comic") return `/read/${card.item.id}`;
  return `/item/${card.item.id}`;
}

/** The item a card's Mark and Remove entries write to. */
export function resumeTarget(card: ResumeCard): Item {
  // An album card's place is its track; marking the album would mark
  // something nobody played.
  return card.kind === "music" && card.track ? card.track.item : card.item;
}
