import { NavLink, useLocation } from "react-router-dom";
import { useLibraries } from "@/api/hooks";
import type { ReactNode } from "react";
import "./AppShell.css";

// The top nav. Library names go straight to the full grid — hubs are a
// convenience, never a gate (the deliberate fix for the main Plex complaint).
export function AppShell({ children }: { children: ReactNode }) {
  const { data: libraries } = useLibraries();
  const location = useLocation();

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
        <NavLink
          to="/settings"
          className={
            "app-shell__settings" +
            (location.pathname === "/settings" ? " is-active" : "")
          }
        >
          Settings
        </NavLink>
      </header>
      <main className="app-shell__main">{children}</main>
    </div>
  );
}
