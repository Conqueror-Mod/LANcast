/*
 * The age-rating country picker, in the Metadata pane.
 *
 * jsdom sees no layout, so nothing here is about how the control looks. What
 * it can prove is the wiring, and the wiring is where this control's risk sits:
 * the value it sends decides what goes into `content_rating`, and
 * `content_rating` is what an account's rating ceiling reads. A picker that
 * offered a country the server never listed would let somebody choose one whose
 * labels no ceiling can place — and the symptom of that is a child's library
 * quietly shrinking, not an error.
 *
 * So the assertions are about the *request* and about *what is offered*, in the
 * same spirit as libraryQueue.test.ts: a queue of the wrong film is a perfectly
 * good queue, and a setting that stores the wrong country is a perfectly good
 * setting.
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
let sent: { url: string; body: string }[];

// What the server actually serves. Deliberately not the full list: the point of
// serving it is that the client does not know it, so a fixture that happened to
// match a hard-coded copy would prove nothing.
const OFFERED = [
  { code: "US", name: "United States" },
  { code: "GB", name: "United Kingdom" },
  { code: "DE", name: "Germany" },
];

function mount(opts: {
  country?: string;
  countries?: { code: string; name: string }[] | undefined;
}) {
  sent = [];
  const settings = {
    tmdb: { configured: true },
    opensubtitles: { configured: false },
    omdb: { configured: false },
    certification_country: opts.country ?? "",
    certification_countries: opts.countries,
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
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/settings")) {
        if (method === "PUT" || method === "PATCH") {
          sent.push({ url, body: String(init?.body ?? "") });
          Object.assign(settings, JSON.parse(String(init?.body ?? "{}")));
          return json(settings);
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
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
  // The picker lives in the Metadata pane, which is not the one Settings opens
  // on.
  const metadata = [...host.querySelectorAll("button")].find(
    (b) => b.textContent?.trim() === "Metadata",
  );
  if (metadata) {
    await act(async () => metadata.click());
    await settle();
  }
}

// The picker is the only select in this pane offering a country name.
function picker(): HTMLSelectElement | undefined {
  return [...host.querySelectorAll("select")].find((s) =>
    [...s.options].some((o) => o.textContent?.includes("United Kingdom")),
  );
}

describe("the age-rating country picker", () => {
  it("offers exactly what the server listed", async () => {
    mount({ countries: OFFERED });
    await render();

    const el = picker();
    expect(el).toBeTruthy();
    const codes = [...el!.options].map((o) => o.value);
    // The served codes, plus the empty value that means the default order.
    expect(codes).toEqual(["", "US", "GB", "DE"]);
  });

  it("does not invent a country the server did not list", async () => {
    /*
     * The fault this file exists for. France is a real certification country
     * and TMDB returns its certificates; the server leaves it out because the
     * rating ladder cannot place "Tous publics", and a ceiling blocks what it
     * cannot place. A client with its own list would offer it.
     */
    mount({ countries: OFFERED });
    await render();

    const el = picker();
    const codes = [...el!.options].map((o) => o.value);
    expect(codes).not.toContain("FR");
    expect(codes).not.toContain("JP");
  });

  it("sends the chosen code and nothing else", async () => {
    mount({ countries: OFFERED });
    await render();

    const el = picker()!;
    el.value = "DE";
    await act(async () => {
      el.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();

    expect(sent.length).toBe(1);
    const body = JSON.parse(sent[0].body);
    expect(body).toEqual({ certification_country: "DE" });
  });

  it("shows the country already chosen", async () => {
    // Otherwise the control reads as unset on every visit and somebody sets it
    // again, which is how a setting gets reported as not saving.
    mount({ country: "GB", countries: OFFERED });
    await render();

    expect(picker()!.value).toBe("GB");
  });

  it("offers a way back to the default order", async () => {
    /*
     * Empty is a real value, not a missing one. Without it the choice is
     * one-way: having picked Germany there would be no control that returns to
     * "the American certificate, or the British one".
     */
    mount({ country: "DE", countries: OFFERED });
    await render();

    const el = picker()!;
    el.value = "";
    await act(async () => {
      el.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await settle();

    expect(JSON.parse(sent[0].body)).toEqual({ certification_country: "" });
  });

  it("shows nothing at all against a server that does not offer the choice", async () => {
    /*
     * A client newer than its server is an ordinary state here — the installer
     * replaces the web bundle while an in-app update replaces the server — so
     * the field is optional in the contract, and a picker with no options would
     * be a control that cannot be used and cannot be explained.
     */
    mount({ countries: undefined });
    await render();

    expect(picker()).toBeUndefined();
    expect(host.textContent ?? "").not.toContain("Age rating country");
  });
});
