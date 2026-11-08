import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useBackHandler } from "@/focus/FocusController";
import "./RemoveDialog.css";
import "./ShelfTitleDialog.css";

/*
 * Asks for a shelf's title: pinning a view to Home, and renaming a pinned
 * shelf (ADR 0119 §2).
 *
 * In the page rather than `prompt()`. A native dialog in the frameless desktop
 * window leaves the web contents unable to take keyboard focus once it closes,
 * which reads as the app freezing until you alt-tab. The overlay is the one
 * RemoveDialog uses, so every question the app asks looks the same.
 *
 * Portalled to the body: the library grid's toolbar is a containing block for
 * fixed elements (its backdrop filter), so drawn in place the overlay was a
 * strip the height of the toolbar rather than the whole window.
 */
export function ShelfTitleDialog({
  heading,
  initial,
  action,
  busy,
  error,
  onSave,
  onClose,
}: {
  heading: string;
  initial: string;
  /** The save button's label: "Pin to Home", "Rename". */
  action: string;
  busy?: boolean;
  error?: string;
  onSave: (title: string) => void;
  onClose: () => void;
}) {
  const [title, setTitle] = useState(initial);
  const input = useRef<HTMLInputElement>(null);
  useBackHandler(onClose);
  useEffect(() => {
    input.current?.focus();
    input.current?.select();
  }, []);
  const trimmed = title.trim();
  const ok = trimmed.length > 0 && trimmed.length <= 80;

  return createPortal(
    <div className="removedlg__overlay" onClick={onClose}>
      <form
        className="removedlg shelfdlg"
        role="dialog"
        aria-label={heading}
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => {
          e.preventDefault();
          if (ok && !busy) onSave(trimmed);
        }}
      >
        <div className="removedlg__head">
          <span className="section-label">{heading}</span>
          <button type="button" className="removedlg__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>
        <label className="shelfdlg__label" htmlFor="shelf-title">
          Shelf title
        </label>
        <input
          id="shelf-title"
          ref={input}
          className="shelfdlg__input"
          value={title}
          maxLength={80}
          onChange={(e) => setTitle(e.target.value)}
        />
        {error && <p className="removedlg__error">{error}</p>}
        <div className="shelfdlg__actions">
          <button type="button" className="removedlg__cancel" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="shelfdlg__save" disabled={!ok || busy}>
            {action}
          </button>
        </div>
      </form>
    </div>,
    document.body,
  );
}
