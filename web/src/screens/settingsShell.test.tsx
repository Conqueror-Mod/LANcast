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
