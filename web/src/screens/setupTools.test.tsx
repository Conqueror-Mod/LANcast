/*
 * The media-tools option on the setup form (ADR 0048).
 *
 * The decision this encodes: ticked to begin with, because the person it
 * protects is the one who reads nothing and would otherwise conclude the
 * software cannot play their library. What keeps that honest is that the fetch
 * follows a button they press, having been told what it downloads — so the
 * disclosure is not decoration, it is the consent, and these assert it is
 * actually on screen.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Setup } from "./Auth";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: Record<string, unknown> | null;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  sent = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      if (init?.body) sent = JSON.parse(String(init.body));
      return new Response(JSON.stringify({ configured: true }), {
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

function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <Setup restartRequired={false} />
      </QueryClientProvider>,
    );
  });
}

function fill() {
  const [user, pass] = [...host.querySelectorAll("input")].filter(
    (i) => i.type !== "checkbox",
  );
  act(() => {
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )!.set!.call(user, "chris");
    user.dispatchEvent(new Event("input", { bubbles: true }));
    Object.getOwnPropertyDescriptor(
      HTMLInputElement.prototype,
      "value",
    )!.set!.call(pass, "a good long password");
    pass.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit() {
  const form = host.querySelector("form")!;
  await act(async () => {
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await new Promise((r) => setTimeout(r, 0));
  });
}

describe("the setup form's media-tools option", () => {
  it("names what is downloaded, how big, from where and under what licence", () => {
    // A download somebody cannot identify is not consent (ADR 0043), and this
    // form is the only place the question is ever asked.
    render();
    const text = host.textContent ?? "";
    expect(text).toContain("160 MB");
    expect(text).toContain("ffmpeg");
    expect(text).toContain("GPL");
    expect(text).toContain("FFmpeg-Builds");
  });

  it("says what happens without it, which is the reason for ticking it", () => {
    render();
    expect(host.textContent).toContain("most of a library will not play");
  });

  it("is ticked to begin with", () => {
    render();
    const box = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    expect(box.checked).toBe(true);
  });

  it("sends the answer with the account, so the server never has to assume", () => {
    render();
    fill();
    return submit().then(() => {
      expect(sent).toMatchObject({ install_media_tools: true });
    });
  });

  it("sends a refusal when it is unticked, rather than omitting the field", () => {
    // Absent and false mean different things to the server: absent is "never
    // asked". Once the question has been put, the answer travels either way.
    render();
    const box = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    act(() => {
      box.click();
    });
    fill();
    return submit().then(() => {
      expect(sent).toMatchObject({ install_media_tools: false });
    });
  });
});
