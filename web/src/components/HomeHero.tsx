import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { showContinueTarget } from "@/lib/continueShow";
import { continueQueue } from "@/lib/continueQueue";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import { isMusic, isSquareArt } from "@/lib/kind";
import { rating, runtime } from "@/lib/format";
import type { Item } from "@/api/types";
import type { HeroReason } from "@/lib/heroMode";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
import "./HomeHero.css";

// Motion is opt-out, and the check is read once per mount rather than per frame.
function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(
    () =>
      typeof window !== "undefined" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    const onChange = () => setReduced(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return reduced;
}

// The backdrop sinks at a fraction of the scroll rate. It is the cheapest
// convincing depth cue there is: two planes moving at different speeds read as
// distance in a way no amount of shadow does. Written straight to a custom
// property inside a rAF so React never re-renders on scroll.
function useParallax(enabled: boolean) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let frame = 0;
    const apply = () => {
      frame = 0;
      const el = ref.current;
      if (!el) return;
      // Past the hero's own height there is nothing left to park, so the offset
      // stops growing rather than dragging the art off its own container.
      const y = Math.min(window.scrollY, el.offsetHeight);
      el.style.setProperty("--parallax", `${y * 0.28}px`);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(apply);
    };
    apply();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [enabled]);
  return ref;
}

// A hero control. Gold is a hairline at rest and full strength with its ring on
// hover or focus — the same contract PosterTile keeps, so the focus signal reads
// identically whether you arrived here by mouse, by tab, or by arrow key.
function HeroButton({
  label,
  onPress,
  primary,
}: {
  label: string;
  onPress: () => void;
  primary?: boolean;
}) {
  const focusable = useFocusable(onPress);
  return (
    <button
      {...focusable}
      className={"hero__btn" + (primary ? " hero__btn--primary" : "")}
      onClick={onPress}
    >
      {label}
    </button>
  );
}

function progressPct(item: Item): number {
  const pos = item.progress?.position_ms ?? 0;
  const dur = item.duration_ms ?? 0;
  if (!pos || !dur) return 0;
  return Math.min(100, (pos / dur) * 100);
}

// The spotlight above the shelves. It shows what you are part-way through, and
// falls back to the newest thing in the library when nothing is in progress —
// resume is the likeliest reason someone opened LANcast at all, and a hero that
// advertises a film you are already 40 minutes into is worse than no hero.
//
// It renders nothing when there is no artwork to render. A hero built around a
// missing backdrop is a grey box with a title in it, which is precisely the look
// this screen exists to get away from.
/*
 * What the spotlight says about itself.
 *
 * The eyebrow is not decoration: it is the difference between a home page that
 * looks arranged and one that looks automatic. A recommendation in particular
 * has to name the thing it came from — "Because you watched X" is a claim, and
 * a suggestion that cannot say where it came from is the kind of thing this
 * project has no interest in growing.
 */
function eyebrow(reason: HeroReason, item: Item, seed: Item | undefined): string {
  switch (reason) {
    case "resuming":
      // An album part way through is being listened to, not watched.
      return isMusic(item) ? "Continue listening" : "Continue watching";
    case "recent":
      return "Just added";
    case "pinned":
      return "Pinned to your homepage";
    case "recommended":
      if (!seed) return "Suggested";
      return isMusic(seed)
        ? `Because you listened to ${seed.title}`
        : `Because you watched ${seed.title}`;
  }
}

export function HomeHero({
  item,
  reason,
  seed,
  actions,
}: {
  item: Item;
  reason: HeroReason;
  /** For "recommended": the item the suggestion was drawn from. */
  seed?: Item;
  /**
   * The same menu the item's Continue Watching tile carries, when it is on
   * that shelf. Reported after 0.9.80: a collection's next film sat in the
   * spotlight with no way to take it off — no right-click, and nothing on
   * its page — so the spotlight answers right-click, and More opens the
   * same menu for a remote or a keyboard.
   */
  actions?: MenuAction[];
}) {
  const label = eyebrow(reason, item, seed);
  const navigate = useNavigate();
  const [menuAt, setMenuAt] = useState<MenuPoint | null>(null);
  const [byKey, setByKey] = useState(false);
  const hasMenu = (actions?.length ?? 0) > 0;

  /*
   * Resuming a show carries on through it.
   *
   * This used to be `navigate("/watch/" + item.id)` with no queue, so pressing
   * Resume on a half-watched series played that one episode and stopped — with
   * nothing to distinguish it from a show that had actually ended. Reported as
   * exactly that.
   *
   * The episodes from this one onward, not all of them: Continue means "carry
   * on from here", and queueing the earlier ones would make the previous
   * button walk back through episodes already finished. Same rule the detail
   * page's Continue follows, and the same shape — history state rather than the
   * URL, because a long series is far too many ids for a query string.
   *
   * A film in a collection takes the same path through its franchise.
   */
  const resume = async () => {
    /*
     * A show resolves to an episode before anything is played.
     *
     * This is the third button that answers "what comes next", and it was the
     * one that answered it differently: a show fell through to
     * `/watch/${item.id}` and handed the *container* to the player. A show's
     * path is a directory, so the server refused it at the containment check —
     * correctly, since there is no file there — and the client reported that as
     * the server being too busy to convert anything. A healthy server, a
     * misleading message, and the hero on the home page unable to play the
     * thing it was advertising.
     *
     * showContinueTarget is the one path that asks, and its own comment says
     * why there may not be a second: "a show cannot be allowed two answers to
     * what comes next". It counted two buttons and there were three.
     *
     * A failure opens the show page rather than dying quietly — it can explain
     * itself and offer Play, which is the same fallback the shelf tile takes.
     */
    if (item.kind === "show") {
      try {
        const target = await showContinueTarget(item.id);
        if (target.kind === "play") {
          navigate(`/watch/${target.episodeID}`, {
            state: { queue: target.queue },
          });
          return;
        }
      } catch {
        // Falls through to the show page below.
      }
      navigate(`/item/${item.id}`);
      return;
    }

    /*
     * An episode carries on through its show; a film in a collection carries
     * on through the franchise (continueQueue). Reported: a collection's next
     * film, resumed from here, played and stopped.
     *
     * Only when resuming or continuing a franchise. "Just added" and a pinned
     * film are a film somebody chose on its own, and pressing Play on one is
     * not a request for the rest of its box set.
     */
    const carriesOn =
      item.kind === "episode" || reason === "resuming" || !!item.continues_from;
    const queue = carriesOn ? await continueQueue(item) : undefined;
    navigate(`/watch/${item.id}`, queue ? { state: { queue } } : undefined);
  };
  const reduced = usePrefersReducedMotion();
  const parallaxRef = useParallax(!reduced);

  const fanart = artworkURL(item.artwork?.fanart, "fanart");
  const poster = artworkURL(item.artwork?.poster, "poster");
  const pct = progressPct(item);
  const score = rating(item.rating);
  const length = item.duration_ms ? runtime(item.duration_ms) : null;

  const meta = [
    item.year ? String(item.year) : null,
    length,
    item.content_rating,
    score ? `★ ${score}` : null,
  ].filter(Boolean) as string[];

  return (
    <section
      className="hero"
      aria-label={label}
      onContextMenu={
        hasMenu
          ? (e) => {
              e.preventDefault();
              setByKey(false);
              setMenuAt({ x: e.clientX, y: e.clientY });
            }
          : undefined
      }
    >
      <div className="hero__backdrop" ref={parallaxRef}>
        {fanart && (
          <div
            className="hero__art"
            style={{ backgroundImage: `url(${fanart})` }}
            role="img"
            aria-label=""
          />
        )}
        {/* Tint and vignette belong to the art — they sit above it and below
            the scrims, so the backdrop is part of the field before anything
            starts darkening it for legibility. */}
        <div className="hero__tint" />
        <div className="hero__vignette" />
        {/* Three scrims, not one. Upward carries the legibility requirement;
            leftward keeps the type column dark under a bright image; downward
            gives the floating nav a bed to sit on. */}
        <div className="hero__scrim" />
        <div className="hero__scrim hero__scrim--side" />
        <div className="hero__scrim hero__scrim--top" />
      </div>

      <div className="hero__body">
        {poster && (
          <div
            className={
              "hero__poster" + (isSquareArt(item) ? " hero__poster--square" : "")
            }
          >
            <img src={poster} alt="" draggable={false} />
          </div>
        )}

        <div className="hero__text">
          <span className="section-label hero__eyebrow">{label}</span>
          <h1 className="hero__title">{item.title}</h1>
          {meta.length > 0 && (
            <div className="hero__meta">
              {meta.map((m, i) => (
                <span key={i}>{m}</span>
              ))}
            </div>
          )}
          {item.overview && <p className="hero__overview">{item.overview}</p>}

          {pct > 0 && (
            <div className="hero__progress" aria-hidden="true">
              <div className="hero__progress-fill" style={{ width: `${pct}%` }} />
            </div>
          )}

          <div className="hero__actions">
            <HeroButton
              primary
              /*
               * The label follows the *progress bar*, not the reason the item
               * is in the spotlight.
               *
               * Found by pinning a part-watched film: the bar was drawn and
               * the button said Play, while pressing it resumed. Before the
               * spotlight had modes the two could not disagree — resuming was
               * the only reason an item with progress appeared here — and
               * adding modes made "pinned, and 88% through" an ordinary state.
               */
              label={pct > 0 ? "Resume" : "Play"}
              onPress={resume}
            />
            <HeroButton
              label="Details"
              onPress={() => navigate(`/item/${item.id}`)}
            />
            {hasMenu && (
              <HeroButton
                label="More Options"
                onPress={() => {
                  const r = document.activeElement?.getBoundingClientRect();
                  setByKey(true);
                  setMenuAt({ x: r ? r.left : 0, y: r ? r.bottom + 4 : 0 });
                }}
              />
            )}
          </div>
        </div>
      </div>
      {menuAt && actions && (
        <PointMenu
          at={menuAt}
          actions={actions}
          autoFocus={byKey}
          onClose={() => setMenuAt(null)}
        />
      )}
    </section>
  );
}
