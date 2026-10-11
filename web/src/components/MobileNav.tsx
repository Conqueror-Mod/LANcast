import { useEffect, useRef, useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { PeerLibraryLinks } from "@/components/PeerLibraryLinks";
import { AvatarGlyph } from "@/components/Avatar";
import {
  useLibraries,
  useCurrentUser,
  useLogout,
  useIsAdmin,
} from "@/api/hooks";
import { gamesSupported } from "@/lib/games";
import { railOrder } from "@/lib/railOrder";
import { GAME_HUB_PATH, hubOffered, insideHub } from "@/lib/gameHub";
import {
  LibraryIcon,
  HomeIcon,
  SettingsIcon,
  SearchGlyph,
  AddonIcon,
  DownloadIcon,
  GamesIcon,
  LiveIcon,
  PeopleIcon,
  SignOutIcon,
} from "./LibraryIcon";
import "./MobileNav.css";

/*
 * Navigation on a phone: a bar of tabs under the thumb, and two sheets
 * (docs/design.md, Phone).
 *
 * The desktop rail squeezed into one row was fifteen links in 375px. Its groups
 * drew over each other, and even laid out properly most of them would have
 * been off the right edge, which is where a phone hides things for good. So
 * below the shell's breakpoint the rail is not shown at all, and this is.
 *
 * Four places get a tab because they are where people go: Home, every library,
 * Search, and People. Everything else is one tap further, in More. The two
 * sheets hold exactly what the rail holds, in the rail's order, so nothing is
 * reachable on a desktop and missing on a phone.
 *
 * CSS decides which is visible (MobileNav.css). Both are always mounted, so
 * there is no width check in script to fall out of step with the stylesheet.
 */

type Sheet = "libraries" | "more" | null;

export function MobileNav() {
  const { data: libraries } = useLibraries();
  const user = useCurrentUser();
  const logout = useLogout();
  const isAdmin = useIsAdmin();
  const location = useLocation();
  const [sheet, setSheet] = useState<Sheet>(null);
  const opener = useRef<HTMLButtonElement | null>(null);
  const panel = useRef<HTMLDivElement | null>(null);

  const retroIDs = (libraries ?? []).filter((l) => l.kind === "retro").map((l) => l.id);
  const hub = hubOffered({ retroLibraries: retroIDs.length, desktop: gamesSupported(), admin: isAdmin });
  const inHub = hub && insideHub(location.pathname, retroIDs);

  // Choosing somewhere closes the sheet. Watching the location rather than
  // each link's click means the peer links, which navigate themselves, close
  // it too.
  useEffect(() => {
    setSheet(null);
  }, [location.pathname]);

  /*
   * An open sheet owns the keyboard (design.md, Keyboard model): focus moves
   * into it, Escape closes it, and focus goes back to the tab that opened it.
   */
  useEffect(() => {
    if (!sheet) return;
    panel.current?.querySelector<HTMLElement>("a, button")?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setSheet(null);
      }
    };
    document.addEventListener("keydown", onKey);
    const back = opener.current;
    return () => {
      document.removeEventListener("keydown", onKey);
      back?.focus();
    };
  }, [sheet]);

  const toggle = (which: Exclude<Sheet, null>) => (e: React.MouseEvent<HTMLButtonElement>) => {
    opener.current = e.currentTarget;
    setSheet((s) => (s === which ? null : which));
  };

  // A library tab is "where you are" inside any library, a hub or a peer's
  // library, because those are what its sheet leads to.
  const inLibraries =
    location.pathname.startsWith("/library/") ||
    location.pathname.startsWith("/peers/") ||
    location.pathname === "/live" ||
    inHub;
  const inMore = ["/settings", "/profile", "/downloads", "/addons"].some((p) =>
    location.pathname.startsWith(p),
  );

  const row = (isActive: boolean) => "mnav__row" + (isActive ? " is-active" : "");

  return (
    <>
      {sheet && (
        <div className="mnav__scrim" onClick={() => setSheet(null)} aria-hidden="true" />
      )}
      {sheet && (
        <div
          className="mnav__sheet"
          ref={panel}
          role="dialog"
          aria-modal="true"
          aria-label={sheet === "libraries" ? "Libraries" : "More"}
        >
          {sheet === "libraries" ? (
            <nav aria-label="Libraries">
              {railOrder(libraries ?? [], hub).map((entry) =>
                entry.type === "game-hub" ? (
                  <NavLink key="game-hub" to={GAME_HUB_PATH} className={() => row(inHub)}>
                    <GamesIcon />
                    <span>Game Hub</span>
                  </NavLink>
                ) : (
                  <NavLink
                    key={entry.lib.id}
                    to={`/library/${entry.lib.id}`}
                    className={({ isActive }) => row(isActive)}
                  >
                    <LibraryIcon kind={entry.lib.kind} />
                    <span>{entry.lib.name}</span>
                  </NavLink>
                ),
              )}
              <NavLink to="/live" className={({ isActive }) => row(isActive)}>
                <LiveIcon />
                <span>Live TV</span>
              </NavLink>
              {/* Other servers' libraries, under their own heading, as on the
                  rail (ADR 0071 §5). */}
              <div className="mnav__peers">
                <PeerLibraryLinks onNavigate={() => setSheet(null)} />
              </div>
            </nav>
          ) : (
            <nav aria-label="More">
              {/* The rail's order: Add-ons, then Downloads. */}
              {isAdmin && (
                <NavLink to="/addons" className={({ isActive }) => row(isActive)}>
                  <AddonIcon />
                  <span>Add-ons</span>
                </NavLink>
              )}
              <NavLink to="/downloads" className={({ isActive }) => row(isActive)}>
                <DownloadIcon />
                <span>Downloads</span>
              </NavLink>
              <NavLink to="/settings" className={({ isActive }) => row(isActive)}>
                <SettingsIcon />
                <span>Settings</span>
              </NavLink>
              {user && (
                <>
                  <NavLink to="/profile" className={({ isActive }) => row(isActive)}>
                    <AvatarGlyph avatar={user.avatar} />
                    <span>{user.name}</span>
                  </NavLink>
                  <button
                    type="button"
                    className="mnav__row"
                    onClick={() => logout.mutate()}
                    disabled={logout.isPending}
                  >
                    <SignOutIcon />
                    <span>Sign out</span>
                  </button>
                </>
              )}
            </nav>
          )}
        </div>
      )}

      <nav className="mnav" aria-label="Main">
        <NavLink to="/" end className={({ isActive }) => "mnav__tab" + (isActive && !sheet ? " is-active" : "")}>
          <HomeIcon />
          <span>Home</span>
        </NavLink>
        <button
          type="button"
          className={"mnav__tab" + (sheet === "libraries" || (!sheet && inLibraries) ? " is-active" : "")}
          aria-expanded={sheet === "libraries"}
          onClick={toggle("libraries")}
        >
          <LibraryIcon kind="movie" />
          <span>Libraries</span>
        </button>
        <NavLink to="/search" className={({ isActive }) => "mnav__tab" + (isActive && !sheet ? " is-active" : "")}>
          <SearchGlyph />
          <span>Search</span>
        </NavLink>
        <NavLink to="/people" className={({ isActive }) => "mnav__tab" + (isActive && !sheet ? " is-active" : "")}>
          <PeopleIcon />
          <span>People</span>
        </NavLink>
        <button
          type="button"
          className={"mnav__tab" + (sheet === "more" || (!sheet && inMore) ? " is-active" : "")}
          aria-expanded={sheet === "more"}
          onClick={toggle("more")}
        >
          <span className="mnav__dots" aria-hidden="true">⋯</span>
          <span>More</span>
        </button>
      </nav>
    </>
  );
}
