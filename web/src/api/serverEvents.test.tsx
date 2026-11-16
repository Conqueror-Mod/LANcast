import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  ServerEvents,
  topicKeys,
  invalidateTopics,
  onTopic,
  workFinishedKeys,
} from "./serverEvents";

/*
 * The client half of ADR 0079: a topic on the stream makes the right queries
 * stale, and a reconnect refetches everything on screen.
 *
 * jsdom has no EventSource, so a fake stands in. What this proves is wiring:
 * which keys each topic reaches, and when. It cannot prove a real stream
 * arrives; the Go tests do that against the real handler.
 */

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

class FakeEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  static all: FakeEventSource[] = [];
  readyState = FakeEventSource.CONNECTING;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  constructor(public url: string) {
    FakeEventSource.all.push(this);
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  close() {
    this.closed = true;
    this.readyState = FakeEventSource.CLOSED;
  }
  open() {
    this.readyState = FakeEventSource.OPEN;
    this.onopen?.();
  }
  emit(type: string, data: unknown) {
    for (const fn of this.listeners.get(type) ?? [])
      fn(new MessageEvent(type, { data: JSON.stringify(data) }));
  }
  fail(closed: boolean) {
    this.readyState = closed ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING;
    this.onerror?.();
  }
}

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;
let spy: { mock: { calls: unknown[][] } };

function invalidated(): unknown[] {
  return spy.mock.calls.map((c) => c[0]);
}

async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <ServerEvents />
      </QueryClientProvider>,
    );
  });
  return FakeEventSource.all[FakeEventSource.all.length - 1];
}

beforeEach(() => {
  FakeEventSource.all = [];
  vi.stubGlobal("EventSource", FakeEventSource);
  qc = new QueryClient();
  spy = vi.spyOn(qc, "invalidateQueries");
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("the event stream", () => {
  it("opens one stream at /api/events", async () => {
    const es = await mount();
    expect(FakeEventSource.all).toHaveLength(1);
    expect(es.url).toBe("/api/events");
  });

  /*
   * The first evening's fault: Settings read ["peers"] and nothing ever told
   * it the pairing had become mutual.
   */
  it("makes the peers list stale when peers change", async () => {
    const es = await mount();
    es.open();
    act(() => es.emit("changed", { topics: ["peers"] }));
    expect(invalidated()).toContainEqual({ queryKey: ["peers"] });
    expect(invalidated()).toContainEqual({ queryKey: ["peer-presence"] });
  });

  it("makes People stale when presence changes", async () => {
    const es = await mount();
    es.open();
    act(() => es.emit("changed", { topics: ["presence"] }));
    expect(invalidated()).toEqual([{ queryKey: ["peer-presence"] }]);
  });

  /*
   * The prefix rule from CLAUDE.md, checked against the table: the browse
   * grid's key is ["items", "infinite", ...], and only a prefix of it reaches
   * it.
   */
  it("reaches the browse grid when items change", async () => {
    qc.setQueryData(["items", "infinite", "library=1"], { pages: [] });
    const es = await mount();
    es.open();
    act(() => es.emit("changed", { topics: ["items"] }));
    expect(qc.getQueryState(["items", "infinite", "library=1"])?.isInvalidated).toBe(true);
  });

  it("ignores topics it does not know", async () => {
    const es = await mount();
    es.open();
    act(() => es.emit("changed", { topics: ["something-new"] }));
    expect(invalidated()).toEqual([]);
  });

  it("invalidates each key once when topics overlap", () => {
    invalidateTopics(qc, ["libraries", "items"]);
    const keys = invalidated().map((c) => JSON.stringify(c));
    expect(new Set(keys).size).toBe(keys.length);
  });

  // The first open follows a fresh fetch; refetching then would be waste.
  it("does not refetch everything on the first open", async () => {
    const es = await mount();
    es.open();
    expect(invalidated()).toEqual([]);
  });

  /*
   * Events sent while the stream was down are gone, so a reconnect refetches
   * everything on screen (ADR 0079 §4).
   */
  it("refetches everything on screen when it reconnects", async () => {
    const es = await mount();
    es.open();
    es.fail(false); // dropped; the browser retries by itself
    es.open();
    expect(invalidated()).toEqual([undefined]);
  });

  /*
   * R13-7. A screen that is cached but not mounted, still inside its
   * staleTime, would otherwise be served its pre-gap answer when next opened.
   */
  it("marks cached screens stale when it reconnects, not just mounted ones", async () => {
    qc.setQueryData(["memories", 12], []);
    const es = await mount();
    es.open();
    es.fail(false);
    es.open();
    expect(qc.getQueryState(["memories", 12])?.isInvalidated).toBe(true);
  });

  /*
   * A 401 or a 503 mid-restart makes EventSource close for good. Without a
   * reopen the window would be deaf for the rest of its life.
   */
  it("opens a new stream after the browser gives up", async () => {
    vi.useFakeTimers();
    const es = await mount();
    es.fail(true);
    expect(es.closed).toBe(true);
    await act(async () => {
      vi.advanceTimersByTime(15_000);
    });
    expect(FakeEventSource.all).toHaveLength(2);
  });

  it("closes the stream when unmounted", async () => {
    const es = await mount();
    act(() => root.unmount());
    expect(es.closed).toBe(true);
    root = createRoot(host);
  });

  /*
   * A detail page, a season list and the filter counts change with a scan
   * too. The activity poll used to be the only thing that refreshed them, and
   * only when it happened to see the job end.
   */
  it("reaches detail pages, children and facets when items change", () => {
    invalidateTopics(qc, ["items"]);
    for (const key of [["item"], ["children"], ["facets"], ["review"]])
      expect(invalidated()).toContainEqual({ queryKey: key });
  });

  /*
   * R13-6. Each of these is a top-level key of its own, so no prefix in the
   * old list reached it, and a scan that marked a film missing left it listed
   * on that screen.
   */
  it("reaches every list a scan can change when items change", () => {
    const keys = [
      ["collection-members", 4],
      ["item-collections", 9],
      ["playlist-entries", 3],
      ["search", "alien"],
      ["timeline", 2],
      ["duplicates", 2, "near"],
      ["memories", 12],
      ["recent-photos"],
    ];
    for (const key of keys) qc.setQueryData(key, []);
    invalidateTopics(qc, ["items"]);
    for (const key of keys)
      expect(qc.getQueryState(key)?.isInvalidated, JSON.stringify(key)).toBe(true);
  });

  // The activity poll's finished-work edge refreshes this same list.
  it("shares one list with the activity poll", () => {
    expect(topicKeys.items).toEqual(workFinishedKeys.map((k) => [k]));
  });

  it("every topic the server publishes has a row", () => {
    for (const topic of ["peers", "presence", "libraries", "items", "backups", "players"])
      expect(topicKeys[topic], topic).toBeDefined();
  });

  /*
   * ADR 0117. `players` is a list a remote shows; `remote` is a command for
   * this window to collect, heard by a listener rather than a query.
   */
  it("refreshes your devices when a player changes", async () => {
    const es = await mount();
    es.open();
    act(() => es.emit("changed", { topics: ["players"] }));
    expect(invalidated()).toEqual([{ queryKey: ["remote-players"] }]);
  });

  it("tells a listener that a command is waiting, and stops when it unsubscribes", async () => {
    const es = await mount();
    es.open();
    const heard = vi.fn();
    const off = onTopic("remote", heard);
    act(() => es.emit("changed", { topics: ["remote", "items"] }));
    expect(heard).toHaveBeenCalledTimes(1);
    off();
    act(() => es.emit("changed", { topics: ["remote"] }));
    expect(heard).toHaveBeenCalledTimes(1);
  });
});
