import { NavLink, useLocation } from "react-router-dom";
import { useLibraries, useReview, useCurrentUser, useLogout } from "@/api/hooks";
import type { ReactNode } from "react";
import { ActivityPanel } from "./ActivityPanel";
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
// needs review, what the server is doing, settings, and who you are signed in
// as. Those were already on the right and stay there.
//
// Library names still go straight to the full grid — hubs are a convenience,
// never a gate (the deliberate fix for the main Plex complaint).
export function AppShell({ children }: { children: ReactNode }) {
  const { data: libraries } = useLibraries();
  const { data: review } = useReview();
  const user = useCurrentUser();
  const logout = useLogout();
  const location = useLocation();
  const reviewCount = review?.total ?? 0;

  return (
    <div className="app-shell">
      <aside className="app-shell__rail">
        <NavLink to="/" className="app-shell__brand">
          LANCAST
        </NavLink>

        <nav className="app-shell__libs" aria-label="Libraries">
          {libraries && libraries.length > 0 && (
            <span className="section-label app-shell__rail-label">Libraries</span>
          )}
          {libraries?.map((lib) => (
            <NavLink
              key={lib.id}
              to={`/library/${lib.id}`}
              className={({ isActive }) =>
                "app-shell__lib" + (isActive ? " is-active" : "")
              }
            >
              <span className="app-shell__lib-name">{lib.name}</span>
              <span className="app-shell__lib-count">{lib.item_count}</span>
            </NavLink>
          ))}
        </nav>
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
          <ActivityPanel />
          <NavLink
            to="/settings"
            className={
              "app-shell__settings" +
              (location.pathname === "/settings" ? " is-active" : "")
            }
          >
            Settings
          </NavLink>
          {user && (
            <div className="app-shell__account">
              <span
                className="app-shell__user"
                title={`Signed in as ${user.name}`}
              >
                {user.name}
              </span>
              <button
                type="button"
                className="app-shell__signout"
                onClick={() => logout.mutate()}
                disabled={logout.isPending}
              >
                Sign out
              </button>
            </div>
          )}
        </header>
        <main className="app-shell__main">{children}</main>
      </div>
    </div>
  );
}
