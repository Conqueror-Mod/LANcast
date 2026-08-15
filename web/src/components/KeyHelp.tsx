import { useEffect, useState } from "react";
import { useBindings, bindingLabel, matchesBinding } from "@/lib/keys";
import "./KeyHelp.css";

/*
 * The keys, where you are.
 *
 * They were only in Settings — which is where you go to read about the keys,
 * not where you are when you want one. That is mid-film, with the chrome hidden
 * and three screens between you and the answer.
 *
 * Opened with ? and closed with anything: Escape, the button, or ? again. A
 * help panel that is hard to dismiss is worse than no help panel, and this one
 * appears over a film.
 */
export function KeyHelp() {
  const [open, setOpen] = useState(false);
  // Read live rather than from a module constant: the point of a customizer is
  // that the overlay shows *your* keys, and an overlay still listing the
  // defaults would be the one screen that lies about them.
  const { bindings } = useBindings();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      // Not while typing. "?" is a character somebody may want in a search box,
      // and stealing it there would be worse than not having the shortcut.
      if (
        el &&
        (el.tagName === "INPUT" ||
          el.tagName === "TEXTAREA" ||
          el.isContentEditable)
      ) {
        return;
      }
      // The binding, not the literal — this is the shortcut most likely to be
      // rebound, because "?" is Shift+/ on some layouts and unreachable on
      // others.
      if (matchesBinding("help", e.key)) {
        e.preventDefault();
        setOpen((v) => !v);
        return;
      }
      // Escape closes this before it means anything else. It is registered
      // directly rather than through the back stack because the overlay is not
      // a screen — it is a thing on top of one, and it must not consume Escape
      // when it is not showing.
      if (e.key === "Escape") {
        setOpen((wasOpen) => {
          if (wasOpen) e.stopImmediatePropagation();
          return false;
        });
      }
    };
    // Capture, so closing the overlay wins over the player's Back handler for
    // the same key press.
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, []);

  if (!open) return null;

  return (
    <div className="keyhelp__overlay" onClick={() => setOpen(false)}>
      <div
        className="keyhelp"
        role="dialog"
        aria-label="Keyboard shortcuts"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="keyhelp__head">
          <span className="section-label">Keyboard</span>
          <button
            className="keyhelp__x"
            onClick={() => setOpen(false)}
            aria-label="Close"
          >
            ✕
          </button>
        </div>
        <div className="keyhelp__cols">
          <div>
            <span className="keyhelp__group">Anywhere</span>
            {bindings
              .filter((b) => b.scope === "global")
              .map((b) => (
                <div className="keyhelp__row" key={b.id}>
                  <kbd>{bindingLabel(b)}</kbd>
                  <span>{b.meaning}</span>
                </div>
              ))}
          </div>
          <div>
            <span className="keyhelp__group">Playing</span>
            {bindings
              .filter((b) => b.scope === "player")
              .map((b) => (
                <div className="keyhelp__row" key={b.id}>
                  <kbd>{bindingLabel(b)}</kbd>
                  <span>{b.meaning}</span>
                </div>
              ))}
          </div>
        </div>
      </div>
    </div>
  );
}
