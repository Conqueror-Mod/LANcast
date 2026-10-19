/*
 * An upload or a delete must change what the stage switcher offers (CLAUDE.md,
 * "A write that changes what a list holds must invalidate that list").
 *
 * The switcher offers MilkDrop from useMilkdropOffer, which reads the preset
 * list. So these assert the wire a person would see: no presets, no MilkDrop;
 * the first upload brings it in; the last delete takes it away. They also
 * hold the other half of the key design in hooks.ts: a preset already fetched
 * is never fetched again because the list changed, since its key is not a
 * child of the list's.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { fetchMilkdropPreset, useDeleteMilkdropPreset, useUploadMilkdropPreset } from "@/api/hooks";
import { resetMilkdropPickedForTests, useMilkdropOffer, type MilkdropOffer } from "./milkdropStage";
import { setWebGL2ForTests } from "./trails";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const summary = (id: string, name: string) => ({
  id,
  name,
  source: "milk",
  shaders: "none",
  size_bytes: 100,
  uploaded_at: 1_760_000_000,
});

let host: HTMLDivElement;
let root: Root;
let stored: ReturnType<typeof summary>[];
let detailReads: number;
let offer: MilkdropOffer;
let api: {
  upload: (name: string) => Promise<unknown>;
  remove: (id: string) => Promise<unknown>;
  qc: QueryClient;
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  stored = [];
  detailReads = 0;
  setWebGL2ForTests(true);
  resetMilkdropPickedForTests();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      const method = init?.method ?? "GET";
      const u = new URL(url, "http://x");
      if (method === "POST" && u.pathname === "/api/visualizers/milkdrop") {
        const name = (u.searchParams.get("filename") ?? "").replace(/\.milk$/, "");
        const s = summary(`id-${name}`, name);
        stored.push(s);
        return json(s, 201);
      }
      if (method === "DELETE") {
        stored = stored.filter((s) => !u.pathname.endsWith(s.id));
        return new Response(null, { status: 204 });
      }
      if (u.pathname === "/api/visualizers/milkdrop") return json({ presets: stored });
      if (u.pathname.startsWith("/api/visualizers/milkdrop/")) {
        detailReads++;
        return json({ ...summary("x", "x"), preset: {} });
      }
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  setWebGL2ForTests(undefined);
});

function Probe() {
  offer = useMilkdropOffer();
  const up = useUploadMilkdropPreset();
  const del = useDeleteMilkdropPreset();
  const qc = useQueryClient();
  api = {
    upload: (name) => up.mutateAsync({ name, data: new ArrayBuffer(4) }),
    remove: (id) => del.mutateAsync(id),
    qc,
  };
  return null;
}

async function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <Probe />
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 5; i++) await act(async () => await new Promise((r) => setTimeout(r, 0)));
}

describe("the stage switcher's MilkDrop offer", () => {
  it("is absent while no preset is stored", async () => {
    await mount();
    expect(offer.offered).toBe(false);
  });

  it("appears with the first upload, without a reload", async () => {
    await mount();
    await act(async () => void (await api.upload("Aurora.milk")));
    await settle();
    expect(offer.offered).toBe(true);
    expect(offer.ids).toEqual(["id-Aurora"]);
  });

  it("goes with the last delete", async () => {
    stored = [summary("id-a", "a")];
    await mount();
    expect(offer.offered).toBe(true);
    await act(async () => void (await api.remove("id-a")));
    await settle();
    expect(offer.offered).toBe(false);
  });

  it("is never offered without WebGL2, presets or not", async () => {
    setWebGL2ForTests(false);
    stored = [summary("id-a", "a")];
    await mount();
    expect(offer.offered).toBe(false);
  });

  it("does not refetch a preset already fetched when the list changes", async () => {
    stored = [summary("id-a", "a")];
    await mount();
    await act(async () => void (await fetchMilkdropPreset(api.qc, "id-a")));
    expect(detailReads).toBe(1);
    await act(async () => void (await api.upload("Second.milk")));
    await settle();
    await act(async () => void (await fetchMilkdropPreset(api.qc, "id-a")));
    expect(detailReads).toBe(1);
  });
});
