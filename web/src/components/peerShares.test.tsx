/*
 * Choosing what a paired server may see (ADR 0071 §1, §6, §7).
 *
 * §7 makes this UI part of the decision rather than a follow-up, so these are
 * about wiring: the controls reach the routes, the limit is offered only where
 * it means something, and the cost of a limit is put in front of the host at
 * the moment they choose it.
 *
 * jsdom performs no layout, so nothing here proves any of it is on screen.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PeerShares } from "./PeerShares";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// Invented. A real fingerprint never appears in a fixture.
const FP = "BBBB4G6XEG33AUPJU7LGSTT4N74U6D4B";

const FILMS = {
  id: 1,
  name: "Films",
  kind: "movie",
  shared: false,
  ceiling: "",
  supports_ceiling: true,
  unrated: 10,
  total: 1212,
};
const MUSIC = {
  id: 2,
  name: "Music",
  kind: "music",
  shared: false,
  ceiling: "",
  supports_ceiling: false,
  unrated: 0,
  total: 0,
};

let host: HTMLDivElement;
let root: Root;
let libraries: unknown[];
let writes: { url: string; method: string; body: unknown }[];
let role: string;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  libraries = [FILMS, MUSIC];
  writes = [];
  role = "admin";

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });

      if (method !== "GET") {
        writes.push({
          url,
          method,
          body: init?.body ? JSON.parse(String(init.body)) : undefined,
        });
        return json({ ok: true });
      }
      if (url.includes("/shares")) return json({ libraries });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          user: { id: "u_1", name: "Chris", role },
        });
      }
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  /*
   * The signed-in user is seeded rather than fetched.
   *
   * The component renders nothing until it knows the role, which is correct —
   * a member must never see a flash of controls they cannot use — but it makes
   * "did it render" depend on how many microtasks the test flushes. Seeding
   * removes the race, and the role is what is under test here, not the query
   * that reads it.
   */
  qc.setQueryData(["auth-status"], {
    authenticated: true,
    user: { id: "u_1", name: "Chris", role },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <PeerShares fingerprint={FP} />
      </QueryClientProvider>,
    );
  });
  // Let the auth status and the shares settle.
  await act(async () => {
    await Promise.resolve();
  });
}

function text() {
  return host.textContent ?? "";
}

function button(label: string): HTMLButtonElement {
  const found = [...host.querySelectorAll("button")].find((b) =>
    (b.textContent ?? "").includes(label),
  );
  if (!found) throw new Error(`no button matching ${label}: ${text()}`);
  return found as HTMLButtonElement;
}

// settle lets a real fetch round trip resolve. A single microtask flush is
// not enough, and a fixed one is flaky; this waits for the condition.
async function settle(done: () => boolean, what: string) {
  for (let i = 0; i < 50; i++) {
    if (done()) return;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
  throw new Error(`never settled: ${what}
${text()}`);
}

async function open() {
  await act(async () => {
    button("Choose what they can see").click();
  });
  await settle(() => !text().includes("Loading"), "shares to load");
}

function checkboxes() {
  return [...host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
}

describe("peer shares", () => {
  it("renders nothing at all for a member", async () => {
    role = "member";
    await render();
    expect(text()).toBe("");
  });

  it("shares a library, and asks for no limit when it does", async () => {
    await render();
    await open();

    expect(text()).toContain("Films");
    await act(async () => {
      checkboxes()[0].click();
    });

    expect(writes).toHaveLength(1);
    expect(writes[0].method).toBe("PUT");
    expect(writes[0].url).toContain(`/api/peers/${FP}/shares/1`);
    // Sharing is not choosing a limit. Defaulting to one would apply a
    // restriction the host never asked for.
    expect(writes[0].body).toEqual({ ceiling: "" });
  });

  it("un-shares through DELETE rather than a PUT with a flag", async () => {
    libraries = [{ ...FILMS, shared: true }, MUSIC];
    await render();
    await open();

    await act(async () => {
      checkboxes()[0].click();
    });

    expect(writes[0].method).toBe("DELETE");
    expect(writes[0].url).toContain(`/api/peers/${FP}/shares/1`);
  });

  /*
   * ADR 0071 §6: a ceiling on a library whose contents carry no certificate is
   * a control that does nothing, and the UI must say so rather than offer it.
   * An inert switch reads as a limit that was applied.
   */
  it("offers no limit on a library that cannot carry one, and says why", async () => {
    libraries = [{ ...MUSIC, shared: true }];
    await render();
    await open();

    expect(host.querySelector("select")).toBeNull();
    expect(text()).toContain("a limit would do nothing");
  });

  it("offers the limit on a shared video library", async () => {
    libraries = [{ ...FILMS, shared: true }];
    await render();
    await open();

    const select = host.querySelector("select");
    expect(select).not.toBeNull();

    await act(async () => {
      const s = select as HTMLSelectElement;
      s.value = "PG-13";
      s.dispatchEvent(new Event("change", { bubbles: true }));
    });

    expect(writes[0].method).toBe("PUT");
    expect(writes[0].body).toEqual({ ceiling: "PG-13" });
  });

  /*
   * The mitigation ADR 0071 §6 offers for items that vanish from a friend's
   * view with no explanation: tell the host, at the moment they choose. A
   * number the host sees beats a mystery the friend does not.
   */
  it("tells the host what a limit costs, once one is set", async () => {
    libraries = [{ ...FILMS, shared: true, ceiling: "PG-13" }];
    await render();
    await open();

    expect(text()).toContain("10 of 1212 are unrated and will not be shown");
  });

  it("says nothing about unrated items when no limit is set", async () => {
    libraries = [{ ...FILMS, shared: true, ceiling: "" }];
    await render();
    await open();

    expect(text()).not.toContain("will not be shown");
  });

  // Nothing is fetched until the pane is opened: the People screen is read far
  // more often than it is changed.
  it("does not ask the server anything until it is opened", async () => {
    await render();
    const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock
      .calls;
    expect(calls.some((c) => String(c[0]).includes("/shares"))).toBe(false);

    await open();
    const after = (fetch as unknown as { mock: { calls: unknown[][] } }).mock
      .calls;
    expect(after.some((c) => String(c[0]).includes("/shares"))).toBe(true);
  });
});
