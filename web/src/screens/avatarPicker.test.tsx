/*
 * The picture an account chooses on its profile, and the rail that shows it.
 *
 * The rail reads the account from auth status, so choosing has to refresh that
 * query and not only the profile's — otherwise the profile says Owl and the
 * rail keeps the old glyph until the next navigation, the quiet kind of stale
 * CLAUDE.md warns about.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { Profile } from "./Profile";
import { AvatarGlyph } from "@/components/Avatar";
import { useCurrentUser } from "@/api/hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let stored: string;
let puts: unknown[];

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  stored = "";
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/profile/avatar") && init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as { avatar: string };
        puts.push(body);
        stored = body.avatar;
        return json({ avatar: stored });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          configured: true,
          authenticated: true,
          user: { id: "u1", name: "Conqueror", role: "admin", avatar: stored },
        });
      }
      if (url.includes("/api/profile")) {
        return json({
          user: { name: "Conqueror", admin: true, secured: true },
          stats: { started: 1, finished: 1, watched_ms: 1, first_at: null },
          history: [],
          total: 0,
        });
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

/** Stands in for the rail's account row: the same hook, the same glyph. */
function RailAccount() {
  const user = useCurrentUser();
  return (
    <span className="rail-account">
      <AvatarGlyph avatar={user?.avatar} />
    </span>
  );
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <RailAccount />
          <Profile />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const railAvatar = () =>
  host.querySelector(".rail-account svg")?.getAttribute("data-avatar") ?? null;
const choice = (label: string) =>
  host.querySelector<HTMLButtonElement>(`.profile__avatar[aria-label="${label}"]`)!;

describe("choosing a picture", () => {
  it("offers six, and the rail shows the one chosen", async () => {
    await render();
    expect(host.querySelectorAll(".profile__avatar").length).toBe(6);
    expect(railAvatar()).toBeNull(); // the plain person glyph

    await act(async () => choice("Owl").click());
    await settle();

    expect(puts).toEqual([{ avatar: "owl" }]);
    expect(choice("Owl").getAttribute("aria-pressed")).toBe("true");
    expect(railAvatar(), "the rail kept the old picture").toBe("owl");
  });

  it("clears it when the chosen one is pressed again", async () => {
    stored = "fox";
    await render();
    expect(railAvatar()).toBe("fox");

    await act(async () => choice("Fox").click());
    await settle();

    expect(puts).toEqual([{ avatar: "" }]);
    expect(railAvatar()).toBeNull();
  });
});
