/*
 * Report an issue (ADR 0109): what the form sends, who may attach the log,
 * and what a build that cannot send offers instead.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { ReportIssue } from "./ReportIssue";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let info: { can_send: boolean; can_attach_log: boolean };
let posted: unknown[];
let asked: string[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  posted = [];
  asked = [];
  info = { can_send: true, can_attach_log: true };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      asked.push(url);
      const json = (b: unknown) => new Response(JSON.stringify(b), { headers: { "Content-Type": "application/json" } });
      if (url === "/api/report" && init?.method === "POST") {
        posted.push(JSON.parse(String(init.body)));
        return json({ sent: true, log_included: false });
      }
      if (url === "/api/report") return json(info);
      if (url.startsWith("/api/logs")) return json({ lines: ["msg=started", "msg=comic reader blank"] });
      return json({});
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

async function mount(onClose = () => {}) {
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <FocusProvider>
          <ReportIssue version="0.9.99" onClose={onClose} />
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
}

function type(el: HTMLTextAreaElement | HTMLInputElement, value: string) {
  const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(proto, "value")!.set!.call(el, value);
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

const button = (label: string) => [...host.querySelectorAll("button")].find((b) => b.textContent === label);

describe("report an issue", () => {
  it("sends what was written, with the log only when ticked, and says it went", async () => {
    await mount();
    const send = button("Send")!;
    expect(send.disabled).toBe(true); // nothing written yet
    const [what, , steps] = host.querySelectorAll<HTMLTextAreaElement>("textarea");
    await act(async () => type(what, "The reader shows a blank page"));
    await act(async () => type(steps, "Open Night Owl 001, press End"));
    const log = host.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
    await act(async () => log.click());
    await act(async () => button("Send")!.click());
    await settle();
    expect(posted).toEqual([
      expect.objectContaining({
        what: "The reader shows a blank page",
        steps: "Open Night Owl 001, press End",
        include_log: true,
        client: "browser",
      }),
    ]);
    expect(host.textContent).toContain("Sent. Thank you");
    // No mail link, no other address: only this server was asked.
    expect(asked.every((u) => u.startsWith("/api/"))).toBe(true);
  });

  it("shows the log before it is sent, when asked", async () => {
    await mount();
    expect(host.querySelector(".report__logview")).toBeNull();
    await act(async () => button("Read it first")!.click());
    await settle();
    expect(host.querySelector(".report__logview")?.textContent).toContain("comic reader blank");
  });

  it("offers a member no log", async () => {
    info = { can_send: true, can_attach_log: false };
    await mount();
    expect(host.querySelector('input[type="checkbox"]')).toBeNull();
    expect(host.textContent).not.toContain("server log");
  });

  it("offers a copy, and no Send, in a build that cannot send", async () => {
    info = { can_send: false, can_attach_log: true };
    const writeText = vi.fn(async () => {});
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    await mount();
    expect(button("Send")).toBeUndefined();
    expect(host.textContent).toContain("can't send reports");
    await act(async () => type(host.querySelector("textarea")!, "Subtitles are late"));
    await act(async () => button("Copy report")!.click());
    await settle();
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining("Subtitles are late"));
    expect(button("Copied")).toBeDefined();
  });
});
