import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useBackHandler } from "@/focus/FocusController";
import "./Menu.css";

/*
 * One menu, two ways of being anchored.
 *
 * This began as RowMenu inside LibrarySettings, with a comment saying a shared
 * component extracted before a second caller exists is a guess about what the
 * second caller will need. The second caller arrived — a context menu on a
 * poster tile — so this is that extraction, made against two real callers
 * rather than one imagined one.
 *
 * They differ in exactly one thing: what the list is positioned against. The
 * libraries pane hangs it under a button you pressed; a tile opens it where the
 * pointer was. Everything else — dismissal, focus, roles, the item styling — is
 * shared, and was going to be duplicated and then drift.
 *
 * What is deliberately *not* here is a keyboard route to opening one. The
 * libraries pane has a real trigger button, so Tab and Enter reach it; a tile
 * has no trigger, and right-click is the only way in today. See MenuItems on
 * PosterTile for what that costs and where it is written down.
 */

/** A point in viewport coordinates, for a menu opened by a pointer. */
export type MenuPoint = { x: number; y: number };

export type MenuAction = {
  label: string;
  onSelect: () => void;
  /** Muted until hovered, the way every destructive control on this client is. */
  danger?: boolean;
  disabled?: boolean;
};

/*
 * MenuList is the list itself, positioned by whoever renders it.
 *
 * Split from the two anchoring shells so that the roles, the item markup and
 * the dismissal rules have one home. A menu that is a `<div role="menu">` in
 * one place and something else in another is the kind of difference nobody
 * notices until a screen reader does.
 *
 * It is also where Escape is handled, and it is handled the way the rest of
 * this client handles it. The first version of these menus listened on the
 * document for the key itself, which FocusController forbids in as many words:
 * "Escape is Back/close, resolved centrally so no screen wires its own key."
 * A private listener does not nest — the menu and whatever it opened over both
 * answer, or neither does — and the symptom was a context menu that Escape
 * simply did not close. Every other dismissible surface here (RemoveDialog,
 * AddToPlaylist, PhotoViewer, FixMatch, DirectoryPicker) already used the
 * central one; this was the odd one out.
 *
 * Registering here rather than in the shells is what makes it conditional: the
 * list is mounted only while the menu is open, so a closed ButtonMenu does not
 * sit on the screen's Escape.
 */
function MenuList({
  actions,
  onDone,
}: {
  actions: MenuAction[];
  onDone: () => void;
}) {
  // Stable, because useBackHandler re-registers whenever the function identity
  // changes and callers pass an inline arrow.
  const done = useRef(onDone);
  done.current = onDone;
  useBackHandler(useCallback(() => done.current(), []));

  return (
    <>
      {actions.map((a, i) => (
        <button
          key={i}
          type="button"
          role="menuitem"
          className={"menu__item" + (a.danger ? " menu__item--danger" : "")}
          disabled={a.disabled}
          onClick={() => {
            a.onSelect();
            onDone();
          }}
        >
          {a.label}
        </button>
      ))}
    </>
  );
}

/*
 * useDismiss closes a menu on a click outside it.
 *
 * Escape is not here — that is MenuList's, through the central back handler.
 * Shared because getting this half wrong is silent too: a menu that only closes
 * when you pick something is a menu you cannot back out of, and one that closes
 * on any click at all eats the click meant for the page behind it.
 */
function useDismiss(
  open: boolean,
  ref: React.RefObject<HTMLElement>,
  close: () => void,
) {
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
}

/**
 * ButtonMenu hangs a menu under a trigger button of its own.
 *
 * The libraries pane's shape: a row that would otherwise carry five buttons
 * keeps the one people came for and puts the rest one level down.
 */
export function ButtonMenu({
  label,
  disabled,
  className,
  actions,
}: {
  label: string;
  disabled?: boolean;
  /** Extra classes for the trigger, so a pane can match its own buttons. */
  className?: string;
  actions: MenuAction[];
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);

  /*
   * Closing puts focus back on the trigger.
   *
   * Without it the menu is a keyboard dead end, which docs/design.md names as a
   * bug outright: the items are real buttons, so Tab walks into them, and when
   * the menu unmounts underneath the focused item focus falls to <body>. On a
   * pane holding a row per library that means tabbing from the top of the
   * screen again to get back to where you were.
   *
   * Only when focus is actually inside the menu — restoring it after an outside
   * click would yank the caret out of whatever the person just clicked on.
   */
  const restoreFocus = () => {
    if (ref.current?.contains(document.activeElement)) trigger.current?.focus();
    setOpen(false);
  };

  useDismiss(open, ref, () => setOpen(false));

  return (
    <div className="menu" ref={ref}>
      <button
        type="button"
        ref={trigger}
        className={"menu__trigger " + (className ?? "")}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
      >
        {/* Three dots, not an icon font: this client ships no icon dependency. */}
        ⋯
      </button>
      {open && (
        <div className="menu__list" role="menu">
          <MenuList actions={actions} onDone={restoreFocus} />
        </div>
      )}
    </div>
  );
}

/**
 * PointMenu is a menu opened where the pointer was.
 *
 * Rendered fixed to the viewport rather than inside whatever opened it: a
 * shelf scrolls horizontally and clips its overflow, so a menu positioned
 * within one would be cut off by the very container it belongs to.
 */
export function PointMenu({
  at,
  actions,
  onClose,
}: {
  at: MenuPoint;
  actions: MenuAction[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState(at);

  /*
   * Kept on screen, measured rather than guessed.
   *
   * A tile at the right-hand edge of a shelf opens a menu that would run off
   * the window, and the last row of a grid opens one that runs off the bottom.
   * Both are only knowable after the list has a size, which is why this is a
   * layout effect: doing it in a plain effect paints the menu in the wrong
   * place first, and a menu that visibly jumps looks broken.
   */
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const margin = 8;
    let { x, y } = at;
    if (x + r.width > window.innerWidth - margin) {
      x = Math.max(margin, window.innerWidth - r.width - margin);
    }
    if (y + r.height > window.innerHeight - margin) {
      y = Math.max(margin, window.innerHeight - r.height - margin);
    }
    setPos({ x, y });
  }, [at]);

  useDismiss(true, ref, onClose);

  return (
    <div
      className="menu__list menu__list--point"
      role="menu"
      ref={ref}
      style={{ left: pos.x, top: pos.y }}
    >
      <MenuList actions={actions} onDone={onClose} />
    </div>
  );
}
