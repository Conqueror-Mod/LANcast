import { useEffect, useRef, useState, type RefObject } from "react";
import { useSearchParams } from "react-router";
import { useIsAdmin } from "@/api/hooks";
import { useFocusZone } from "@/focus/FocusController";
import {
  ACCOUNT_PANES,
  DEVICE_PANES,
  SERVER_PANES,
  STATUS_PANES,
  normalize,
  searchSettings,
  type Pane,
  type SettingMatch,
} from "./settingsIndex";
import { PANE_VIEWS } from "./settings/panes";
import "./Settings.css";

// Kept importable from here, where their tests already look for them.
export { TrustedProxiesSection } from "./settings/network";
export { PrerollSection } from "./settings/progress";

/*
 * The settings shell.
 *
 * Everything used to be one column: eight sections stacked in a single scroll,
 * so finding the log meant passing every library, every provider key and every
 * user on the way down, and the page got longer with each release. Plex answers
 * this with a vertical list of categories on the left, and the answer is right —
 * settings are looked up, not read through.
 *
 * The pane lives in the URL, like every other view state in this client, so a
 * category is linkable and survives a reload.
 *
 * Four groups, split by who the setting belongs to rather than by subject
 * (settingsIndex.ts): what the server is doing, how it is set up, your
 * account, and this device. That distinction is the one that actually matters
 * when two people use the same server, and it is invisible in a flat list.
 *
 * This file is only the shell: the nav, the search and the redirects. What
 * each pane draws lives in settings/, one file per pane, looked up through
 * PANE_VIEWS.
 */

/*
 * Where a pane that was renamed or split now lives.
 *
 * Links outlive a restructure: the tray asks for ?pane=updates, an older
 * window open against a newer server asks for ?pane=account, and somebody
 * may have a bookmark. Each lands where its contents went rather than on the
 * first pane, which would read as the link being broken.
 */
const MOVED: Record<string, string> = {
  activity: "dashboard",
  logs: "troubleshooting",
  updates: "general",
  playback: "transcoding",
  account: "profile",
  // Settings 3.0 step 2: Player became four panes; films lead.
  player: "video",
};

// Panes that only mean anything inside the desktop window. Offering either in a
// browser tab would give a heading that leads to an empty column.
// Where the list and the pane stop sitting side by side. Matches Settings.css.
const NARROW = "(max-width: 860px)";

const DESKTOP_ONLY = new Set(["app", "games", "theater"]);

// DesktopSettings renders nothing in a browser tab — there is no tray to reduce
// to and no close button LANcast owns. Offering the category anyway would give a
// browser user a heading that leads to an empty column, which is the same fault
// the player's controls avoid by appearing only when they can act. Asked the
// same way the component asks itself, so the two cannot disagree.
function desktopAvailable(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof (window as { lancastDesktopState?: unknown }).lancastDesktopState ===
      "function"
  );
}

/*
 * The row in a pane that carries a setting's label, or null.
 *
 * The deepest element whose text holds the label, then the row around it, so
 * what is scrolled to and marked is the whole setting — the switch, its name
 * and the sentence under it — rather than the one word that matched.
 */
export function findSettingRow(pane: HTMLElement, label: string): HTMLElement | null {
  const want = normalize(label);
  let best: HTMLElement | null = null;
  for (const el of pane.querySelectorAll<HTMLElement>("*")) {
    if (!normalize(el.textContent ?? "").includes(want)) continue;
    // Deeper matches come later in document order and replace their ancestors.
    if (!best || best.contains(el)) best = el;
  }
  if (!best) return null;
  return (
    best.closest<HTMLElement>(".set-row, .set-toggle, .set-lib, .set-add, label") ??
    best.closest<HTMLElement>(".settings__section") ??
    best
  );
}

/*
 * After a search result opens its pane, scroll to the setting and mark it.
 *
 * Most panes draw nothing until the server has answered, so the row is waited
 * for rather than looked for once, and given up on after a few seconds: the
 * pane is open either way, which is most of the answer.
 */
function useFoundSetting(
  paneRef: RefObject<HTMLDivElement | null>,
  found: { label: string; at: number } | null,
) {
  useEffect(() => {
    const pane = paneRef.current;
    if (!found || !pane) return;
    let marked: HTMLElement | null = null;
    let unmark: ReturnType<typeof setTimeout> | undefined;
    const attempt = () => {
      const row = findSettingRow(pane, found.label);
      if (!row) return false;
      marked = row;
      row.classList.add("is-found");
      row.scrollIntoView?.({ block: "center", behavior: "smooth" });
      // Focus the control itself, so a keyboard or a remote is already on it.
      row.querySelector<HTMLElement>("input, select, textarea, button")?.focus({ preventScroll: true });
      unmark = setTimeout(() => row.classList.remove("is-found"), 2400);
      return true;
    };
    if (attempt()) return () => clearTimeout(unmark);
    const observer = new MutationObserver(() => {
      if (attempt()) observer.disconnect();
    });
    observer.observe(pane, { childList: true, subtree: true, characterData: true });
    const giveUp = setTimeout(() => observer.disconnect(), 5000);
    return () => {
      observer.disconnect();
      clearTimeout(giveUp);
      clearTimeout(unmark);
      (marked as HTMLElement | null)?.classList.remove("is-found");
    };
  }, [paneRef, found]);
}

/*
 * The category column is never taller than the window below where it starts.
 *
 * It sticks 60px from the top once the page scrolls, but at rest it starts
 * under the page's heading, further down; a fixed CSS height left its last
 * entries below the fold until the page scrolled, which on a short pane it
 * never can. So its height follows its own top: the space actually left
 * beneath it, as the page scrolls and the window resizes. Not on a phone,
 * where the list is the page and the page scrolls it.
 */
function useNavFit(nav: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const el = nav.current;
    if (!el) return;
    const narrow =
      typeof window.matchMedia === "function" ? window.matchMedia(NARROW) : null;
    // Run on each scroll as it comes: one measurement and one style write,
    // and an animation frame is not guaranteed to run promptly in every
    // window this page is shown in.
    const fit = () => {
      if (narrow?.matches) {
        el.style.maxHeight = "";
        return;
      }
      const top = Math.max(0, el.getBoundingClientRect().top);
      el.style.maxHeight = `${Math.max(160, window.innerHeight - top - 16)}px`;
    };
    fit();
    window.addEventListener("scroll", fit, { passive: true });
    window.addEventListener("resize", fit);
    narrow?.addEventListener?.("change", fit);
    return () => {
      window.removeEventListener("scroll", fit);
      window.removeEventListener("resize", fit);
      narrow?.removeEventListener?.("change", fit);
      el.style.maxHeight = "";
    };
  }, [nav]);
}

export function Settings() {
  const isAdmin = useIsAdmin();
  const [params, setParams] = useSearchParams();

  const status = isAdmin ? STATUS_PANES : [];
  const server = isAdmin ? SERVER_PANES : [];
  const account = ACCOUNT_PANES;
  const device = DEVICE_PANES.filter(
    (x) => !DESKTOP_ONLY.has(x.id) || desktopAvailable(),
  );
  const all = [...status, ...server, ...account, ...device];
  // An unknown or absent pane falls back to the first one the user may see,
  // rather than rendering an empty column — a link to an admin pane followed by
  // a demotion should land somewhere, not nowhere. For an administrator that
  // is the Dashboard; for anyone else, their Profile.
  const asked = params.get("pane") ?? "";
  const requested = MOVED[asked] ?? asked;
  const pane = all.some((x) => x.id === requested)
    ? requested
    : (all[0]?.id ?? "profile");

  // The open pane's view. Admin panes are never in `all` for anyone else, so
  // this check is a second lock on the same door, not the only one.
  const adminOnly = [...STATUS_PANES, ...SERVER_PANES].some((x) => x.id === pane);
  const View = !adminOnly || isAdmin ? PANE_VIEWS[pane] : undefined;

  /*
   * On a phone, Settings is a list first and a pane second (settings-3-plan
   * §8): with every category above the pane, the nav filled most of the screen
   * and a chosen pane showed about a row and a half. `listing` is true when no
   * pane is named in the URL. Only the narrow layout acts on it (Settings.css);
   * a wide screen still opens the first pane, as it always has.
   *
   * Going from the list into a pane adds a history entry, so a phone's own back
   * gesture returns to the list. Moving between panes replaces it, as before.
   */
  const listing = !params.get("pane");
  const go = (id: string) =>
    setParams(
      (prev) => {
        prev.set("pane", id);
        return prev;
      },
      { replace: !listing },
    );
  const backToList = () =>
    setParams(
      (prev) => {
        prev.delete("pane");
        return prev;
      },
      { replace: true },
    );
  const paneLabel = all.find((x) => x.id === pane)?.label ?? "";

  // A pane chosen on a phone starts at its top, not wherever the list was.
  useEffect(() => {
    if (listing) return;
    if (typeof window.matchMedia === "function" && window.matchMedia(NARROW).matches) {
      window.scrollTo(0, 0);
    }
  }, [pane, listing]);

  const [query, setQuery] = useState("");
  const [found, setFound] = useState<{ label: string; pane: string; at: number } | null>(
    null,
  );
  const paneRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const matches = searchSettings(query, new Set(all.map((x) => x.id)));

  const choose = (m: SettingMatch) => {
    setQuery("");
    go(m.pane);
    // A pane's own name opens the pane and no more; a setting is looked for.
    setFound(
      m.label === m.paneLabel ? null : { label: m.label, pane: m.pane, at: Date.now() },
    );
  };

  // React Router 7 changes the URL inside a transition, so `found` commits
  // before the pane does. Looked for straight away, the setting was searched
  // for in the pane being left. It waits until its own pane is the open one.
  useFoundSetting(paneRef, found?.pane === pane ? found : null);
  const navRef = useRef<HTMLElement>(null);
  useNavFit(navRef);
  const zone = useFocusZone();

  const navGroup = (label: string, panes: Pane[]) =>
    panes.length > 0 && (
      <div className="settings__navgroup">
        <span className="section-label">{label}</span>
        {panes.map((x) => (
          <button
            key={x.id}
            className={
              "settings__navitem" + (pane === x.id ? " is-active" : "")
            }
            aria-current={pane === x.id ? "page" : undefined}
            onClick={() => go(x.id)}
          >
            {x.label}
          </button>
        ))}
      </div>
    );

  return (
    /*
     * A focus zone, so arrow keys and a TV remote move through the nav and
     * every pane's controls (ADR 0004). None of them registered before, and
     * on a TV Settings could not be moved through at all (settings-3-plan §8).
     */
    <div className="settings" ref={zone} data-showing={listing ? "list" : "pane"}>
      <h1 className="settings__title">Settings</h1>

      <div className="settings__layout">
        <nav
          ref={navRef}
          className="settings__nav"
          aria-label="Settings categories"
          data-focus-column
        >
          <input
            ref={searchRef}
            className="set-input settings__search"
            type="search"
            placeholder="Search settings"
            aria-label="Search settings"
            aria-controls="settings-search-results"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && matches[0]) {
                e.preventDefault();
                choose(matches[0]);
              } else if (e.key === "Escape" && query) {
                // Clears the box rather than leaving the page.
                e.preventDefault();
                e.stopPropagation();
                setQuery("");
              } else if (e.key === "ArrowDown" && matches[0]) {
                e.preventDefault();
                e.stopPropagation();
                resultsRef.current?.querySelector<HTMLButtonElement>("button")?.focus();
              }
            }}
          />
          {query.trim() !== "" ? (
            <div
              ref={resultsRef}
              id="settings-search-results"
              className="settings__navgroup settings__results"
              role="list"
              aria-label="Matching settings"
              onKeyDown={(e) => {
                if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Escape") return;
                e.preventDefault();
                e.stopPropagation();
                if (e.key === "Escape") {
                  searchRef.current?.focus();
                  return;
                }
                const items = [...(resultsRef.current?.querySelectorAll<HTMLButtonElement>("button") ?? [])];
                const at = items.indexOf(document.activeElement as HTMLButtonElement);
                const next = e.key === "ArrowDown" ? at + 1 : at - 1;
                if (next < 0) searchRef.current?.focus();
                else items[Math.min(next, items.length - 1)]?.focus();
              }}
            >
              {matches.length === 0 ? (
                <p className="settings__noresult">No setting matches &ldquo;{query.trim()}&rdquo;.</p>
              ) : (
                matches.map((m) => (
                  <button
                    key={m.pane + m.label}
                    role="listitem"
                    className="settings__navitem settings__result"
                    onClick={() => choose(m)}
                  >
                    <span className="settings__result-label">{m.label}</span>
                    <span className="settings__result-where">
                      {m.label === m.paneLabel ? m.group : `${m.group} · ${m.paneLabel}`}
                    </span>
                  </button>
                ))
              )}
            </div>
          ) : (
            <>
              {navGroup("Status", status)}
              {navGroup("Server", server)}
              {navGroup("Your account", account)}
              {navGroup("This device", device)}
            </>
          )}
        </nav>

        <div className="settings__pane" ref={paneRef} data-focus-column>
          {/* Drawn only in the narrow layout, where the list is not beside the pane. */}
          <div className="settings__panehead">
            <button type="button" className="settings__back" onClick={backToList}>
              &lsaquo; Settings
            </button>
            <h2 className="settings__panetitle">{paneLabel}</h2>
          </div>
          {View && <View />}
        </div>
      </div>
    </div>
  );
}
