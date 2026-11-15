/*
 * A container's page lists every child, not the first hundred (R13-1).
 *
 * The server defaults `limit` to 100 and caps it at 500. useChildren asked
 * once, without a limit, and threw `total` away, so a gallery of 250 photos
 * showed 100 and counted "100 photos", and a season's Play all queued the first
 * 100 episodes. The fake server here applies the same rule, so the hook passes
 * only if it pages to `total`.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useChildren } from "./hooks";
import type { Item } from "./types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const TOTAL = 1234;

let host: HTMLDivElement;
let root: Root;
let seen: Item[] | undefined;
let requests: string[];

function Probe({ sort }: { sort?: string }) {
  seen = useChildren(9, true, sort).data;
  return null;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  seen = undefined;
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), "http://x");
      requests.push(url.pathname + url.search);
      // The server's rule (internal/store/store.go): no limit, or one past
      // 500, means 100.
      let limit = Number(url.searchParams.get("limit") ?? 0);
      if (limit <= 0 || limit > 500) limit = 100;
      const offset = Number(url.searchParams.get("offset") ?? 0);
      const items = [];
      for (let i = offset; i < Math.min(offset + limit, TOTAL); i++) {
        items.push({ id: 1000 + i, kind: "photo", title: `p${i}` });
      }
      return new Response(JSON.stringify({ items, total: TOTAL }), {
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
});

async function render(sort?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <Probe sort={sort} />
      </QueryClientProvider>,
    );
  });
  await vi.waitFor(() => expect(seen).toBeDefined());
}

describe("useChildren", () => {
  it("returns every child of a container larger than one page", async () => {
    await render();
    expect(seen).toHaveLength(TOTAL);
    expect(seen?.[0].id).toBe(1000);
    expect(seen?.[TOTAL - 1].id).toBe(1000 + TOTAL - 1);
    // Three pages of 500, not thirteen of 100.
    expect(requests).toHaveLength(3);
  });

  it("keeps the sort on every page", async () => {
    await render("track");
    expect(seen).toHaveLength(TOTAL);
    for (const r of requests) expect(r).toContain("sort=track");
  });
});
