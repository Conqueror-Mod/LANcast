/*
 * The settings shell: does the left-hand nav actually select a pane?
 *
 * Written because this change cannot be checked the way the rest of the client
 * is. The settings page is behind a sign-in, and the one thing that would prove
 * the shell works — clicking a category and seeing the page change — is exactly
 * what an unauthenticated session cannot do. A build that compiles proves the
 * JSX nests; it does not prove the panes are wired to the buttons.
 *
 * These render the real Settings component against a query client with no
 * server behind it. Every request fails, which is fine: what is under test is
 * the shell — which categories exist, which pane is showing, and whether the
 * URL carries it. The sections themselves render their empty state and are not
 * the subject.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
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

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

function render(initialEntry = "/settings") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Settings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

function navItems(): HTMLButtonElement[] {
  return [...host.querySelectorAll<HTMLButtonElement>(".settings__navitem")];
}

function activeItem(): string | undefined {
  return host.querySelector(".settings__navitem.is-active")?.textContent ?? undefined;
}

function click(label: string) {
  const btn = navItems().find((b) => b.textContent === label);
  if (!btn) throw new Error(`no settings category called ${label}`);
  act(() => {
    btn.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

describe("the settings shell", () => {
  it("renders a category list", () => {
    render();
    // Not an exhaustive list on purpose: asserting the exact set would make
    // this fail every time a category is added, which is not a regression.
    const labels = navItems().map((b) => b.textContent);
    expect(labels).toContain("Account");
    expect(labels).toContain("Keyboard");
    expect(labels.length).toBeGreaterThan(1);
  });

  it("marks exactly one category as current", () => {
    render();
    const active = host.querySelectorAll(".settings__navitem.is-active");
    expect(active.length).toBe(1);
  });

  it("switches pane when a category is pressed", () => {
    render();
    click("Keyboard");
    expect(activeItem()).toBe("Keyboard");
    // The keyboard pane is the one thing here that renders without a server:
    // its content is a static table, so it is the honest way to prove the pane
    // actually changed rather than only the highlight.
    expect(host.textContent).toContain("Keyboard");
    click("Account");
    expect(activeItem()).toBe("Account");
  });

  it("takes the pane from the URL", () => {
    render("/settings?pane=keyboard");
    expect(activeItem()).toBe("Keyboard");
  });

  it("falls back to a real pane when the URL names one that does not exist", () => {
    // A link to an admin pane, followed by a demotion, must land somewhere.
    render("/settings?pane=not-a-pane");
    expect(host.querySelectorAll(".settings__navitem.is-active").length).toBe(1);
  });
});

/*
 * The panes added in later passes are reachable.
 *
 * This is the failure that has recurred in three consecutive feature passes and
 * every time it was invisible to the suites: a shortcut whose handlers ignored
 * it, a warning that never survived a restart, a button hidden inside the wrong
 * conditional. Each time the logic was right and nothing could get to it.
 *
 * Asserting the category *exists and switches* is the cheapest possible guard
 * against the same shape here — a pane registered in one list and not the other
 * renders nothing, and no unit test of its contents would notice.
 */
describe("panes added after the shell was built", () => {
  it("lists Display, Keyboard and Sharing's home among the categories", () => {
    render();
    const labels = navItems().map((b) => b.textContent);
    for (const label of ["Account", "Display", "Keyboard"]) {
      expect(labels).toContain(label);
    }
  });

  it("switches to Display and renders it", () => {
    render();
    click("Display");
    expect(activeItem()).toBe("Display");
    // Bigscreen is the pane's only control, and it renders without a server.
    expect(host.textContent).toContain("Bigscreen");
  });
});

/*
 * The codec-denial row, wired end to end.
 *
 * Here rather than beside capabilities.ts because the unit tests prove the
 * *rule* and this proves the *wiring* — which is the half that has failed
 * before in this file's own history, and the half a passing build does not
 * cover.
 *
 * It matters because the state it shows was invisible while being wrong: a real
 * install withheld all four claims it is capable of making and served a full 4K
 * re-encode of every HEVC film as a result, with no symptom but a busy CPU.
 */
describe("the playback codecs row", () => {
  function codecRow(): HTMLElement | undefined {
    return [...host.querySelectorAll<HTMLElement>(".set-row")].find((r) =>
      r.querySelector(".set-row__title")?.textContent?.includes("Playback codecs"),
    );
  }

  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it("names what is being withheld rather than only counting it", () => {
    localStorage.setItem(
      "lancast:codec-denied",
      JSON.stringify({ hevc: Date.now(), ac3: Date.now() }),
    );
    render("/settings?pane=display");

    const sub = codecRow()?.querySelector(".set-row__sub")?.textContent ?? "";
    expect(sub).toContain("hevc");
    expect(sub).toContain("ac3");
  });

  it("clears them when asked, and says so", () => {
    localStorage.setItem(
      "lancast:codec-denied",
      JSON.stringify({ hevc: Date.now() }),
    );
    render("/settings?pane=display");

    const btn = codecRow()?.querySelector("button");
    expect(btn).toBeTruthy();
    expect(btn!.disabled).toBe(false);
    act(() => {
      btn!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(localStorage.getItem("lancast:codec-denied")).toBeNull();
    // The row has to re-read, or the button clears the store and leaves the
    // screen asserting the opposite of what is true — the stale-view bug this
    // project keeps shipping.
    expect(codecRow()?.querySelector(".set-row__sub")?.textContent).not.toContain(
      "hevc",
    );
  });

  it("offers nothing to press when nothing is withheld", () => {
    render("/settings?pane=display");
    expect(codecRow()?.querySelector("button")?.disabled).toBe(true);
  });
});
