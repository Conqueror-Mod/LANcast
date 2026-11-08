import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  type ReactNode,
} from "react";
import { matchesBinding } from "@/lib/keys";

/*
 * The central roving-tabindex controller (ADR 0004).
 *
 * A TV remote is a spatial device — up, down, left, right, select — and tab
 * order is linear. Retrofitting spatial navigation later means touching every
 * interactive component, so it is built from the first one. Components declare
 * themselves focusable and register their DOM element; this controller owns all
 * arrow-key resolution by geometry. A component that handles its own arrow keys
 * is a defect, not a shortcut.
 *
 * Only one focusable holds tabIndex 0 at a time (the roving part); the rest are
 * -1 and reached by arrow keys. tabIndex is set imperatively so moving focus
 * across a large grid does not re-render every tile.
 */

interface Entry {
  el: HTMLElement;
  onSelect?: () => void;
  /*
   * What the actions key opens, given the element it belongs to.
   *
   * The element rather than its rect: a menu opened this way has to be placed
   * against the thing it belongs to *and* has to give focus back to it on the
   * way out, and the second of those needs the element. The rect is one call
   * away from it; the element is not recoverable from the rect.
   */
  onMenu?: (el: HTMLElement) => void;
  /*
   * Leave the element's own tabIndex alone. A focus zone's native controls
   * set this: a form keeps its Tab order for a keyboard or a screen reader,
   * and arrows reach the same controls besides. Roving tabindex is for grids,
   * where one Tab stop among two thousand tiles is the point.
   */
  keepTab?: boolean;
}

interface FocusAPI {
  register: (id: string, entry: Entry) => void;
  unregister: (id: string) => void;
  setSelect: (id: string, onSelect?: () => void, onMenu?: (el: HTMLElement) => void) => void;
  focusFirst: () => void;
  pushBack: (fn: () => void) => () => void;
  setSuspended: (v: boolean) => void;
  pushScope: (el: HTMLElement) => () => void;
}

const FocusContext = createContext<FocusAPI | null>(null);

type Dir = "up" | "down" | "left" | "right";

const KEY_TO_DIR: Record<string, Dir> = {
  ArrowUp: "up",
  ArrowDown: "down",
  ArrowLeft: "left",
  ArrowRight: "right",
};

// nearest finds the registered element closest to `from` in direction `dir`,
// scoring travel along the axis of motion cheaply and drift across it steeply,
// so navigation prefers the visually aligned neighbour.
function nearest(
  from: DOMRect,
  fromEl: HTMLElement,
  dir: Dir,
  entries: Map<string, Entry>,
  self: string,
  scope: HTMLElement | null,
): HTMLElement | null {
  /*
   * Up and down stay inside the column they start in. A form laid out as a
   * nav beside a pane (Settings) otherwise sends Down from the pane's last
   * row into whichever nav item happens to sit lower on the screen. Left and
   * right are how a column is left.
   */
  const column = dir === "up" || dir === "down" ? fromEl.closest("[data-focus-column]") : null;
  const fx = from.left + from.width / 2;
  const fy = from.top + from.height / 2;

  let best: HTMLElement | null = null;
  let bestScore = Infinity;

  for (const [id, { el }] of entries) {
    if (id === self) continue;
    if (scope && !scope.contains(el)) continue;
    if (column && !column.contains(el)) continue;
    // A disabled control cannot take focus, and moving to one would leave
    // focus where it was; a control with no box is not on screen.
    if ((el as HTMLButtonElement).disabled) continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    const cx = r.left + r.width / 2;
    const cy = r.top + r.height / 2;
    const dx = cx - fx;
    const dy = cy - fy;

    // Must lie in the requested direction, with a small tolerance so a slightly
    // misaligned neighbour still counts.
    const tol = 8;
    let along: number;
    let across: number;
    switch (dir) {
      case "up":
        if (dy > -tol) continue;
        along = -dy;
        across = Math.abs(dx);
        break;
      case "down":
        if (dy < tol) continue;
        along = dy;
        across = Math.abs(dx);
        break;
      case "left":
        if (dx > -tol) continue;
        along = -dx;
        across = Math.abs(dy);
        break;
      case "right":
        if (dx < tol) continue;
        along = dx;
        across = Math.abs(dy);
        break;
    }

    const score = along + across * 3;
    if (score < bestScore) {
      bestScore = score;
      best = el;
    }
  }
  return best;
}

export function FocusProvider({ children }: { children: ReactNode }) {
  const entries = useRef(new Map<string, Entry>());
  const currentID = useRef<string | null>(null);
  // A stack of Escape handlers: the topmost wins, so a modal opened over a
  // screen closes first and the screen's own back handler resumes when it
  // unmounts. Each push returns its own remover.
  const backHandlers = useRef<(() => void)[]>([]);
  const suspended = useRef(false);
  /*
   * Scopes: while one is open, arrows reach only what is inside the topmost.
   *
   * A menu drawn over a screen that still has live buttons — the game menu
   * over the corner card's grip and size controls — is otherwise one arrow
   * away from them, because nearest() scores by geometry and the card is
   * simply the next thing down. A stack, like the back handlers, so a scope
   * opened inside another nests.
   */
  const scopes = useRef<HTMLElement[]>([]);

  const pushScope = useCallback((el: HTMLElement) => {
    scopes.current.push(el);
    return () => {
      const i = scopes.current.lastIndexOf(el);
      if (i >= 0) scopes.current.splice(i, 1);
    };
  }, []);

  const pushBack = useCallback((fn: () => void) => {
    backHandlers.current.push(fn);
    return () => {
      const i = backHandlers.current.lastIndexOf(fn);
      if (i >= 0) backHandlers.current.splice(i, 1);
    };
  }, []);

  // A modal transport surface (the player) suspends spatial navigation: while
  // it is open, arrows mean seek, not move-focus. This is the one place a
  // component owns its own keys, and it does so by taking the whole layer, not
  // by fighting the controller. Escape/back still resolves centrally.
  const setSuspended = useCallback((v: boolean) => {
    suspended.current = v;
  }, []);

  const setCurrent = useCallback((id: string | null) => {
    const prev = currentID.current;
    // Moving onto a zone control leaves the roving stop where it was, so the
    // grid or rail behind it stays one Tab away rather than becoming unreachable.
    const next = id ? entries.current.get(id) : undefined;
    if (prev && prev !== id && !next?.keepTab) {
      const p = entries.current.get(prev);
      if (p && !p.keepTab) p.el.tabIndex = -1;
    }
    currentID.current = id;
    if (next && !next.keepTab) next.el.tabIndex = 0;
  }, []);

  const register = useCallback(
    (id: string, entry: Entry) => {
      entries.current.set(id, entry);
      // The first focusable to appear becomes the tab stop, so the page is
      // keyboard-reachable without a click.
      if (currentID.current === null) setCurrent(id);
      else if (!entry.keepTab) entry.el.tabIndex = -1;
    },
    [setCurrent],
  );

  const unregister = useCallback(
    (id: string) => {
      entries.current.delete(id);
      if (currentID.current === id) {
        currentID.current = null;
        // Hand the tab stop to any surviving focusable.
        const next = entries.current.keys().next();
        if (!next.done) setCurrent(next.value);
      }
    },
    [setCurrent],
  );

  const setSelect = useCallback((id: string, onSelect?: () => void, onMenu?: (el: HTMLElement) => void) => {
    const e = entries.current.get(id);
    if (e) {
      e.onSelect = onSelect;
      e.onMenu = onMenu;
    }
  }, []);

  const focusFirst = useCallback(() => {
    const id = currentID.current ?? entries.current.keys().next().value;
    if (id) entries.current.get(id)?.el.focus();
  }, []);

  // Track focus so arrow keys resolve from wherever the user actually is,
  // including after a mouse click.
  const onFocusIn = useCallback(
    (e: FocusEvent) => {
      const target = e.target as HTMLElement;
      const id = target?.dataset?.focusId;
      if (id && entries.current.has(id)) setCurrent(id);
    },
    [setCurrent],
  );

  const onKeyDown = useCallback((e: KeyboardEvent) => {
    // Escape is Back/close, resolved centrally so no screen wires its own key.
    if (e.key === "Escape" && backHandlers.current.length > 0) {
      e.preventDefault();
      backHandlers.current[backHandlers.current.length - 1]();
      return;
    }

    if (suspended.current) return;

    // Never hijack arrows or Enter while the user is typing or in a select —
    // there the keys mean move-cursor and change-option, not move-focus.
    const ae = document.activeElement as HTMLElement | null;
    const typing =
      !!ae &&
      (ae.tagName === "TEXTAREA" ||
        ae.isContentEditable ||
        ae.tagName === "SELECT" ||
        (ae.tagName === "INPUT" &&
          !["checkbox", "radio", "button", "submit"].includes(
            (ae as HTMLInputElement).type,
          )));
    /*
     * Inside a focus zone (a form, such as Settings) a remote has to be able to
     * leave a control, or one dropdown is a dead end. So there:
     * - a closed dropdown gives every arrow back to navigation, and Enter opens
     *   it, where the browser's own list takes the keys;
     * - a text box or a slider gives up and down back, and keeps left and right
     *   for its cursor or its value.
     * Outside a zone nothing changes.
     */
    if (typing && ae!.closest("[data-focus-zone]") && ae!.tagName !== "TEXTAREA") {
      if (ae!.tagName === "SELECT") {
        if (e.key === "Enter") {
          e.preventDefault();
          try {
            (ae as HTMLSelectElement).showPicker();
          } catch {
            // A browser without showPicker keeps Space and Alt+Down for this.
          }
          return;
        }
        // Arrows fall through to navigation below.
      } else if (e.key !== "ArrowUp" && e.key !== "ArrowDown") {
        return;
      }
    } else if (typing && (KEY_TO_DIR[e.key] || e.key === "Enter")) return;

    const id = currentID.current;
    const scope = scopes.current[scopes.current.length - 1] ?? null;
    const entry = id ? entries.current.get(id) : undefined;
    /*
     * Focus outside the open scope (a click on the card behind a menu, or
     * nothing focused yet): a move or a press brings it into the scope,
     * onto its first focusable, rather than acting out there.
     */
    if (scope && (!entry || !scope.contains(entry.el)) && (KEY_TO_DIR[e.key] || e.key === "Enter")) {
      for (const [, en] of entries.current) {
        if (scope.contains(en.el)) {
          e.preventDefault();
          en.el.focus();
          return;
        }
      }
      return;
    }
    if (!id || !entry) return;

    if (e.key === "Enter") {
      if (entry.onSelect) {
        e.preventDefault();
        entry.onSelect();
      }
      return;
    }

    /*
     * The context menu, opened without a pointer.
     *
     * Resolved here rather than by each surface for the same reason Escape is:
     * one controller owns what a key means, and a grid of two thousand tiles
     * each listening for a keystroke is the thing this file exists to avoid.
     *
     * The element's rect goes with it, because a menu has to open somewhere and
     * "at the pointer" is not available. Anchoring to the thing it belongs to
     * is the only answer that reads correctly from a sofa.
     */
    if (matchesBinding("actions", e.key) && entry.onMenu) {
      e.preventDefault();
      entry.onMenu(entry.el);
      return;
    }

    const dir = KEY_TO_DIR[e.key];
    if (!dir) return;
    const target = nearest(entry.el.getBoundingClientRect(), entry.el, dir, entries.current, id, scope);
    if (target) {
      e.preventDefault();
      target.focus();
    }
  }, []);

  useEffect(() => {
    document.addEventListener("focusin", onFocusIn);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("focusin", onFocusIn);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [onFocusIn, onKeyDown]);

  const api: FocusAPI = {
    register,
    unregister,
    setSelect,
    focusFirst,
    pushBack,
    setSuspended,
    pushScope,
  };
  return <FocusContext.Provider value={api}>{children}</FocusContext.Provider>;
}

export function useFocusController(): FocusAPI {
  const api = useContext(FocusContext);
  if (!api) throw new Error("useFocusController must be used within FocusProvider");
  return api;
}

/*
 * useFocusable registers an element with the controller and returns the props
 * to spread onto it. The element must be a real focus target (a button, or any
 * element — tabIndex is managed for you). Pass onSelect for Enter/click.
 */
export function useFocusable(
  onSelect?: () => void,
  onMenu?: (el: HTMLElement) => void,
) {
  const api = useFocusController();
  const id = useId();
  const ref = useRef<HTMLElement | null>(null);

  const setRef = useCallback(
    (el: HTMLElement | null) => {
      if (el) {
        el.dataset.focusId = id;
        ref.current = el;
        api.register(id, { el, onSelect, onMenu });
      } else {
        api.unregister(id);
        ref.current = null;
      }
    },
    // register/unregister are stable; onSelect is kept current via setSelect.
    [api, id],
  );

  // Keep the select handler fresh without re-registering the element.
  useEffect(() => {
    api.setSelect(id, onSelect, onMenu);
  }, [api, id, onSelect, onMenu]);

  return { ref: setRef, tabIndex: -1 as const, "data-focus-id": id };
}

// useBackHandler registers what Escape should do while this component is
// mounted, on top of any handler already registered. Removing it on unmount
// restores the one beneath, so a modal over a screen nests correctly.
export function useBackHandler(fn: () => void) {
  const api = useFocusController();
  useEffect(() => api.pushBack(fn), [api, fn]);
}

/*
 * useFocusScope keeps arrows inside an element while it is mounted: put the
 * returned ref on a menu or dialog, and nothing behind it can take focus by
 * arrow or pad. Removed when the element goes.
 */
export function useFocusScope(): (el: HTMLElement | null) => void {
  const api = useFocusController();
  const remove = useRef<(() => void) | undefined>(undefined);
  return useCallback(
    (el: HTMLElement | null) => {
      remove.current?.();
      remove.current = el ? api.pushScope(el) : undefined;
    },
    [api],
  );
}

const ZONE_FOCUSABLE =
  "button, a[href], input:not([type=hidden]), select, textarea, [tabindex]:not([tabindex='-1'])";

/*
 * useFocusZone registers every native control inside an element, as it
 * appears, so arrows and a remote reach them.
 *
 * For a form, where registering each control by hand would mean touching every
 * row of every pane, and where a new row added later would quietly be out of
 * reach. Settings is the case it was written for: dozens of panes, hundreds of
 * plain buttons, switches and dropdowns, none of which were registered, so on
 * a TV the whole screen could not be moved through at all.
 *
 * Anything already registered (a useFocusable inside the zone) is left to its
 * owner. Zone controls keep their own Tab order (keepTab). A switch is
 * flipped by Enter, since a remote's OK sends Enter and a checkbox only
 * answers Space; buttons and links already answer Enter themselves.
 *
 * Without a FocusProvider (a test that renders one screen) it does nothing.
 */
export function useFocusZone(): (el: HTMLElement | null) => void {
  const api = useContext(FocusContext);
  const cleanup = useRef<(() => void) | undefined>(undefined);
  return useCallback(
    (root: HTMLElement | null) => {
      cleanup.current?.();
      cleanup.current = undefined;
      if (!root || !api) return;
      root.dataset.focusZone = "";
      const mine = new Map<string, HTMLElement>();
      let n = 0;
      const sync = () => {
        for (const [id, el] of mine) {
          if (!root.contains(el)) {
            api.unregister(id);
            mine.delete(id);
            // Cleared so the element is taken up again if it comes back.
            delete el.dataset.focusId;
          }
        }
        for (const el of root.querySelectorAll<HTMLElement>(ZONE_FOCUSABLE)) {
          if (el.dataset.focusId) continue;
          const id = `zone-${(n += 1)}-${Math.random().toString(36).slice(2, 8)}`;
          el.dataset.focusId = id;
          const toggle =
            el instanceof HTMLInputElement && (el.type === "checkbox" || el.type === "radio")
              ? () => el.click()
              : undefined;
          api.register(id, { el, onSelect: toggle, keepTab: true });
          mine.set(id, el);
        }
      };
      sync();
      const observer = new MutationObserver(sync);
      observer.observe(root, { childList: true, subtree: true });
      cleanup.current = () => {
        observer.disconnect();
        for (const [id, el] of mine) {
          api.unregister(id);
          delete el.dataset.focusId;
        }
        mine.clear();
        delete root.dataset.focusZone;
      };
    },
    [api],
  );
}

// useSuspendFocus turns off spatial navigation while a modal transport surface
// (the player) is mounted, and restores it on unmount. `active` false gives it
// back while the surface stays mounted: the browser's game player does that
// while its states panel is open, so a pad can move through the panel.
export function useSuspendFocus(active = true) {
  const api = useFocusController();
  useEffect(() => {
    if (!active) return;
    api.setSuspended(true);
    return () => api.setSuspended(false);
  }, [api, active]);
}
