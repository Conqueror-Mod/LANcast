/*
 * The core picker after R12-2: a console is pointed at a core by the name of
 * its .dll in the cores folder, never by a path, and the folder opens from
 * here. The window refuses a path whatever the page sends; these prove the
 * page sends what the person typed, offers the folder, and shows a refusal
 * instead of swallowing it.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RetroCoreSettings } from "./RetroCoreSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

async function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <RetroCoreSettings />
      </QueryClientProvider>,
    );
  });
  await act(async () => {});
}

function button(label: string): HTMLButtonElement {
  const b = Array.from(host.querySelectorAll("button")).find((x) => x.textContent?.trim() === label);
  if (!b) throw new Error(`no button "${label}"`);
  return b as HTMLButtonElement;
}

function type(input: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  set.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("RetroCoreSettings", () => {
  beforeEach(() => {
    host = document.createElement("div");
    document.body.appendChild(host);
    root = createRoot(host);
    window.lancastRetroOpen = vi.fn(async () => {});
    window.lancastRetroAvailable = vi.fn(async () => ({ available: true, core: "mGBA" }));
    window.lancastRetroCores = vi.fn(async () => ({}));
    window.lancastRetroSetCore = vi.fn(async () => {});
    window.lancastRetroOpenCoresFolder = vi.fn(async () => {});
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    delete window.lancastRetroOpen;
    delete window.lancastRetroAvailable;
    delete window.lancastRetroCores;
    delete window.lancastRetroSetCore;
    delete window.lancastRetroOpenCoresFolder;
  });

  it("asks for a file name, and opens the cores folder", async () => {
    await render();
    const input = host.querySelector<HTMLInputElement>('input[aria-label^="Core for"]')!;
    expect(input.placeholder).toBe("core_libretro.dll");
    expect(host.textContent).toContain("in the cores folder");
    await act(async () => button("Open the cores folder").click());
    expect(window.lancastRetroOpenCoresFolder).toHaveBeenCalledTimes(1);
  });

  it("sends the name typed, and shows the window's refusal of anything else", async () => {
    window.lancastRetroSetCore = vi.fn(async (_platform: string, name: string) => {
      if (name.includes("\\")) throw new Error("choose a core by the name of its .dll in C:\\cores");
    });
    await render();
    const input = host.querySelector<HTMLInputElement>('input[aria-label^="Core for"]')!;

    await act(async () => type(input, "mine_libretro.dll"));
    const save = input.parentElement!.querySelector("button")!;
    await act(async () => save.click());
    expect(window.lancastRetroSetCore).toHaveBeenLastCalledWith(expect.any(String), "mine_libretro.dll");

    const again = host.querySelector<HTMLInputElement>('input[aria-label^="Core for"]')!;
    await act(async () => type(again, "\\\\host\\share\\evil.dll"));
    await act(async () => again.parentElement!.querySelector("button")!.click());
    expect(host.textContent).toContain("choose a core by the name of its .dll");
  });
});
