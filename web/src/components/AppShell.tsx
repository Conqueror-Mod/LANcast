import { NavLink, useLocation, useNavigate } from "react-router-dom";
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
import { plainVersion } from "./UpdateSettings";
import { clientIsStale, type DesktopVersion } from "@/lib/clientVersion";
import { useScrollRestoration } from "@/lib/useScrollRestoration";
import {
  LibraryIcon,
  HomeIcon,
  SettingsIcon,
  AddonIcon,
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

                Admin only: installing and granting a plugin is an admin act,
                and a rail entry leading to a panel of refusals is worse than
                no entry. */}
            {isAdmin && (
              <NavLink
                to="/settings?pane=addons"
                title="Add-ons"
                onClick={releaseRail}
                className={
                  "app-shell__lib" +
                  (location.search.includes("pane=addons") ? " is-active" : "")
                }
              >
                <AddonIcon />
                <span className="app-shell__lib-name app-shell__label">
                  Add-ons
                </span>
              </NavLink>
            )}
          </nav>

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
                {/* The name is a destination in waiting — the profile page —
                    so it is shaped like one rather than like a label. */}
                <div className="app-shell__lib app-shell__whoami" title={`Signed in as ${user.name}`}>
                  <AccountIcon />
                  <span className="app-shell__lib-name app-shell__label">
                    {user.name}
                  </span>
                </div>
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
