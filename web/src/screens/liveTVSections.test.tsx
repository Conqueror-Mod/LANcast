/*
 * Organising a large channel list (ADR 0039, steps 1 and 2).
 *
 * A real server carries 1,862 channels from one provider with a second playlist
 * beside it. Before this, both were merged onto one page with no way to ask for
 * one and not the other, and about sixty group chips wrapped to five rows
 * before a single channel was visible.
 *
 * jsdom performs no layout, so nothing here can say the page *fits*. What it
 * proves is the wiring: which channels are offered under which heading, and
 * what each control changes.
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

const ch = (id: number, source_id: number, source_name: string, name: string, group: string | null) => ({
  id,
  source_id,
  source_name,
  name,
  logo_url: null,
  group,
  position: id,
  tvg_id: null,
});

let channels: ReturnType<typeof ch>[];
let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  channels = [
    ch(1, 1, "Provider", "BBC One", "UK"),
    ch(2, 1, "Provider", "ITV", "UK"),
    ch(3, 1, "Provider", "Sky Sports", "Sports"),
    ch(4, 2, "Tuner", "Local News", "News"),
    ch(5, 2, "Tuner", "Tuner Sports", "Sports"),
  ];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (String(url).includes("/guide")) return json({ at: 0, channels: {} });
      if (String(url).includes("/api/channels")) return json({ channels });
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LiveTV />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
}

const names = () =>
  [...host.querySelectorAll(".livetv__channel .livetv__name")].map((e) => e.textContent);
const heads = () => [...host.querySelectorAll<HTMLButtonElement>(".livetv__sectionhead")];
const headNamed = (group: string, source?: string) =>
  heads().find(
    (h) =>
      h.querySelector(".livetv__sectionname")?.textContent === group &&
      (source === undefined || h.querySelector(".livetv__sectionsource")?.textContent === source),
  );
const sourceButtons = () =>
  [...host.querySelectorAll<HTMLButtonElement>(".livetv__sources button")];
const click = async (el: HTMLElement | undefined) => {
  expect(el, "the control is not on the page").toBeTruthy();
  await act(async () => el!.click());
};

describe("the playlist selector", () => {
  it("offers each playlist by name, with its count, once there are two", async () => {
    await render();
    const labels = sourceButtons().map((b) => b.textContent);
    expect(labels).toEqual(["All playlists", "Provider3", "Tuner2"]);
    expect(sourceButtons()[0].getAttribute("aria-pressed")).toBe("true");
  });

  // A selector offering one choice implies others exist.
  it("is absent with a single playlist", async () => {
    channels = channels.filter((c) => c.source_id === 1);
    await render();
    expect(host.querySelector(".livetv__sources")).toBeNull();
  });

  it("shows only the chosen playlist's groups and channels", async () => {
    await render();
    await click(sourceButtons().find((b) => b.textContent?.startsWith("Tuner")));
    expect(heads().map((h) => h.querySelector(".livetv__sectionname")?.textContent)).toEqual([
      "News",
      "Sports",
    ]);
    // Its first section opens, as the page's did.
    expect(names()).toEqual(["Local News"]);
    expect(host.querySelector(".browse__count")?.textContent).toBe("2");
  });
});

describe("groups that open rather than filter", () => {
  it("opens the first group and leaves the rest as headings with counts", async () => {
    await render();
    expect(heads()).toHaveLength(4);
    expect(heads()[0].getAttribute("aria-expanded")).toBe("true");
    expect(heads().slice(1).every((h) => h.getAttribute("aria-expanded") === "false")).toBe(true);
    expect(names()).toEqual(["BBC One", "ITV"]);
    expect(headNamed("UK")?.querySelector(".livetv__sectioncount")?.textContent).toBe("2");
  });

  it("opens and closes a group without touching the others", async () => {
    await render();
    await click(headNamed("Sports", "Provider"));
    expect(names()).toEqual(["BBC One", "ITV", "Sky Sports"]);
    await click(headNamed("UK"));
    expect(names()).toEqual(["Sky Sports"]);
  });

  // Two providers both carrying "Sports" are two lists of channels.
  it("keeps the same group name from two playlists apart, and says which is which", async () => {
    await render();
    expect(headNamed("Sports", "Provider")).toBeTruthy();
    expect(headNamed("Sports", "Tuner")).toBeTruthy();
    await click(headNamed("Sports", "Tuner"));
    expect(names()).toContain("Tuner Sports");
    expect(names()).not.toContain("Sky Sports");
  });

  it("names a channel with no group under Ungrouped", async () => {
    channels = [ch(9, 1, "Provider", "Loose", null)];
    await render();
    expect(heads().map((h) => h.querySelector(".livetv__sectionname")?.textContent)).toEqual([
      "Ungrouped",
    ]);
  });
});

describe("search", () => {
  // Somebody typing a name wants the channel, not the closed section it is in.
  it("answers flat across every group, including closed ones", async () => {
    await render();
    const input = host.querySelector<HTMLInputElement>(".livetv__search")!;
    await act(async () => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      set.call(input, "sports");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(heads()).toHaveLength(0);
    expect(names()).toEqual(["Sky Sports", "Tuner Sports"]);
  });
});
