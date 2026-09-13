/*
 * The Games settings pane.
 *
 * Written because of a real report rather than for coverage: the switch shipped
 * as the fourth option inside "This app", and the first person to go looking for
 * games in LANcast concluded the feature had no interface at all. Off by default
 * is the decision (ADR 0066); off and unfindable is a different thing, and these
 * assert the finding rather than the toggling.
 *
 * The last one is the sharper test. Every preference goes to the client in a
 * single whole-value call, so a pane that changes one of them must send the
 * other three unchanged — and a pane holding its own stale copy would quietly
 * revert whatever changed since it read.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

// What the window reports about itself. Close-to-tray and open-at-login are on
// here deliberately: they are what a careless write would switch off.
function desktopState(games = false) {
  return {
    client_version: "0.9.19",
    close_to_tray: true,
    open_at_login: true,
    devtools: false,
    games,
    owns_server: false,
    holder: "service" as const,
  };
}

function stubDesktop(games = false) {
  const set = vi.fn(async () => ({ ok: true }));
  const w = window as unknown as Record<string, unknown>;
  w.lancastDesktopState = vi.fn(async () => desktopState(games));
  w.lancastDesktopSet = set;
  return set;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  const w = window as unknown as Record<string, unknown>;
  delete w.lancastDesktopState;
  delete w.lancastDesktopSet;
});

async function render(initialEntry = "/settings") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

// A resolved binding still takes several turns to become rendered rows, and one
// of those turns is a timer rather than a microtask.
async function settle(times = 6) {
  for (let i = 0; i < times; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
}

function navLabels(): string[] {
  return [...host.querySelectorAll(".settings__navitem")].map(
    (b) => b.textContent ?? "",
  );
}

async function click(el: HTMLElement) {
  await act(async () => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await settle();
}

function navItem(label: string): HTMLButtonElement {
  const btn = [...host.querySelectorAll<HTMLButtonElement>(".settings__navitem")].find(
    (b) => b.textContent === label,
  );
  if (!btn) throw new Error(`no settings category called ${label}`);
  return btn;
}

describe("the games settings pane", () => {
  it("puts the word Games in the settings list", async () => {
    // The whole point. Somebody hunting for games should meet the word on the
    // path they are already walking.
    stubDesktop();
    await render();
    expect(navLabels()).toContain("Games");
  });

  it("offers nothing of the sort in a browser tab", async () => {
    // No bindings: the games are on the machine running the desktop app, and a
    // heading leading to an empty column is worse than no heading.
    await render();
    expect(navLabels()).not.toContain("Games");
  });

  it("shows the switch, off, when nothing has been turned on", async () => {
    stubDesktop(false);
    await render();
    await click(navItem("Games"));
    expect(host.textContent).toContain("Show my installed games");
    const box = host.querySelector<HTMLInputElement>(
      ".settings__pane input[type=checkbox]",
    );
    expect(box?.checked).toBe(false);
  });

  it("reads as on once it is on", async () => {
    stubDesktop(true);
    await render();
    await click(navItem("Games"));
    const box = host.querySelector<HTMLInputElement>(
      ".settings__pane input[type=checkbox]",
    );
    expect(box?.checked).toBe(true);
  });

  it("turns games on without switching the other preferences off", async () => {
    /*
     * The regression this pane could easily have introduced. lancastDesktopSet
     * takes all four preferences at once, so a second pane that sent its own
     * idea of the other three would silently undo them — close to tray and open
     * at login are both on here, and both must survive.
     */
    const set = stubDesktop(false);
    await render();
    await click(navItem("Games"));
    const box = host.querySelector<HTMLInputElement>(
      ".settings__pane input[type=checkbox]",
    )!;
    await click(box);
    expect(set).toHaveBeenCalledWith(true, true, false, true);
  });
});
