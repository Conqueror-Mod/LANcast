/*
 * A library's own options (ADR 0123): a film or show library chooses its
 * metadata language and is offered that library's refresh; a comics library
 * chooses its reading direction; other kinds show nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LibraryOptions } from "./LibrarySettings";
import type { Library } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: Record<string, unknown> }[];

const settings = {
  metadata_language: "en-US",
  metadata_languages: [
    { code: "en-US", name: "English (United States)", native: "English (United States)" },
    { code: "ja-JP", name: "Japanese", native: "日本語" },
  ],
};

function lib(kind: string, extra: Partial<Library> = {}): Library {
  return {
    id: 4, name: "Anime", kind, path: "X:/anime", created_at: 0, scanned_at: null,
    item_count: 0, media_count: 0, metadata_language: null, reading_direction: null, ...extra,
  } as Library;
}

beforeEach(() => {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, url, body });
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.startsWith("/api/settings")) return json(settings);
      if (url === "/api/libraries/4/refresh?scope=language") {
        return json(method === "POST" ? { queued: 37, scope: "language" } : { count: 37, scope: "language" });
      }
      return json({});
    }),
  );
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render(library: Library) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LibraryOptions library={library} />
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function choose(label: string, value: string) {
  await act(async () => {
    const el = host.querySelector<HTMLSelectElement>(`[aria-label="${label}"]`)!;
    el.value = value;
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}

const patches = () => calls.filter((c) => c.method === "PATCH");

describe("a library's own options", () => {
  it("a film library follows the server's language until it chooses one", async () => {
    await render(lib("movie"));
    const el = host.querySelector<HTMLSelectElement>('[aria-label="Library metadata language"]')!;
    expect(el.value).toBe("");
    expect(el.options[0].textContent).toBe("The server's language (English (United States))");
    expect(host.querySelector('[aria-label="Library reading direction"]')).toBeNull();
  });

  it("saves a language and offers this library's refresh without starting it", async () => {
    await render(lib("show"));
    await choose("Library metadata language", "ja-JP");
    expect(patches()).toEqual([
      { method: "PATCH", url: "/api/libraries/4", body: { metadata_language: "ja-JP" } },
    ]);
    const offer = host.querySelector('[aria-label="Refresh in the new language"]');
    expect(offer?.textContent).toContain("37 identified titles");
    expect(calls.some((c) => c.method === "POST")).toBe(false);

    const button = [...host.querySelectorAll("button")].find((b) => b.textContent === "Refresh 37")!;
    await act(async () => button.click());
    await settle();
    expect(calls.filter((c) => c.method === "POST").map((c) => c.url)).toEqual([
      "/api/libraries/4/refresh?scope=language",
    ]);
  });

  it("a comics library chooses which way it reads", async () => {
    await render(lib("comics", { reading_direction: null }));
    const el = host.querySelector<HTMLSelectElement>('[aria-label="Library reading direction"]')!;
    expect(el.value).toBe("ltr");
    expect(host.querySelector('[aria-label="Library metadata language"]')).toBeNull();
    await choose("Library reading direction", "rtl");
    expect(patches().map((c) => c.body)).toEqual([{ reading_direction: "rtl" }]);
  });

  it("a music library has neither", async () => {
    await render(lib("music"));
    expect(host.querySelector("select")).toBeNull();
  });
});
