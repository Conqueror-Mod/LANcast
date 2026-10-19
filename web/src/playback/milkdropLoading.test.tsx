/*
 * What the MilkDrop stage shows while it loads (MilkdropCanvas.tsx).
 *
 * On a phone over cellular, v0.9.92's stage sat blank while the 233 KB engine
 * chunk downloaded, then blank again while the preset was fetched and
 * compiled: nothing said whether it was coming or broken. The chunk is held
 * back here with a mock, which is the slow network without the network.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { resetMilkdropPickedForTests } from "./milkdropStage";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let chunk: ReturnType<typeof deferred<unknown>>;
vi.mock("./loadButterchurn", () => ({ loadButterchurn: () => chunk.promise }));

import { forgetButterchurnForTests, MilkdropCanvas, presetNeighbours, SLOW_SWITCH_MS } from "./MilkdropCanvas";

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;
let presetLoads: ReturnType<typeof deferred<void>>[];
let fetched: string[];
const ids = ["a", "b", "c", "d"];

// A fresh AudioContext stand-in per test: the engine is cached per context.
let analyser: { current: () => AnalyserNode | undefined };

function engine() {
  return {
    createVisualizer: () => ({
      loadPreset: () => {
        const d = deferred<void>();
        presetLoads.push(d);
        return d.promise;
      },
      setRendererSize: () => {},
      render: () => {},
    }),
  };
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  chunk = deferred();
  presetLoads = [];
  fetched = [];
  forgetButterchurnForTests();
  resetMilkdropPickedForTests();
  const node = { context: {} } as unknown as AnalyserNode;
  analyser = { current: () => node };
  vi.spyOn(console, "warn").mockImplementation(() => {});
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const id = decodeURIComponent(new URL(url, "http://x").pathname.split("/").pop() ?? "");
      fetched.push(id);
      return new Response(JSON.stringify({ id, name: id, preset: {} }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function render(preset: string) {
  // Not running, so no frame loop and no WebGL: what is under test is what
  // the stage says, which does not wait for the music to play.
  return act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <MilkdropCanvas preset={preset} ids={ids} running={false} calm={false} analyser={analyser} />
      </QueryClientProvider>,
    );
  });
}

async function settle() {
  for (let i = 0; i < 5; i++) await act(async () => await new Promise((r) => setTimeout(r, 0)));
}

const note = () => host.querySelector(".playback__viz-note")?.textContent ?? null;

describe("the MilkDrop stage while it loads", () => {
  it("says it is loading while the engine downloads, and stops once a preset draws", async () => {
    await render("a");
    expect(note()).toBe("Loading MilkDrop…");

    await act(async () => chunk.resolve(engine()));
    await settle();
    // The engine is here, the preset is still compiling: still loading.
    expect(note()).toBe("Loading MilkDrop…");

    await act(async () => presetLoads[0].resolve());
    await settle();
    expect(note()).toBeNull();
  });

  it("says so in words when the engine will not load, rather than sitting blank", async () => {
    await render("a");
    await act(async () => chunk.reject(new Error("network")));
    await settle();
    expect(note()).toBe("MilkDrop could not load");
  });

  it("tries the download again the next time MilkDrop is shown", async () => {
    await render("a");
    await act(async () => chunk.reject(new Error("network")));
    await settle();
    act(() => root.unmount());
    root = createRoot(host);

    chunk = deferred();
    await render("a");
    expect(note()).toBe("Loading MilkDrop…");
    await act(async () => chunk.resolve(engine()));
    await settle();
    await act(async () => presetLoads[0].resolve());
    await settle();
    expect(note()).toBeNull();
  });

  it("keeps quiet through a quick switch, and speaks when one is slow", async () => {
    await render("a");
    await act(async () => chunk.resolve(engine()));
    await settle();
    await act(async () => presetLoads[0].resolve());
    await settle();

    await render("b");
    await settle();
    // The old preset is still drawing: no words yet.
    expect(note()).toBeNull();
    await act(async () => await new Promise((r) => setTimeout(r, SLOW_SWITCH_MS + 50)));
    expect(note()).toBe("Loading MilkDrop…");

    await act(async () => presetLoads[1].resolve());
    await settle();
    expect(note()).toBeNull();
  });

  it("fetches the presets either side ahead, once one is drawing", async () => {
    await render("b");
    await act(async () => chunk.resolve(engine()));
    await settle();
    expect(fetched).toEqual(["b"]);

    await act(async () => presetLoads[0].resolve());
    await settle();
    expect(fetched.sort()).toEqual(["a", "b", "c"]);

    // Stepping to one fetched ahead goes straight to the compile.
    await render("c");
    await settle();
    expect(fetched.filter((id) => id === "c")).toHaveLength(1);
    expect(presetLoads).toHaveLength(2);
  });

  it("makes one engine, however many times the stage mounts on one context", async () => {
    const made = vi.fn(engine().createVisualizer);
    await render("a");
    await act(async () => chunk.resolve({ createVisualizer: made }));
    await settle();
    act(() => root.unmount());
    root = createRoot(host);
    await render("a");
    await settle();
    expect(made).toHaveBeenCalledTimes(1);
  });
});

describe("presetNeighbours", () => {
  it("is the next and the previous, wrapping round", () => {
    expect(presetNeighbours("a", ["a", "b", "c"])).toEqual(["b", "c"]);
    expect(presetNeighbours("c", ["a", "b", "c"])).toEqual(["a", "b"]);
  });

  it("is one preset when there are two, and none when there is one or it is unknown", () => {
    expect(presetNeighbours("a", ["a", "b"])).toEqual(["b"]);
    expect(presetNeighbours("a", ["a"])).toEqual([]);
    expect(presetNeighbours("z", ["a", "b"])).toEqual([]);
  });
});
