/*
 * The browser cores block in Settings → Retro games (ADR 0077 part a).
 *
 * A download is consent, so what it says before the button is the point:
 * how big, from which release, which cores under which licences, and that
 * nothing is fetched until it is pressed. Then that the button asks the server
 * and says how it is going. jsdom performs no layout; this is about what the
 * pane says and sends.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { FocusProvider } from "@/focus/FocusController";
import { Settings } from "./Settings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let posted: string[];
let installed: boolean;
let running: boolean;

const core = (name: string, display: string, licence: string, consoles: string[]) => ({
  name,
  display,
  licence,
  source_url: `https://example.test/${name}`,
  consoles,
  size_bytes: 3_000_000,
  installed,
});

beforeEach(() => {
  posted = [];
  installed = false;
  running = false;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (url.includes("/api/auth/status")) {
        return json({ authenticated: true, configured: true, user: { id: "u1", name: "chris", role: "admin" } });
      }
      if (url.includes("/api/retro/web-cores")) {
        if (method === "POST") {
          posted.push(url);
          running = true;
          return json({ running: true, stage: "download", done: 0, total: 749072539 }, 202);
        }
        return json({
          cores: [
            core("fceumm", "FCEUmm", "GPL-2.0", ["nes"]),
            core("mgba", "mGBA", "MPL-2.0", ["gb", "gbc", "gba"]),
            core("gearsystem", "Gearsystem", "GPL-3.0", ["sms"]),
            core("clownmdemu", "ClownMDEmu", "AGPL-3.0", ["genesis"]),
          ],
          archive: { url: "https://buildbot.libretro.com/x", size_bytes: 749072539, version: "1.22.2" },
          job: running
            ? { running: true, stage: "download", done: 374536269, total: 749072539 }
            : { running: false, stage: "", done: 0, total: 0 },
        });
      }
      if (url.includes("/api/retro/database")) {
        return json({ installed: true, files: [], bytes_total: 0, job: { running: false, stage: "", bytes_done: 0, bytes_total: 0 } });
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
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function openRetro() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  const tab = [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Retro games");
  expect(tab, "no Retro games pane").toBeDefined();
  await act(async () => tab!.click());
  await settle();
}

const text = () => host.textContent ?? "";
const downloadButton = () =>
  [...host.querySelectorAll("button")].find((b) => b.textContent === "Download the browser cores");

describe("the browser cores in Settings", () => {
  it("says what would be fetched, under which licences, before anything is", async () => {
    await openRetro();
    expect(text()).toContain("Play in a browser");
    expect(text()).toContain("RetroArch 1.22.2");
    for (const l of ["FCEUmm (GPL-2.0", "mGBA (MPL-2.0", "Gearsystem (GPL-3.0", "ClownMDEmu (AGPL-3.0"]) {
      expect(text()).toContain(l);
    }
    expect(text()).toContain("Nothing is fetched until you press the button");
    expect(text()).toContain("SNES, N64 and PlayStation stay in the desktop app");
    expect(posted).toEqual([]);
  });

  it("asks the server to download, and says how it is going", async () => {
    await openRetro();
    await act(async () => downloadButton()!.click());
    await settle();
    expect(posted.some((u) => u.endsWith("/api/retro/web-cores/install"))).toBe(true);
    expect(text()).toContain("Downloading — 50%");
    expect(downloadButton()).toBeUndefined();
  });

  it("says they are installed once they are, and offers no download", async () => {
    installed = true;
    await openRetro();
    expect(text()).toContain("The browser cores are installed (RetroArch 1.22.2)");
    expect(downloadButton()).toBeUndefined();
  });
});
