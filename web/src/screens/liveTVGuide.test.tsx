/*
 * The guide on the Live TV page.
 *
 * These assert the two things a guide can get wrong without looking wrong. That
 * a channel *with* listings shows them, and — the one that matters — that a
 * channel *without* listings shows nothing rather than borrowing another
 * channel's. A guide that quietly attaches "BBC One" listings to "BBC One HD"
 * renders perfectly (ADR 0036); the only way to catch it is to assert the
 * absence.
 *
 * Also that a channel is asked for by id when its schedule is opened, because
 * "the schedule of whichever channel loaded first" is the shape that bug takes.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LiveTV } from "./LiveTV";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const now = 1_755_290_400;

const channels = [
  {
    id: 1,
    source_id: 1,
    name: "Channel One",
    logo_url: null,
    group: "UK",
    position: 0,
    tvg_id: "one.example",
  },
  // No tvg-id: this channel can never have listings, and must never show
  // anybody else's.
  {
    id: 2,
    source_id: 1,
    name: "Channel Two",
    logo_url: null,
    group: "UK",
    position: 1,
    tvg_id: null,
  },
];

const guide = {
  at: now,
  channels: {
    "1": {
      now: {
        id: 91,
        channel_id: 1,
        start_at: now - 1800,
        stop_at: now + 1800,
        title: "The Nine O'Clock News",
        description: "What happened.",
        category: "News",
        season: null,
        episode: null,
        icon_url: null,
      },
      next: {
        id: 92,
        channel_id: 1,
        start_at: now + 1800,
        stop_at: now + 5400,
        title: "A Film",
        description: null,
        category: null,
        season: null,
        episode: null,
        icon_url: null,
      },
    },
  },
};

let host: HTMLDivElement;
let root: Root;
let requests: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      requests.push(url);
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/guide") && url.includes("/channels/")) {
        return json({ programs: [guide.channels["1"].now] });
      }
      if (url.includes("/api/guide")) return json(guide);
      if (url.includes("/api/channels")) return json({ channels });
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LiveTV />
      </QueryClientProvider>,
    );
  });
  await flush();
}

async function flush() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

function tiles(): HTMLButtonElement[] {
  return [...host.querySelectorAll<HTMLButtonElement>(".livetv__channel")];
}

describe("live tv guide", () => {
  it("asks for the whole guide once rather than per channel", async () => {
    await render();
    const guideCalls = requests.filter((u) => u.includes("/api/guide"));
    expect(guideCalls).toHaveLength(1);
  });

  it("shows what is on, and what is after it", async () => {
    await render();
    const first = tiles()[0];
    expect(first.textContent).toContain("The Nine O'Clock News");
    expect(first.textContent).toContain("A Film");
  });

  /*
   * The assertion this file exists for.
   *
   * A channel with no tvg-id has no listings, and showing it a neighbour's is
   * the failure that renders perfectly and is only discoverable by watching the
   * channel.
   */
  it("shows no listings at all for a channel that has none", async () => {
    await render();
    const second = tiles()[1];
    expect(second.textContent).toContain("Channel Two");
    expect(second.textContent).not.toContain("The Nine O'Clock News");
    expect(second.querySelector(".livetv__on")).toBeNull();
  });

  it("searches programme titles, not only channel names", async () => {
    await render();
    const search = host.querySelector<HTMLInputElement>(".livetv__search")!;
    await act(async () => {
      // Through the native setter, not `.value =`. React tracks the previous
      // value on the element and swallows an input event whose value it thinks
      // it already knows — the assignment would land and the handler would
      // never run.
      Object.getOwnPropertyDescriptor(
        HTMLInputElement.prototype,
        "value",
      )!.set!.call(search, "nine o'clock");
      search.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await flush();

    const shown = tiles();
    expect(shown).toHaveLength(1);
    expect(shown[0].textContent).toContain("Channel One");
  });

  it("opens the schedule for the channel that was clicked", async () => {
    await render();
    await act(async () => {
      tiles()[0].click();
    });
    await flush();

    // By id — "the schedule of whichever channel loaded first" is the shape
    // this bug takes.
    expect(requests.some((u) => u.includes("/api/channels/1/guide"))).toBe(true);
    expect(host.querySelector(".livetv__schedule")).not.toBeNull();
  });

  // A channel that cannot have a guide says why, rather than showing an empty
  // list that reads as a broken feature.
  it("explains why a channel with no tvg-id has no schedule", async () => {
    await render();
    await act(async () => {
      tiles()[1].click();
    });
    await flush();

    const note = host.querySelector(".livetv__nolistings");
    expect(note?.textContent).toContain("tvg-id");
    // And it did not ask the server for a schedule it knows cannot exist.
    expect(requests.some((u) => u.includes("/api/channels/2/guide"))).toBe(
      false,
    );
  });
});
