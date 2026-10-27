/*
 * The pre-roll folder box (ADR 0099): it saves on Enter as well as on leaving
 * it, and says so once the server has taken it. It used to save silently on
 * blur only, and was reported as a box that did not save.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { PrerollSection } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let dir: string;
let clips: number;
let puts: unknown[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  dir = "";
  clips = 0;
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/api/settings") && init?.method === "PUT") {
        const body = JSON.parse(init.body as string);
        puts.push(body);
        dir = body.preroll_dir;
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/api/settings")) {
        return new Response(JSON.stringify({ preroll_dir: dir, preroll_clips: dir ? clips : 0 }), {
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("{}", { headers: { "Content-Type": "application/json" } });
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <PrerollSection />
      </QueryClientProvider>,
    );
  });
  await settle();
  return host.querySelector('input[aria-label="Pre-roll folder"]') as HTMLInputElement;
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    input.focus();
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

describe("the pre-roll folder box", () => {
  it("saves on Enter and says so", async () => {
    const input = await mount();
    expect(host.querySelector('[role="status"]')).toBeNull();
    type(input, "  D:\\Clips  ");
    act(() => {
      input.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    });
    await settle();
    expect(puts).toEqual([{ preroll_dir: "D:\\Clips" }]);
    expect(host.querySelector('[role="status"]')?.textContent).toBe("Saved.");
    // Typing again takes the note away until the next save.
    type(input, "D:\\Other");
    expect(host.querySelector('[role="status"]')).toBeNull();
  });

  // A folder that is set and holds no video plays nothing; saying "saved" and
  // no more let that pass for working when it was tried for real.
  it("says how many clips the folder holds, and warns when it holds none", async () => {
    dir = "D:\\Clips";
    clips = 3;
    await mount();
    expect(host.textContent).toContain("3 clips in this folder; one plays at random.");
    act(() => root.unmount());
    root = createRoot(host);
    clips = 0;
    await mount();
    expect(host.querySelector(".set-error")?.textContent).toMatch(/no videos in it, so no clip plays/);
  });

  it("says pre-roll is off when the box is emptied", async () => {
    dir = "D:\\Clips";
    const input = await mount();
    expect(input.value).toBe("D:\\Clips");
    type(input, "");
    act(() => input.blur());
    await settle();
    expect(puts).toEqual([{ preroll_dir: "" }]);
    expect(host.querySelector('[role="status"]')?.textContent).toMatch(/Pre-roll is off/);
  });

  it("does not save when nothing changed", async () => {
    dir = "D:\\Clips";
    const input = await mount();
    act(() => {
      input.focus();
      input.blur();
    });
    await settle();
    expect(puts).toEqual([]);
    expect(host.querySelector('[role="status"]')).toBeNull();
  });
});
