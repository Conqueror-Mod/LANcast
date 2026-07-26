import { useCallback, useEffect, useRef, useState } from "react";
import "./Splash.css";

// Shown once per browser session (not on every route change or reload within a
// session), so it is a greeting, not a toll booth. It plays the animated logo,
// then fades to reveal the app.
const SEEN_KEY = "lancast-splash-seen";

function reduceMotion(): boolean {
  return (
    typeof window !== "undefined" &&
    !!window.matchMedia &&
    window.matchMedia("(prefers-reduced-motion: reduce)").matches
  );
}

export function Splash() {
  const [show, setShow] = useState(() => {
    try {
      return !sessionStorage.getItem(SEEN_KEY);
    } catch {
      return false;
    }
  });
  const [leaving, setLeaving] = useState(false);
  const reduced = useRef(reduceMotion());

  const dismiss = useCallback(() => {
    setLeaving((already) => {
      if (already) return already;
      try {
        sessionStorage.setItem(SEEN_KEY, "1");
      } catch {
        /* private mode: it simply shows again next session */
      }
      window.setTimeout(() => setShow(false), 650);
      return true;
    });
  }, []);

  useEffect(() => {
    if (!show) return;
    // The animation is a courtesy; it must never hold the app hostage if the
    // video stalls or a browser blocks autoplay. A hard cap always dismisses.
    const cap = window.setTimeout(dismiss, reduced.current ? 1400 : 5500);
    const skip = () => dismiss();
    window.addEventListener("keydown", skip);
    return () => {
      window.clearTimeout(cap);
      window.removeEventListener("keydown", skip);
    };
  }, [show, dismiss]);

  if (!show) return null;

  return (
    <div
      className={"splash" + (leaving ? " splash--leaving" : "")}
      onClick={dismiss}
      role="presentation"
      aria-hidden="true"
    >
      {reduced.current ? (
        // No motion: the logo, held briefly, then faded. Same greeting, still.
        <img className="splash__logo" src="/splash-poster.png" alt="" />
      ) : (
        <video
          className="splash__logo"
          src="/splash.mp4"
          // No poster: the clip now opens on black and fades the logo up, so a
          // poster of the settled logo would flash it, then jump back to black.
          autoPlay
          muted
          playsInline
          onEnded={(e) => {
            // Only a genuine end dismisses. A stalled or non-composited element
            // can fire "ended" at zero; ignore that and let the safety cap (or a
            // real finish) handle it, so the greeting never flashes past.
            const v = e.currentTarget;
            if (!v.duration || v.currentTime >= v.duration - 0.5) dismiss();
          }}
        />
      )}
    </div>
  );
}
