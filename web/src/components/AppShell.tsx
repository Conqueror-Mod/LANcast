import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { useBigscreen, useBigscreenShortcut } from "@/lib/bigscreen";
import { matchesBinding, bindingLabel, useBindings } from "@/lib/keys";
import {
  useLibraries,
  useReview,
  useCurrentUser,
  useLogout,
  useIsAdmin,
  useUpdateStatus,
  useHealth,
} from "@/api/hooks";
import { useEffect, useState, type ReactNode } from "react";
import { ActivityPanel } from "./ActivityPanel";
import { KeyHelp } from "./KeyHelp";
import { plainVersion } from "./UpdateSettings";
import { clientIsStale, type DesktopVersion } from "@/lib/clientVersion";
import { useScrollRestoration } from "@/lib/useScrollRestoration";
import {
  LibraryIcon,
  HomeIcon,
  SettingsIcon,
  SearchGlyph,
  AddonIcon,
  DownloadIcon,
  LiveIcon,
  PeopleIcon,
  AccountIcon,
  SignOutIcon,
} from "./LibraryIcon";
import "./AppShell.css";

// The shell: a vertical rail of places, and a top bar of state.
//
// The split is by what each thing *is*. The rail holds destinations — home, and
// every library — and it runs vertically because that is the axis with room to
// spare. Horizontally, library names competed with each other and with the
// account controls for the same strip of pixels, so a fourth library made the
// third one shorter. Vertically each name gets its own line and stops fighting.
//
// The top bar holds what is true right now rather than where you can go: what
// needs review, and what the server is doing. Both are transient and both are
// about the moment, so they stay at the top where a glance finds them.
//
// Settings, who you are, and signing out went to the foot of the rail. They are
// destinations and an action *about you*, not state — and every application
// that has a rail puts that group at the bottom of it, which is a convention
// worth obeying rather than being interesting about. It also stops the top bar
// being a drawer of leftovers.
//
// Library names still go straight to the full grid — hubs are a convenience,
// never a gate (the deliberate fix for the main Plex complaint).
/*
 * The rail expands on hover *and* on focus-within, and a click leaves focus on
 * the thing clicked — so choosing a library kept the rail open over the page it
 * had just navigated to, until you clicked somewhere else to dismiss it.
 *
 * Dropping focus on the way out closes it. Only for a pointer click, which is
 * what `detail > 0` distinguishes: a keyboard activation reports 0, and blurring
 * there would throw a keyboard user back to the top of the document having just
 * chosen where to go. Hover is unaffected either way — move the pointer off and
 * it closes as it always did.
 */
function releaseRail(e: React.MouseEvent<HTMLElement>) {
  if (e.detail > 0) e.currentTarget.blur();
}

export function AppShell({ children }: { children: ReactNode }) {
  const { data: libraries } = useLibraries();
  const { data: review } = useReview();
  const user = useCurrentUser();
  const logout = useLogout();
  const location = useLocation();
  const isAdmin = useIsAdmin();
  const reviewCount = review?.total ?? 0;
  const navigate = useNavigate();

  // The tooltip names the shortcut, so it reads the binding rather than a
  // literal — a tooltip advertising "/" after somebody has rebound search is
  // the same lie the overlay would have told.
  const { bindings } = useBindings();
  const searchKey = bindingLabel(bindings.find((b) => b.id === "search")!);

  // Bigscreen is one attribute on the document root, applied here because the
  // shell is the one component always mounted. index.html sets it before the
  // first paint; this keeps it in step once React owns the page.
  useBigscreen();
  useBigscreenShortcut();

  /*
   * "/" opens search, the way it does in every application with a search.
   *
   * Not while typing, and not while something is playing full-screen — the
   * player owns its keys, and pulling somebody out of a film into a search box
   * because they pressed a punctuation key would be worse than the shortcut is
   * worth.
   */
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // The binding, not the literal. A customizer whose keys the app then
      // ignores is worse than no customizer: it stores a preference, shows it
      // back, and does nothing with it.
      if (!matchesBinding("search", e.key) || e.ctrlKey || e.metaKey || e.altKey)
        return;
      const el = e.target as HTMLElement | null;
      if (
        el &&
        (el.tagName === "INPUT" ||
          el.tagName === "TEXTAREA" ||
          el.isContentEditable)
      ) {
        return;
      }
      if (location.pathname.startsWith("/watch/")) return;
      e.preventDefault();
      navigate("/search");
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [navigate, location.pathname]);
  // Back returns you to where you were, and a new page starts at the top.
  // Neither is the browser's default in a single-page app; see the hook.
  useScrollRestoration();

  return (
    <div className="app-shell">
      {/* The rail keeps its collapsed width in the layout and expands over the
          content rather than pushing it. Plex does the same, and the reason is
          worth stating: a page that slides sideways whenever the pointer
          crosses the left edge is harder to use than a narrow rail, and the
          thing being read is never the thing being hovered. */}
      <aside className="app-shell__rail">
        <div className="app-shell__rail-inner">
          <NavLink to="/" className="app-shell__brand" title="Home" onClick={releaseRail}>
            <HomeIcon />
            <span className="app-shell__label">LANCAST</span>
          </NavLink>

          <nav className="app-shell__libs" aria-label="Libraries">
            {libraries && libraries.length > 0 && (
              <span className="section-label app-shell__rail-label">
                Libraries
              </span>
            )}
            {libraries?.map((lib) => (
              <NavLink
                key={lib.id}
                to={`/library/${lib.id}`}
                title={lib.name}
                onClick={releaseRail}
                className={({ isActive }) =>
                  "app-shell__lib" + (isActive ? " is-active" : "")
                }
              >
                <LibraryIcon kind={lib.kind} />
                <span className="app-shell__lib-name app-shell__label">
                  {lib.name}
                </span>
                <span className="app-shell__lib-count app-shell__label">
                  {lib.item_count}
                </span>
              </NavLink>
            ))}

            {/* Add-ons sits at the foot of the library list rather than buried
                three levels into Settings, because it is a *place* — a thing
                with contents you go and look at — and the rail is where places
                live. It is here whether or not there are libraries: on a fresh
                install the rail is otherwise empty, which is exactly when
                somebody is looking for what else this thing can do.

                It leads to /addons — a page — rather than into Settings, which
                is the mismatch this entry carried from the start: a rail item
                that lands in Settings teaches that Add-ons is a setting.

                Still admin only, and for a narrower reason than before: the
                plugin list itself is admin-gated on the server, so a member
                following this would reach a page that could not tell them
                whether anything was installed. */}
            {isAdmin && (
              <NavLink
                to="/addons"
                title="Add-ons"
                onClick={releaseRail}
                className={({ isActive }) =>
                  "app-shell__lib" + (isActive ? " is-active" : "")
                }
              >
                <AddonIcon />
                <span className="app-shell__lib-name app-shell__label">
                  Add-ons
                </span>
              </NavLink>
            )}
            {/* Live TV is a place with contents, like a library — and unlike a
                library it is the same list for everybody, so it sits below them
                rather than among them. Shown to everyone: adding a channel
                source is an admin act, watching is not. */}
            <NavLink
              to="/live"
              title="Live TV"
              onClick={releaseRail}
              className={({ isActive }) =>
                "app-shell__lib" + (isActive ? " is-active" : "")
              }
            >
              <LiveIcon />
              <span className="app-shell__lib-name app-shell__label">
                Live TV
              </span>
            </NavLink>

            {/* Downloads is a place too, and unlike Add-ons it is one every
                account has: the receipts are per device, so there is nothing
                here to gate on a role. It sits after the libraries because it
                is a view *of* them rather than one of them. */}
            <NavLink
              to="/downloads"
              title="Downloads"
              onClick={releaseRail}
              className={({ isActive }) =>
                "app-shell__lib" + (isActive ? " is-active" : "")
              }
            >
              <DownloadIcon />
              <span className="app-shell__lib-name app-shell__label">
                Downloads
              </span>
            </NavLink>
          </nav>

          {/* People sits at the foot rather than among the libraries: it is
              about who is here, not about what there is to watch. */}
          <div className="app-shell__foot">
            <NavLink
              to="/people"
              title="People"
              onClick={releaseRail}
              className={({ isActive }) =>
                "app-shell__lib" + (isActive ? " is-active" : "")
              }
            >
              <PeopleIcon />
              <span className="app-shell__lib-name app-shell__label">
                People
              </span>
            </NavLink>
          </div>

          {/* The foot of the rail. `margin-top: auto` puts it against the
              bottom edge however many libraries there are, and it collapses to
              icons with everything else — the labels use the same class, so
              they appear and disappear on the same hover. */}
          <div className="app-shell__foot">
            <NavLink
              to="/settings"
              title="Settings"
              onClick={releaseRail}
              className={
                "app-shell__lib" +
                (location.pathname === "/settings" ? " is-active" : "")
              }
            >
              <SettingsIcon />
              <span className="app-shell__lib-name app-shell__label">
                Settings
              </span>
            </NavLink>

            {user && (
              <>
                {/* The name was shaped like a destination while leading
                    nowhere. It now leads to the profile page, which is the
                    thing it was shaped for. */}
                <NavLink
                  to="/profile"
                  title={`Signed in as ${user.name}`}
                  onClick={releaseRail}
                  className={({ isActive }) =>
                    "app-shell__lib app-shell__whoami" +
                    (isActive ? " is-active" : "")
                  }
                >
                  <AccountIcon />
                  <span className="app-shell__lib-name app-shell__label">
                    {user.name}
                  </span>
                </NavLink>
                <button
                  type="button"
                  className="app-shell__lib app-shell__signout"
                  onClick={() => logout.mutate()}
                  disabled={logout.isPending}
                  title="Sign out"
                >
                  <SignOutIcon />
                  <span className="app-shell__lib-name app-shell__label">
                    Sign out
                  </span>
                </button>
              </>
            )}
          </div>
        </div>
      </aside>

      <div className="app-shell__body">
        <header className="app-shell__top">
          {/* Search is a place, and it is the first place somebody goes on a
              server with more than one library — so it lives in the bar rather
              than inside one library's page, where it can only find that
              library's contents. */}
          <NavLink
            to="/search"
            className={({ isActive }) =>
              "app-shell__search" + (isActive ? " is-active" : "")
            }
            title={`Search everything (${searchKey})`}
          >
            <SearchGlyph />
            <span>Search</span>
          </NavLink>
          {reviewCount > 0 && (
            <NavLink
              to="/review"
              className={({ isActive }) =>
                "app-shell__review" + (isActive ? " is-active" : "")
              }
            >
              Review<span className="app-shell__badge">{reviewCount}</span>
            </NavLink>
          )}
          {/* Stays put. The activity tracker is the one thing here that is
              genuinely live, and moving it would cost the glance that finds it
              while something is scanning. */}
          <ActivityPanel />
        </header>
        <UpdateBanner />
      <RestartBanner />
      <KeyHelp />
        <main className="app-shell__main">{children}</main>
      </div>
    </div>
  );
}

/*
 * "An update is ready" is not an activity log entry.
 *
 * It lived only in the activity indicator, which is a list of things the server
 * is *doing* — a scan running, an enrichment pass working through a library.
 * A staged update is not that: it is a thing waiting on a decision, and the
 * only reason to look in an activity list for one is that you already know it
 * is there. So it gets a line at the top of the page, once, until it is either
 * installed or dismissed.
 *
 * Admin only, because nobody else can act on it: the restart endpoint is
 * admin-gated, and a banner that offers a button a member cannot press is worse
 * than saying nothing to them.
 */
function UpdateBanner() {
  const isAdmin = useIsAdmin();
  const { data: status } = useUpdateStatus(isAdmin);
  const [dismissed, setDismissed] = useState("");
  const navigate = useNavigate();

  // Bigscreen is one attribute on the document root, applied here because the
  // shell is the one component always mounted. index.html sets it before the
  // first paint; this keeps it in step once React owns the page.
  useBigscreen();
  useBigscreenShortcut();

  const staged = status?.staged ?? "";
  // A staged update whose version is already the one running is an update that
  // has been installed — the banner's own belt to the panel's braces, for the
  // case where this page never saw the install happen (a different tab, a
  // reload mid-restart) and would otherwise offer to install what is already
  // there.
  const running = status?.current ?? "";
  const alreadyOn =
    staged !== "" && running !== "" && plainVersion(staged) === plainVersion(running);
  if (!isAdmin || !staged || alreadyOn || dismissed === staged) return null;

  return (
    <div className="app-shell__banner" role="status">
      <span className="app-shell__banner-text">
        LANcast {staged} has been downloaded and verified, and is ready to
        install.
      </span>
      <button
        className="app-shell__banner-go"
        onClick={() => navigate("/settings?pane=updates")}
      >
        Install it
      </button>
      {/* Dismissal is per version: a new staged update says so again, and the
          one you dismissed does not come back to nag. */}
      <button
        className="app-shell__banner-x"
        onClick={() => setDismissed(staged)}
        aria-label="Dismiss"
      >
        ✕
      </button>
    </div>
  );
}

/*
 * "The server updated; this window did not."
 *
 * The in-app updater replaces the server and the assets it serves, and cannot
 * replace a running client. So after a release that changed the desktop shell,
 * the app on screen is the previous version — its bindings, its window
 * behaviour, its tray — and until now nothing said so. A fullscreen fix shipped
 * inside the client, the server updated itself, and the button went on doing
 * nothing because the window predated the binary on disk by twenty-six minutes.
 *
 * Everyone sees this one, not just admins: it is a fact about the app in front
 * of them and the fix is closing a window, which needs no permission.
 */
function RestartBanner() {
  const { data: health } = useHealth();
  const [desktop, setDesktop] = useState<DesktopVersion>(null);
  const [dismissed, setDismissed] = useState("");

  useEffect(() => {
    const state = (window as { lancastDesktopState?: () => Promise<DesktopVersion> })
      .lancastDesktopState;
    if (!state) return;
    state().then(setDesktop).catch(() => setDesktop(null));
  }, []);

  const stale = clientIsStale(desktop, health?.version);
  if (!stale || dismissed === health?.version) return null;

  return (
    <div className="app-shell__banner" role="status">
      <span className="app-shell__banner-text">
        The server is running LANcast {health?.version} and this window is still{" "}
        {desktop?.client_version}. Close LANcast and open it again to finish
        updating — your library and playback are unaffected.
      </span>
      <button
        className="app-shell__banner-x"
        onClick={() => setDismissed(health?.version ?? "")}
        aria-label="Dismiss"
      >
        ✕
      </button>
    </div>
  );
}
