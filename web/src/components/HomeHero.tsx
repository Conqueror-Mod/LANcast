import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { artworkURL } from "@/api/client";
import { useFocusable } from "@/focus/FocusController";
import { isSquareArt } from "@/lib/kind";
import { rating, runtime } from "@/lib/format";
import type { Item } from "@/api/types";
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
export function HomeHero({ item, resuming }: { item: Item; resuming: boolean }) {
  const navigate = useNavigate();
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
    <section className="hero" aria-label={resuming ? "Continue watching" : "Just added"}>
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
          <span className="section-label hero__eyebrow">
            {resuming ? "Continue watching" : "Just added"}
          </span>
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
              label={resuming ? "Resume" : "Play"}
              onPress={() => navigate(`/watch/${item.id}`)}
            />
            <HeroButton
              label="Details"
              onPress={() => navigate(`/item/${item.id}`)}
            />
          </div>
        </div>
      </div>
    </section>
  );
}
