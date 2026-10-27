/*
 * Invite a friend in Settings (ADR 0104): the link is built from the public
 * name when there is one, warns when it cannot reach a friend, is shown once,
 * and an open invite can be withdrawn.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { InviteFriend, inviteLink, reachWarning } from "./InviteFriend";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

describe("the invite link", () => {
  it("uses the public name when there is one", () => {
    expect(inviteLink("abc", "lighthouse.example.net", "http://127.0.0.1:8080")).toBe(
      "https://lighthouse.example.net/join#abc",
    );
    expect(reachWarning("lighthouse.example.net", "127.0.0.1")).toBeNull();
  });

  it("falls back to this window's address, and says who that reaches", () => {
    expect(inviteLink("abc", "", "https://192.168.1.20:8080")).toBe("https://192.168.1.20:8080/join#abc");
    expect(reachWarning("", "127.0.0.1")).toMatch(/this computer/);
    expect(reachWarning("", "192.168.1.20")).toMatch(/home network/);
  });
});

let host: HTMLDivElement;
let root: Root;
let calls: { method: string; url: string; body?: unknown }[];
let invites: unknown[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  calls = [];
  invites = [
    {
      id: "i1", note: "for Sam", hidden_libraries: [2], max_content_rating: "PG",
      created_by: "Dana", created_at: 1, expires_at: 2000000000,
    },
  ];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push({ method, url, body: init?.body ? JSON.parse(init.body as string) : undefined });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/api/libraries")) return json([{ id: 1, name: "Films" }, { id: 2, name: "Home videos" }]);
      if (url.endsWith("/api/invites") && method === "POST") {
        return json({ invite: { id: "i2" }, token: "the-secret", public_name: "lighthouse.example.net" }, 201);
      }
      if (url.endsWith("/api/invites")) return json({ invites });
      if (method === "DELETE") {
        invites = [];
        return new Response(null, { status: 204 });
      }
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

async function mount() {
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <InviteFriend />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const button = (text: string) =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === text) as HTMLButtonElement;

describe("invite a friend", () => {
  it("makes a link with the limits chosen, and shows it once", async () => {
    await mount();
    await act(async () => button("+ Invite a friend").click());
    const note = host.querySelector('input[aria-label="Who the invite is for"]') as HTMLInputElement;
    const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    act(() => {
      set.call(note, "for Georgia");
      note.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const select = host.querySelector("select") as HTMLSelectElement;
    act(() => {
      select.value = "PG-13";
      select.dispatchEvent(new Event("change", { bubbles: true }));
    });
    const homeVideos = [...host.querySelectorAll("label")]
      .find((l) => l.textContent === "Home videos")!
      .querySelector("input") as HTMLInputElement;
    await act(async () => homeVideos.click());
    await act(async () => {
      host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });
    await settle();

    const post = calls.find((c) => c.method === "POST")!;
    expect(post.body).toEqual({ note: "for Georgia", max_content_rating: "PG-13", hidden_libraries: [2] });
    const link = host.querySelector('input[aria-label="Invite link"]') as HTMLInputElement;
    expect(link.value).toBe("https://lighthouse.example.net/join#the-secret");
    expect(host.textContent).toContain("you will not see it again");

    await act(async () => button("Done").click());
    expect(host.querySelector('input[aria-label="Invite link"]')).toBeNull();
    expect(host.textContent).not.toContain("the-secret");
  });

  it("lists open invites with what they carry, and withdraws one", async () => {
    await mount();
    expect(host.textContent).toContain("for Sam");
    expect(host.textContent).toContain("PG and under");
    expect(host.textContent).toContain("1 library kept from them");
    await act(async () => button("Withdraw").click());
    await settle();
    expect(calls.some((c) => c.method === "DELETE" && c.url.endsWith("/api/invites/i1"))).toBe(true);
    expect(host.textContent).not.toContain("for Sam");
  });
});
