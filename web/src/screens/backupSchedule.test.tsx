/*
 * The backup schedule, in Backup & storage.
 *
 * jsdom sees no layout; what it can prove is the wiring. The risks are that
 * the controls send the wrong field, and that "how many to keep" stays live
 * while the schedule is off, inviting somebody to tune a setting that does
 * nothing.
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
let sent: string[];

function mount(everyDays: number) {
  sent = [];
  const settings = {
    tmdb: { configured: true },
    opensubtitles: { configured: false },
    omdb: { configured: false },
    media_tools: { probe_available: true, transcode_available: true },
    encoder: { preference: "auto", active: "", available: [] },
    rate_per_sec: 5,
    write_nfo: false,
    auto_enrich: true,
    update_check: true,
    sensitive_marking: false,
    detect_markers: false,
    debug_logging: false,
    watched_threshold: 90,
    continue_weeks: 16,
    continue_limit: 40,
    allow_media_deletion: true,
    empty_trash_on_scan: false,
    scan_interval_hours: 0,
    audit_retention_days: 90,
    backup_every_days: everyDays,
    backup_keep: 4,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "dana", role: "admin" },
        });
      }
      if (url.includes("/api/backups")) {
        return json({ backups: [], folder: "C:\\data\\backups", restore_command: "x" });
      }
      if (url.includes("/api/settings")) {
        if (method === "PUT" || method === "PATCH") {
          sent.push(String(init?.body ?? ""));
          Object.assign(settings, JSON.parse(String(init?.body ?? "{}")));
        }
        return json(settings);
      }
      return json({});
    }),
  );
}

beforeEach(() => {
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
  for (let i = 0; i < 6; i++) {
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
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  await settle();
  const pane = [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Backup & storage",
  );
  expect(pane).toBeTruthy();
  await act(async () => pane!.click());
  await settle();
}

// The two schedule controls are the selects in the row with that title.
function control(title: string): HTMLSelectElement {
  const row = [...host.querySelectorAll(".set-row")].find(
    (r) => r.querySelector(".set-row__title")?.textContent === title,
  );
  const sel = row?.querySelector("select");
  expect(sel).toBeTruthy();
  return sel!;
}

async function choose(sel: HTMLSelectElement, value: string) {
  await act(async () => {
    sel.value = value;
    sel.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}

describe("the backup schedule", () => {
  it("shows the schedule the server holds, and sends a new interval", async () => {
    mount(7);
    await render();
    const every = control("Back up automatically");
    expect(every.value).toBe("7");
    await choose(every, "1");
    expect(sent.map((b) => JSON.parse(b))).toContainEqual({ backup_every_days: 1 });
  });

  it("sends how many to keep", async () => {
    mount(7);
    await render();
    await choose(control("Scheduled backups to keep"), "8");
    expect(sent.map((b) => JSON.parse(b))).toContainEqual({ backup_keep: 8 });
  });

  it("does not offer a keep count while the schedule is off", async () => {
    mount(0);
    await render();
    expect(control("Scheduled backups to keep").disabled).toBe(true);
  });
});
