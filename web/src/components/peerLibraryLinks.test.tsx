/*
 * Other servers' libraries in the rail
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §5).
 *
 * §5 is a rule about **not merging**, and these are the tests of that rule:
 * theirs appear under their own name, ours are unaffected, and a server that
 * is not answering adds nothing at all.
 *
 * jsdom performs no layout, so nothing here proves the rail looks right — only
 * that the two lists stay two lists.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { PeerLibraryLinks } from "./PeerLibraryLinks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

// Invented. A real fingerprint never appears in a fixture.
const FP = "BBBB4G6XEG33AUPJU7LGSTT4N74U6D4B";

let host: HTMLDivElement;
let root: Root;
let peers: unknown[];
let peerLibraries: unknown[];
let peerStatus: number;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  peers = [{ fingerprint: FP, name: "Utopia", state: "paired", addrs: [] }];
  peerLibraries = [
    { id: 4, name: "Their Films", kind: "movie" },
    { id: 9, name: "Their Music", kind: "music" },
  ];
  peerStatus = 200;
  localStorage.clear();

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), {
          status,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/libraries")) {
        if (peerStatus !== 200) {
          return json(
            { error: { code: "peer_unreachable", message: "not answering" } },
            peerStatus,
          );
        }
        return json({ libraries: peerLibraries });
      }
      if (url.includes("/api/peers")) return json({ peers });
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function render(path = "/") {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[path]}>
          <PeerLibraryLinks onNavigate={() => {}} />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 40; i++) {
    if ((host.textContent ?? "") !== "") return;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

function text() {
  return host.textContent ?? "";
}

function links() {
  return [...host.querySelectorAll("a")];
}

/** Press the server's heading, which folds and unfolds its list. */
async function toggle() {
  const heading = host.querySelector<HTMLButtonElement>(".app-shell__server");
  if (!heading) throw new Error("no server heading");
  await act(async () => heading.click());
}

describe("other servers in the rail", () => {
  /*
   * A server folds. Every paired server used to spell out its whole library
   * list in the rail, which with a few friends is the rail. A new one starts
   * folded, the heading unfolds it, and the choice is remembered here.
   */
  it("starts folded, and its heading unfolds and folds it", async () => {
    await render();
    expect(text()).toContain("Utopia");
    expect(links().length).toBe(0);
    const heading = host.querySelector(".app-shell__server")!;
    expect(heading.getAttribute("aria-expanded")).toBe("false");

    await toggle();
    expect(links().length).toBe(2);
    expect(heading.getAttribute("aria-expanded")).toBe("true");

    await toggle();
    expect(links().length).toBe(0);
  });

  it("remembers an unfolded server on this device", async () => {
    await render();
    await toggle();
    act(() => root.unmount());
    root = createRoot(host);
    await render();
    expect(links().length).toBe(2);
  });

  // Where you are is never folded away: the gold edge needs a row to sit on.
  it("stays open while you are in one of its libraries", async () => {
    await render(`/peers/${FP}/library/4`);
    expect(links().length).toBe(2);
  });

  it("lists their libraries under their server's name", async () => {
    await render();
    await toggle();

    expect(text()).toContain("Utopia");
    expect(text()).toContain("Their Films");
    expect(text()).toContain("Their Music");
  });

  /*
   * The link carries the peer, so the screen it opens can never be confused
   * with one of ours. Item ids are not in the same namespace — their item 42
   * is a different film from ours — and a link to `/library/4` would open
   * *our* library 4.
   */
  it("links to a path that names the peer, never to a local library", async () => {
    await render();
    await toggle();

    const hrefs = links().map((a) => a.getAttribute("href") ?? "");
    expect(hrefs.length).toBe(2);
    for (const href of hrefs) {
      expect(href).toContain(`/peers/${FP}/library/`);
      expect(href).not.toMatch(/^\/library\//);
    }
  });

  /*
   * No counts on their rows. Ours carry one, and a number here would sit in
   * the same column and read as part of the same library — which is the
   * merging §5 forbids, arriving as a formatting choice.
   */
  it("shows no item count for somebody else's library", async () => {
    await render();

    expect(host.querySelector(".app-shell__lib-count")).toBeNull();
  });

  /*
   * A server that is not answering adds nothing — not a heading, not an
   * error. Another household's downtime is not this household's furniture.
   */
  it("renders nothing at all when that server is not answering", async () => {
    peerStatus = 502;
    await render();

    expect(text()).toBe("");
  });

  it("renders nothing when a paired server has shared nothing", async () => {
    peerLibraries = [];
    await render();

    expect(text()).toBe("");
  });

  it("renders nothing when there are no peers", async () => {
    peers = [];
    await render();

    expect(text()).toBe("");
  });
});
