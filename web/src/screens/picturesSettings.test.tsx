/*
 * The Pictures settings pane.
 *
 * It exists because face grouping shipped with no way to set it up — the API
 * was there, the People page explained an absence, and nothing anywhere told
 * somebody what to do about it. These check the three states it has to keep
 * apart, and the one sentence that answers the most likely question.
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

type World = {
  ready: boolean;
  capsReason?: string;
  installed: boolean;
  running?: boolean;
};

let host: HTMLDivElement;
let root: Root;
let posted: string[];

function mount(w: World) {
  posted = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (v: unknown) =>
        new Response(JSON.stringify(v), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if ((init?.method ?? "GET") !== "GET") {
        posted.push(url);
        return json({ ok: true });
      }
      if (url.includes("/api/auth/status")) {
        return json({
          authenticated: true,
          configured: true,
          user: { id: "u1", name: "chris", role: "admin" },
        });
      }
      if (url.includes("/api/faces/capabilities")) {
        return json({ ready: w.ready, reason: w.capsReason });
      }
      if (url.includes("/api/faces/models")) {
        return json({
          supported: true,
          installed: w.installed,
          bytes_total: 118574000,
          directory: "C:\\ProgramData\\LANcast\\faces",
          assets: [
            {
              name: "face_detection_yunet_2023mar.onnx",
              size_bytes: 232589,
              licence: "MIT",
              licence_url: "https://example.invalid/mit",
              url: "https://example.invalid/yunet",
            },
          ],
          job: {
            running: w.running ?? false,
            stage: w.running ? "downloading" : "",
            asset: w.running ? "face_recognition_sface_2021dec.onnx" : "",
            bytes_done: w.running ? 59287000 : 0,
            bytes_total: w.running ? 118574000 : 0,
          },
        });
      }
      if (url.includes("/api/settings")) {
        return json({ sensitive_marking: true, scan_interval_hours: 24 });
      }
      if (url.includes("/api/libraries")) return json([]);
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

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <FocusProvider>
          <MemoryRouter initialEntries={["/settings?pane=pictures"]}>
            <Settings />
          </MemoryRouter>
        </FocusProvider>
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

describe("the Pictures pane", () => {
  // The setting moved here from Libraries, where it was the only picture
  // setting there was.
  it("carries the sensitive-folder switch", async () => {
    mount({ ready: false, installed: false });
    await render();
    expect(host.textContent).toContain("marked sensitive");
  });

  /*
   * Nothing is downloaded without being asked, so the size and the licences are
   * shown first. A download somebody cannot identify is not consent.
   */
  it("says what would be downloaded, and how big, before offering it", async () => {
    mount({ ready: false, installed: false });
    await render();
    expect(host.textContent).toContain("113 MB");
    expect(host.textContent).toContain("MIT");
    expect(host.textContent).toContain("Download the face models");
  });

  /*
   * The sentence that answers the most likely question about this feature.
   *
   * The in-app updater replaces the server only; the worker arrives with the
   * installer. Without this, somebody who updated in-app sees a feature that
   * reports itself unavailable and has nothing to act on — which is exactly
   * what happened on v0.8.46.
   */
  it("explains that the worker comes with the installer", async () => {
    mount({ ready: false, capsReason: "the face worker is not installed", installed: false });
    await render();
    expect(host.textContent).toContain("installer");
  });

  it("shows progress while a download runs, and offers to stop it", async () => {
    mount({ ready: false, installed: false, running: true });
    await render();
    expect(host.textContent).toContain("50%");
    expect(host.textContent).toContain("Cancel");
    // And does not simultaneously offer to start one.
    expect(host.textContent).not.toContain("Download the face models");
  });

  // Installed models with a missing worker is its own state: the fix is the
  // installer, not another download.
  it("distinguishes installed models from a ready feature", async () => {
    mount({
      ready: false,
      capsReason: "the face worker is not installed",
      installed: true,
    });
    await render();
    expect(host.textContent).toContain("models are installed");
    expect(host.textContent).toContain("worker itself is still missing");
  });

  it("says it is ready when it is", async () => {
    mount({ ready: true, installed: true });
    await render();
    expect(host.textContent).toContain("ready");
    expect(host.textContent).toContain("People");
  });
});
