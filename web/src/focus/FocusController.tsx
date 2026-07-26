import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useRef,
  type ReactNode,
} from "react";

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
}

interface FocusAPI {
  register: (id: string, entry: Entry) => void;
  unregister: (id: string) => void;
  setSelect: (id: string, onSelect?: () => void) => void;
  focusFirst: () => void;
  setBackHandler: (fn: (() => void) | null) => void;
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
  dir: Dir,
  entries: Map<string, Entry>,
  self: string,
): HTMLElement | null {
  const fx = from.left + from.width / 2;
  const fy = from.top + from.height / 2;

  let best: HTMLElement | null = null;
  let bestScore = Infinity;

  for (const [id, { el }] of entries) {
    if (id === self) continue;
    const r = el.getBoundingClientRect();
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
  const backHandler = useRef<(() => void) | null>(null);

  const setBackHandler = useCallback((fn: (() => void) | null) => {
    backHandler.current = fn;
  }, []);

  const setCurrent = useCallback((id: string | null) => {
    const prev = currentID.current;
    if (prev && prev !== id) {
      const p = entries.current.get(prev);
      if (p) p.el.tabIndex = -1;
    }
    currentID.current = id;
    if (id) {
      const e = entries.current.get(id);
      if (e) e.el.tabIndex = 0;
    }
  }, []);

  const register = useCallback(
    (id: string, entry: Entry) => {
      entries.current.set(id, entry);
      // The first focusable to appear becomes the tab stop, so the page is
      // keyboard-reachable without a click.
      if (currentID.current === null) setCurrent(id);
      else entry.el.tabIndex = -1;
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

  const setSelect = useCallback((id: string, onSelect?: () => void) => {
    const e = entries.current.get(id);
    if (e) e.onSelect = onSelect;
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
    if (e.key === "Escape" && backHandler.current) {
      e.preventDefault();
      backHandler.current();
      return;
    }

    const id = currentID.current;
    if (!id) return;
    const entry = entries.current.get(id);
    if (!entry) return;

    if (e.key === "Enter") {
      if (entry.onSelect) {
        e.preventDefault();
        entry.onSelect();
      }
      return;
    }

    const dir = KEY_TO_DIR[e.key];
    if (!dir) return;
    const target = nearest(entry.el.getBoundingClientRect(), dir, entries.current, id);
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

  const api: FocusAPI = { register, unregister, setSelect, focusFirst, setBackHandler };
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
export function useFocusable(onSelect?: () => void) {
  const api = useFocusController();
  const id = useId();
  const ref = useRef<HTMLElement | null>(null);

  const setRef = useCallback(
    (el: HTMLElement | null) => {
      if (el) {
        el.dataset.focusId = id;
        ref.current = el;
        api.register(id, { el, onSelect });
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
    api.setSelect(id, onSelect);
  }, [api, id, onSelect]);

  return { ref: setRef, tabIndex: -1 as const, "data-focus-id": id };
}

// useBackHandler registers what Escape should do for the current screen, and
// clears it on unmount so the previous screen's handler is not left dangling.
export function useBackHandler(fn: () => void) {
  const api = useFocusController();
  useEffect(() => {
    api.setBackHandler(fn);
    return () => api.setBackHandler(null);
  }, [api, fn]);
}
