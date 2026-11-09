import { useEffect, useRef, useState } from "react";
import { useFocusable } from "@/focus/FocusController";
import { onRetroEvent, retroCommand, type AchievementSet, type GameAchievement } from "@/playback/retro";
import "./RetroAchievements.css";

/*
 * RetroAchievements while playing (ADR 0112), as the game page shows it: a
 * popup when one unlocks, a line while one is being counted towards, and the
 * set on a page of the game menu.
 *
 * Everything here is what the client said. Whether earning is on is the
 * server's to say; the page hears "off" with the reason, and the menu passes
 * it on rather than guessing.
 */

/** What the running game's achievements are doing. */
export interface AchievementStatus {
  /** unknown until the client says: no runtime at all is also unknown. */
  status: "unknown" | "none" | "off" | "ready";
  title: string;
  total: number;
  unlocked: number;
  /** Why earning is off, in the server's words. */
  offText: string;
}

/** One popup. */
export interface Unlock {
  key: number;
  kind: "unlocked" | "mastered";
  title: string;
  description: string;
  badge?: string;
  points: number;
}

const UNLOCK_SHOWN_MS = 5000;

/**
 * useAchievements listens to the game for the life of the page. notice is
 * the page's own short message line, for "can't be reached" and the like.
 */
export function useAchievements(notice: (text: string) => void) {
  const [status, setStatus] = useState<AchievementStatus>({
    status: "unknown",
    title: "",
    total: 0,
    unlocked: 0,
    offText: "",
  });
  const [set, setSet] = useState<AchievementSet | null>(null);
  const [queue, setQueue] = useState<Unlock[]>([]);
  const [progress, setProgress] = useState<{ title: string; text: string } | null>(null);
  const noticeRef = useRef(notice);
  noticeRef.current = notice;
  const seq = useRef(0);

  useEffect(
    () =>
      onRetroEvent((e) => {
        if (e.kind === "achievements") {
          setSet(e.set ?? null);
          return;
        }
        if (e.kind !== "achievement") return;
        switch (e.event) {
          case "ready":
            setStatus({
              status: "ready",
              title: e.title ?? "",
              total: e.total ?? 0,
              unlocked: e.unlocked ?? 0,
              offText: "",
            });
            break;
          case "none":
            setStatus((s) => ({ ...s, status: "none" }));
            break;
          case "off":
            setStatus((s) => ({ ...s, status: "off", offText: e.text ?? "" }));
            break;
          case "unlocked":
            setStatus((s) => ({ ...s, unlocked: Math.min(s.total, s.unlocked + 1) }));
            setProgress(null);
            setQueue((q) => [
              ...q,
              {
                key: ++seq.current,
                kind: "unlocked",
                title: e.title ?? "",
                description: e.description ?? "",
                badge: e.badge,
                points: e.points ?? 0,
              },
            ]);
            // The list, if it was fetched, is now out of date.
            setSet(null);
            break;
          case "mastered":
            setQueue((q) => [
              ...q,
              { key: ++seq.current, kind: "mastered", title: "", description: "", points: 0 },
            ]);
            break;
          case "progress":
            setProgress({ title: e.title ?? "", text: e.progress ?? "" });
            break;
          case "progress-hide":
            setProgress(null);
            break;
          case "offline":
            noticeRef.current("RetroAchievements can't be reached. Unlocks will be sent when it can.");
            break;
          case "online":
            noticeRef.current("RetroAchievements is reachable again.");
            break;
          case "problem":
            if (e.text) noticeRef.current(`RetroAchievements: ${e.text}`);
            break;
        }
      }),
    [],
  );

  // One popup at a time, each for a few seconds.
  const current = queue[0] ?? null;
  useEffect(() => {
    if (!current) return;
    const t = setTimeout(() => setQueue((q) => q.slice(1)), UNLOCK_SHOWN_MS);
    return () => clearTimeout(t);
  }, [current]);

  return { status, set, unlock: current, progress };
}

/** The popup for an unlock, and for completing the set. */
export function UnlockPopup({ unlock, gameTitle }: { unlock: Unlock | null; gameTitle: string }) {
  if (!unlock) return null;
  if (unlock.kind === "mastered") {
    return (
      <div className="ra-unlock" role="status" aria-live="polite">
        <div className="ra-unlock__what">
          <span className="ra-unlock__label">Set complete</span>
          <span className="ra-unlock__title">Every achievement in {gameTitle || "this game"}</span>
        </div>
      </div>
    );
  }
  return (
    <div className="ra-unlock" role="status" aria-live="polite">
      {unlock.badge ? <img className="ra-unlock__badge" src={unlock.badge} alt="" /> : <span className="ra-unlock__badge" />}
      <div className="ra-unlock__what">
        <span className="ra-unlock__label">Achievement unlocked · {unlock.points} pts</span>
        <span className="ra-unlock__title">{unlock.title}</span>
        {unlock.description && <span className="ra-unlock__desc">{unlock.description}</span>}
      </div>
    </div>
  );
}

/** The line shown while an achievement is being counted towards. */
export function ProgressLine({ progress }: { progress: { title: string; text: string } | null }) {
  if (!progress || !progress.text) return null;
  return (
    <div className="ra-progress" aria-live="polite">
      {progress.title} · {progress.text}
    </div>
  );
}

/** What the menu's entry says, or null when there is nothing to offer. */
export function achievementsLabel(s: AchievementStatus): string | null {
  switch (s.status) {
    case "ready":
      return `Achievements: ${s.unlocked} of ${s.total}`;
    case "off":
      return "Achievements: off";
    default:
      return null;
  }
}

/**
 * AchievementsPage is the menu's page. It asks the client for the set when
 * it opens (and again after an unlock), and lists it; each row takes focus,
 * so a pad steps through a long set and the panel scrolls with it.
 */
export function AchievementsPage({ status, set }: { status: AchievementStatus; set: AchievementSet | null }) {
  useEffect(() => {
    if (status.status === "ready" && !set) void retroCommand("achievements");
  }, [status.status, set]);

  if (status.status === "off") {
    return (
      <>
        <div className="retro-play__heading">Achievements</div>
        <p className="retro-play__hint">
          {status.offText || "Earning achievements is off."} Earning is turned on in Settings, under
          Connected accounts.
        </p>
      </>
    );
  }
  const list = set?.achievements ?? [];
  const ordered = [...list].sort((a, b) => rank(a) - rank(b));
  // Every one unsupported means the core shows rcheevos none of the game's
  // memory (bsnes, today): say so once rather than on every row.
  const noneEarnable = list.length > 0 && list.every((a) => a.state === "unsupported");
  return (
    <>
      <div className="retro-play__heading">Achievements</div>
      <div className="ra-set__summary">
        {status.title && <span className="ra-set__game">{status.title}</span>}
        <span>
          {status.unlocked} of {status.total} unlocked
          {set && set.points > 0 && (
            <>
              {" "}
              · {set.points_unlocked} of {set.points} points
            </>
          )}
        </span>
        <span className="retro-play__hint">Casual (softcore): save states and rewind stay available.</span>
        {set?.warning && <span className="retro-play__hint">RetroAchievements: {set.warning}</span>}
      </div>
      {noneEarnable && (
        <p className="ra-set__none" role="note">
          None of these can be earned with this console's emulator yet: it doesn't let LANcast read the game's memory.
        </p>
      )}
      {!set && <p className="retro-play__hint">Loading…</p>}
      {set && (
        <ul className="ra-set" aria-label="Achievements in this game">
          {ordered.map((a) => (
            <AchievementRow key={a.id} a={a} />
          ))}
        </ul>
      )}
    </>
  );
}

// Unlocked first, then those still to earn, then those this core cannot.
function rank(a: GameAchievement): number {
  return a.state === "unlocked" ? 0 : a.state === "locked" ? 1 : 2;
}

function AchievementRow({ a }: { a: GameAchievement }) {
  const f = useFocusable(() => {});
  return (
    <li
      className={`ra-set__row ra-set__row--${a.state}`}
      ref={f.ref}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
    >
      {a.badge ? <img className="ra-set__badge" src={a.badge} alt="" /> : <span className="ra-set__badge" />}
      <div className="ra-set__what">
        <span className="ra-set__title">{a.title}</span>
        <span className="ra-set__desc">{a.description}</span>
        <span className="ra-set__meta">
          {a.points} pts
          {a.state === "unlocked" && " · unlocked"}
          {a.state === "locked" && a.progress && ` · ${a.progress}`}
          {a.state === "unsupported" && " · not available with this emulator"}
        </span>
      </div>
    </li>
  );
}
