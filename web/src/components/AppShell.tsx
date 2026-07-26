import { NavLink, useLocation } from "react-router-dom";
import { useLibraries, useReview, useCurrentUser, useLogout } from "@/api/hooks";
import type { ReactNode } from "react";
import "./AppShell.css";

// The top nav. Library names go straight to the full grid — hubs are a
// convenience, never a gate (the deliberate fix for the main Plex complaint).
export function AppShell({ children }: { children: ReactNode }) {
  const { data: libraries } = useLibraries();
  const { data: review } = useReview();
  const user = useCurrentUser();
  const logout = useLogout();
  const location = useLocation();
  const reviewCount = review?.total ?? 0;

  return (
    <div className="app-shell">
      <header className="app-shell__nav">
        <NavLink to="/" className="app-shell__brand">
          LANCAST
        </NavLink>
        <nav className="app-shell__libs">
          {libraries?.map((lib) => (
            <NavLink
              key={lib.id}
              to={`/library/${lib.id}`}
              className={({ isActive }) =>
                "app-shell__lib" + (isActive ? " is-active" : "")
              }
            >
              {lib.name}
            </NavLink>
          ))}
        </nav>
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
            <span className="app-shell__user" title={`Signed in as ${user.name}`}>
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
  );
}
