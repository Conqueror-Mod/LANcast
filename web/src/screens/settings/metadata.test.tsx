/*
 * Metadata › Metadata language (ADR 0122): the served languages are offered,
 * a choice is saved, and the refresh it makes possible is offered — per film
 * and show library, with its count — and never started by the choice itself.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MetadataPane } from "./metadata";

// The provider keys have their own tests; this file is about the language.
vi.mock("@/components/ProviderKeys", () => ({ ProviderKeys: () => null }));

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let settings: Record<string, unknown>;
let calls: { method: string; url: string; body?: unknown }[];

const libraries = [
  { id: 1, name: "Films", kind: "movie", path: "X:/films" },
  { id: 2, name: "Shows", kind: "show", path: "X:/shows" },
  { id: 3, name: "Music", kind: "music", path: "X:/music" },
];

function json(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  calls = [];
  settings = {
    rate_per_sec: 5,
    auto_enrich: true,
    write_nfo: false,
    match_music: true,
    metadata_language: "en-US",
    metadata_languages: [
      { code: "en-US", name: "English (US)", native: "English (US)" },
      { code: "de-DE", name: "German", native: "Deutsch" },
    ],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : undefined;
      calls.push({ method, url, body });
      if (url.startsWith("/api/settings")) {
        if (method === "PUT") settings = { ...settings, ...body };
        return json(settings);
      }
      if (url === "/api/libraries") return json(libraries);
      const m = url.match(/^\/api\/libraries\/(\d+)\/refresh\?scope=language$/);
      if (m) {
        const count = m[1] === "1" ? 412 : 9;
        return json(method === "POST" ? { queued: count, scope: "language" } : { count, scope: "language" });
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MetadataPane />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const languageSelect = () =>
  host.querySelector<HTMLSelectElement>('[aria-label="Metadata language"]')!;

async function chooseLanguage(code: string) {
  await act(async () => {
    const el = languageSelect();
    el.value = code;
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}

const refreshPosts = () =>
  calls.filter((c) => c.method === "POST" && c.url.includes("/refresh"));

describe("Metadata › Metadata language", () => {
  it("offers the served languages and shows the default", async () => {
    await render();
    const options = [...languageSelect().options].map((o) => o.value);
    expect(options).toEqual(["en-US", "de-DE"]);
    expect(languageSelect().value).toBe("en-US");
    expect(host.textContent).toContain("German — Deutsch");
  });

  it("saves a choice and offers the refresh without starting it", async () => {
    await render();
    expect(host.querySelector('[aria-label="Refresh in the new language"]')).toBeNull();

    await chooseLanguage("de-DE");

    expect(calls.some((c) => c.method === "PUT" && (c.body as { metadata_language?: string })?.metadata_language === "de-DE")).toBe(true);
    const offer = host.querySelector('[aria-label="Refresh in the new language"]');
    expect(offer).not.toBeNull();
    expect(offer!.textContent).toContain("German");
    // Film and show libraries, each with its own count; music is not TMDB's.
    expect(offer!.textContent).toContain("Films");
    expect(offer!.textContent).toContain("412 identified titles");
    expect(offer!.textContent).toContain("Shows");
    expect(offer!.textContent).not.toContain("Music");
    expect(refreshPosts()).toHaveLength(0);
  });

  it("refreshes one library when asked, and only that one", async () => {
    await render();
    await chooseLanguage("de-DE");
    const button = [...host.querySelectorAll("button")].find((b) => b.textContent === "Refresh 412")!;
    await act(async () => button.click());
    await settle();

    expect(refreshPosts().map((c) => c.url)).toEqual(["/api/libraries/1/refresh?scope=language"]);
    expect(host.textContent).toContain("Fetching 412 titles again in the background.");
  });

  it("puts the offer away on Not now, asking nothing of the server", async () => {
    await render();
    await chooseLanguage("de-DE");
    const notNow = [...host.querySelectorAll("button")].find((b) => b.textContent === "Not now")!;
    await act(async () => notNow.click());
    await settle();

    expect(host.querySelector('[aria-label="Refresh in the new language"]')).toBeNull();
    expect(refreshPosts()).toHaveLength(0);
  });
});
