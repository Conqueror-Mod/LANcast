/*
 * Your account › Reading & listening (settings-3-plan step 4): each control
 * shows the account's value, or the default where nothing is chosen, and
 * saves to the account.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReadingPane } from "./reading";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let state: Record<string, unknown>;
let puts: unknown[];

beforeEach(() => {
  puts = [];
  state = { account: true, book_speed: null, book_sleep: null, comic_layout: null, comic_fit: null };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        puts.push(body);
        state = { ...state, ...body };
      }
      return new Response(JSON.stringify(state), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
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

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <ReadingPane />
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const select = (label: string) => host.querySelector<HTMLSelectElement>(`[aria-label="${label}"]`)!;

async function choose(label: string, value: string) {
  await act(async () => {
    const el = select(label);
    el.value = value;
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}

describe("Your account › Reading & listening", () => {
  it("shows the defaults where nothing is chosen", async () => {
    await render();
    expect(select("Audiobook speed").value).toBe("1");
    expect(select("Sleep timer").value).toBe("off");
    expect(select("Comic pages").value).toBe("single");
    expect(select("Comic fit").value).toBe("page");
  });

  it("saves each choice to the account, one field at a time", async () => {
    await render();
    await choose("Audiobook speed", "1.5");
    await choose("Sleep timer", "chapter");
    await choose("Comic pages", "spread");
    await choose("Comic fit", "width");
    expect(puts).toEqual([
      { book_speed: 1.5 },
      { book_sleep: "chapter" },
      { comic_layout: "spread" },
      { comic_fit: "width" },
    ]);
    expect(select("Audiobook speed").value).toBe("1.5");
    expect(select("Comic fit").value).toBe("width");
  });

  it("offers nothing to change on a server with no accounts", async () => {
    state = { ...state, account: false };
    await render();
    expect(select("Audiobook speed").disabled).toBe(true);
  });
});
