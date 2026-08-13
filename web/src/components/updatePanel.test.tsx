/*
 * The update panel's download state.
 *
 * Reported from a real install: the panel sat on "Downloading…" while the
 * activity indicator, three inches away, already said the update was ready.
 * Nothing was wrong on the server — POST /api/update/download returns
 * immediately and downloads in the background, and the panel's status query was
 * cached for a minute with nothing to tell it to look again. Two surfaces
 * reading the same server and disagreeing, which reads as a hang.
 *
 * So what is asserted here is the *refetch*: that pressing Download makes the
 * panel keep asking, and that it stops asking once the answer arrives.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { UpdateSettings, plainVersion } from "./UpdateSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let statusBody: Record<string, unknown>;
let statusReads: number;
let healthBody: Record<string, unknown>;

const base = {
  supported: true,
  current: "0.6.13",
  latest: "v0.6.14",
  available: true,
  can_verify: true,
  enabled: true,
  checking: false,
  error: "",
  download_error: "",
};

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  statusBody = { ...base };
  statusReads = 0;
  healthBody = { status: "ok", version: "0.6.13", api_version: 1 };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") return json({});
      if (url.includes("/api/update")) {
        statusReads++;
        return json(statusBody);
      }
      if (url.includes("/api/health")) return json(healthBody);
      if (url.includes("/api/settings")) return json({ update_check: true });
      return json({});
    }),
  );
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
        <MemoryRouter>
          <UpdateSettings />
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  await settle();
}

async function settle(ms = 5) {
  await act(async () => {
    await new Promise((r) => setTimeout(r, ms));
  });
}

function button(text: RegExp): HTMLButtonElement {
  const b = [...host.querySelectorAll<HTMLButtonElement>("button")].find((x) =>
    text.test(x.textContent ?? ""),
  );
  if (!b) throw new Error(`no button matching ${text}: ${host.textContent}`);
  return b;
}

/*
 * The comparison that hung.
 *
 * Reported from a real update: the server restarted, came back on the new
 * version, and the panel sat on "Installing…" for ever. `staged` is the release
 * tag as GitHub published it — "v0.6.16" — and /api/health reports the string
 * the build injected — "0.6.16". An exact comparison between them can never
 * match, and the earlier test missed it by using the same string on both sides,
 * which is the shape of assumption a test is supposed to catch rather than
 * share.
 */
describe("version comparison", () => {
  it("treats a release tag and a running version as the same version", () => {
    expect(plainVersion("v0.6.16")).toBe(plainVersion("0.6.16"));
    expect(plainVersion("V1.2.3")).toBe("1.2.3");
    expect(plainVersion(undefined)).toBe("");
    // Not a blanket strip: only a leading v, and only one.
    expect(plainVersion("vv1.0.0")).toBe("v1.0.0");
  });
});

describe("update panel", () => {
  // Real timers: the panel polls every 2s, so these wait it out. Slow for a
  // unit test and worth it — the bug was entirely about time passing and
  // nothing happening.
  it("keeps asking after a download starts, and notices when it is staged", { timeout: 20000 }, async () => {
    await render();
    await act(async () => button(/Download and install/).click());
    await settle();

    const afterStart = statusReads;
    // The download runs in the background, so the only way the panel learns
    // anything is by asking again.
    await settle(2500);
    expect(statusReads).toBeGreaterThan(afterStart);

    // The server finishes: staged appears.
    statusBody = { ...base, staged: "v0.6.14", staged_at: 1 };
    await settle(2500);
    expect(host.textContent).toContain("ready to install");

    // And it stops polling once there is nothing to wait for — a panel that
    // keeps hitting an admin endpoint every two seconds for ever is its own
    // bug.
    const afterStaged = statusReads;
    await settle(2500);
    expect(statusReads).toBe(afterStaged);
  });

  it("finishes installing when the server comes back, tag prefix and all", { timeout: 20000 }, async () => {
    // Staged, the way the server actually reports it: with the v.
    statusBody = { ...base, staged: "v0.6.14", staged_at: 1 };
    await render();
    await act(async () => button(/Install and restart/).click());
    await settle();
    expect(host.textContent).toContain("Installing");

    // The server comes back as the build reports itself: without the v.
    healthBody = { status: "ok", version: "0.6.14", api_version: 1 };
    await settle(2500);
    expect(host.textContent).toContain("Updated to LANcast 0.6.14");
  });

  /*
   * The floor under the whole thing.
   *
   * Three releases running shipped a confident version comparison that was
   * wrong in a way nobody could see until an update ran, and the cost each time
   * was a panel saying "Installing…" for ever over a server that had finished
   * minutes earlier. So the panel now has a state it cannot fail to reach: if
   * the server is answering and the version still proves nothing, it says what
   * it actually knows.
   *
   * The version here never changes and never matches — the shape of every bug
   * this has had.
   */
  it("stops waiting eventually, even when the version proves nothing", { timeout: 70000 }, async () => {
    // The server keeps reporting the version it started on: neither signal ever
    // fires. That is what a broken comparison looks like from here — and what
    // an install that genuinely did not take would look like too, which is why
    // the fallback says "reload and look" rather than claiming success.
    statusBody = { ...base, staged: "v0.6.14", staged_at: 1 };
    healthBody = { status: "ok", version: "0.6.13", api_version: 1 };
    await render();
    await act(async () => button(/Install and restart/).click());
    await settle();
    expect(host.textContent).toContain("Installing");

    // Past the deadline, with the server answering the whole time.
    await settle(46_000);
    expect(host.textContent).toContain("may have restarted");
    expect(host.textContent).toContain("reload");
    expect(host.textContent).not.toContain("Installing LANcast");
  });

  it("stops waiting when the download fails rather than saying Downloading for ever", { timeout: 20000 }, async () => {
    await render();
    await act(async () => button(/Download and install/).click());
    await settle();

    statusBody = { ...base, download_error: "checksum did not match" };
    await settle(2500);

    expect(host.textContent).toContain("checksum did not match");
    const settled = statusReads;
    await settle(2500);
    expect(statusReads).toBe(settled);
  });
});
