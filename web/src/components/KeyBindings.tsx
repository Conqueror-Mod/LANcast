import { useEffect, useState } from "react";
import { useBindings, keyLabel, bindingLabel, type Binding } from "@/lib/keys";
import "./KeyBindings.css";

/*
 * The keyboard map, editable.
 *
 * The pane used to be a printed list: here is what the keys do, and no, you
 * cannot change them. That is fine until a layout gets in the way — `[` and `]`
 * for subtitle tracks are one key on a US keyboard and a dead key on several
 * European ones, and a shortcut you physically cannot press is not a shortcut.
 *
 * Capture, not typing. A field you type "ArrowLeft" into is a field that can be
 * given a key that does not exist, so the row listens for the next real key
 * press and records what the browser reports. That also means the map ends up
 * correct for a remote control, which emits whatever it emits and does not care
 * what we would have called it.
 *
 * Three rules make it safe to give somebody this control at all:
 *
 *   - Escape and the arrows are fixed. They are how you leave a screen and how
 *     you move between tiles, and a customizer that lets you strand yourself on
 *     a page you cannot escape is a trap.
 *   - Escape *during* capture cancels rather than binding. It is the one key
 *     somebody presses to mean "no", so it cannot mean "yes, this one".
 *   - A key already in use is refused with the name of the binding holding it,
 *     rather than silently taken. Two actions on one key is a map that does the
 *     wrong one.
 */
export function KeyBindings() {
  const { bindings, rebind, reset, resetAll, changed } = useBindings();
  const [capturing, setCapturing] = useState<string | null>(null);
  const [refused, setRefused] = useState<string | null>(null);

  useEffect(() => {
    if (!capturing) return;
    const target = bindings.find((b) => b.id === capturing);
    if (!target) return;

    const onKey = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();

      if (e.key === "Escape") {
        setCapturing(null);
        return;
      }
      // A modifier on its own is somebody halfway through a chord, not a
      // choice. Waiting is what makes Shift+? possible to press.
      if (["Shift", "Control", "Alt", "Meta"].includes(e.key)) return;

      const clash = bindings.find(
        (b) => b.id !== target.id && b.keys.includes(e.key),
      );
      if (clash) {
        // Named, not just refused — "that key is taken" leaves somebody
        // hunting for which one took it. The meaning keeps its own casing;
        // lowercasing it produced "S is already search everything".
        setRefused(
          `${keyLabel(e.key)} is already used for "${clash.meaning}"`,
        );
        setCapturing(null);
        return;
      }

      // A binding with several keys keeps its shape: rebinding "play / pause"
      // replaces the first key and leaves the alternative, because losing K
      // because you set Space was never what anybody meant.
      const next = [...target.keys];
      next[0] = e.key;
      rebind(target.id, next);
      setRefused(null);
      setCapturing(null);
    };

    // Capture phase, so the app's own handlers do not act on the key being
    // *assigned*. Pressing F to bind it should not also go fullscreen.
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [capturing, bindings, rebind]);

  const group = (label: string, scope: Binding["scope"]) => (
    <div className="keybind__group">
      <span className="keybind__grouplabel">{label}</span>
      {bindings
        .filter((b) => b.scope === scope)
        .map((b) => (
          <div className="keybind__row" key={b.id}>
            <span className="keybind__meaning">{b.meaning}</span>
            {b.fixed ? (
              // Shown, not hidden. "Escape goes back" is the single most useful
              // line on this pane, and removing it because it cannot be changed
              // would make the map incomplete to save a column.
              <span className="keybind__fixed" title="This one cannot be changed">
                <kbd>{bindingLabel(b)}</kbd>
              </span>
            ) : (
              <>
                <button
                  className={
                    "keybind__key" + (capturing === b.id ? " is-capturing" : "")
                  }
                  onClick={() =>
                    setCapturing(capturing === b.id ? null : b.id)
                  }
                  aria-label={`Change the key for ${b.meaning}`}
                >
                  {capturing === b.id ? (
                    <span className="keybind__prompt">Press a key…</span>
                  ) : (
                    <kbd>{bindingLabel(b)}</kbd>
                  )}
                </button>
                <button
                  className="keybind__reset"
                  onClick={() => reset(b.id)}
                  disabled={!changed(b.id)}
                  title="Back to the default"
                >
                  Reset
                </button>
              </>
            )}
          </div>
        ))}
    </div>
  );

  return (
    <section className="settings__section">
      <span className="section-label">Keyboard</span>
      <p className="keybind__intro">
        Click a key to change it, then press the one you want. Escape cancels.
        These are stored on this device.
      </p>

      {refused && (
        <p className="keybind__refused" role="alert">
          {refused}
        </p>
      )}

      <div className="keybind">
        {group("Anywhere", "global")}
        {group("Playing", "player")}
      </div>

      <button className="keybind__resetall" onClick={resetAll}>
        Reset every shortcut
      </button>
    </section>
  );
}
