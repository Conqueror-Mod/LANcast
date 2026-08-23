/*
 * The People page tells four states apart, across a network.
 *
 * ADR 0045 permits one disclosure and bounds it hard, and almost every way of
 * getting this UI wrong is a *collapse*: rendering "has not shared with you" as
 * absence, an unreachable server as somebody being offline, or an idle person
 * as nothing at all. Each of those turns a choice or a fact about a machine
 * into a statement about a person, and each is invisible to a test that only
 * checks whether a title appears.
 *
 * The local list already holds itself to this rule — its own test exists
 * because the sharing toggle read its state out of a list that excludes the
 * caller and answered `undefined` for ever. This is the same discipline one
 * server further away.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { People } from "./People";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

type PeerPersonFixture = {
  id: string;
  name: string;
  granted: boolean;
  shares: boolean;
  online?: boolean;
  watching?: string;
};

const puts: { url: string; body: unknown }[] = [];

function mockServer(peers: unknown) {
  puts.length = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (init?.method === "PUT") {
        puts.push({ url, body: JSON.parse(String(init.body)) });
        return new Response(null, { status: 204 });
      }
      if (url.includes("/api/people/peers")) return json({ peers });
      if (url.includes("/api/people")) return json({ people: [] });
      return json({});
    }),
  );
}

async function render() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, refetchInterval: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <People />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // Let the peers query settle.
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function onePeer(people: PeerPersonFixture[], reachable = true) {
  return [
    {
      fingerprint: "AAAA-BBBB",
      name: "Georgia's LANcast",
      state: "paired",
      reachable,
      people,
    },
  ];
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("people on paired servers", () => {
  it("names the work somebody is watching", async () => {
    mockServer(
      onePeer([
        {
          id: "g-1",
          name: "Georgia",
          granted: false,
          shares: true,
          online: true,
          watching: "Blade Runner",
        },
      ]),
    );
    await render();

    expect(host.textContent).toContain("Georgia");
    expect(host.textContent).toContain("Watching Blade Runner");
  });

  /*
   * The failure this whole test file exists for. "Has not shared" and "is not
   * watching anything" are different statements, and rendering the first as the
   * second accuses somebody of being idle when they have simply made a choice.
   */
  it("says a choice is a choice, not an absence", async () => {
    mockServer(
      onePeer([{ id: "g-1", name: "Georgia", granted: false, shares: false }]),
    );
    await render();

    expect(host.textContent).toContain("Not sharing with you");
    expect(host.textContent).not.toContain("Offline");
  });

  /*
   * Somebody can only be known to be offline if they share. Saying "Offline"
   * about a person who has told us nothing is inventing a fact about them —
   * and it is the tempting default, because the field is simply absent.
   */
  it("does not call a non-sharer offline", async () => {
    mockServer(
      onePeer([{ id: "g-1", name: "Georgia", granted: true, shares: false }]),
    );
    await render();

    expect(host.textContent).toContain("Not sharing with you");
  });

  it("distinguishes an idle person from one watching something", async () => {
    mockServer(
      onePeer([
        {
          id: "g-1",
          name: "Georgia",
          granted: false,
          shares: true,
          online: true,
        },
      ]),
    );
    await render();

    expect(host.textContent).toContain("Online");
    expect(host.textContent).not.toContain("Watching");
  });

  it("blames the machine when the machine is the problem", async () => {
    mockServer(
      onePeer(
        [
          {
            id: "g-1",
            name: "Georgia",
            granted: false,
            shares: true,
            online: true,
            watching: "Blade Runner",
          },
        ],
        false,
      ),
    );
    await render();

    // A server that is not answering cannot be reporting anybody as watching
    // anything, and a stale title is a false statement about the present.
    expect(host.textContent).toContain("Server not answering");
    expect(host.textContent).not.toContain("Watching Blade Runner");
  });

  /*
   * The grant is *your* decision about *yourself*, so the checkbox must reflect
   * `granted` and never `shares`. Reading the wrong one produces a switch that
   * shows their choice while claiming to be yours — the exact confusion the
   * sharing-toggle bug produced on the local list.
   */
  it("shows your grant, not theirs", async () => {
    mockServer(
      onePeer([
        { id: "g-1", name: "Georgia", granted: true, shares: false },
        { id: "g-2", name: "Alex", granted: false, shares: true, online: true },
      ]),
    );
    await render();

    const boxes = Array.from(
      host.querySelectorAll<HTMLInputElement>('input[type="checkbox"]'),
    );
    expect(boxes).toHaveLength(2);
    expect(boxes[0].checked).toBe(true);
    expect(boxes[1].checked).toBe(false);
  });

  it("grants presence to a named person, by id", async () => {
    mockServer(
      onePeer([{ id: "g-1", name: "Georgia", granted: false, shares: false }]),
    );
    await render();

    const box = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    await act(async () => {
      box.click();
    });

    expect(puts).toHaveLength(1);
    // The person, not the server: a grant that named only a fingerprint would
    // be a grant to everybody on it.
    expect(puts[0].url).toContain("g-1");
    expect(puts[0].url).toContain("AAAA-BBBB");
    expect(puts[0].body).toEqual({ on: true });
  });

  // Nothing to show is not the same as a section saying there is nothing. A
  // server with no peers should not grow an empty heading.
  it("stays out of the way when there are no peers", async () => {
    mockServer([]);
    await render();
    expect(host.textContent).not.toContain("Other servers");
  });
});
