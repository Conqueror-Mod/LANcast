/*
 * Settings › About shows what the server says it is: the version, the
 * release notes committed for it, and the notice — all embedded at build
 * time, so the pane asks nobody else's server.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AboutSettings } from "./AboutSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let requested: string[];

function stub(about: Record<string, unknown>) {
  requested = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      requested.push(url);
      return new Response(JSON.stringify(about), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={qc}>
        <AboutSettings />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const base = {
  version: "0.9.79",
  repository: "https://github.com/Conqueror-Mod/LANcast",
  licence: "AGPL-3.0-only",
  notice: "LANcast\nThird-party components\n  modernc.org/sqlite  BSD 3-Clause",
};

describe("the About pane", () => {
  it("shows the version, this release's notes and the notice, from the server only", async () => {
    stub({
      ...base,
      notes: "## One program, one icon\n\n- **A single LANcast.** It replaces `LANcast-Server.exe`.\n  - nested point\n",
    });
    await render();
    expect(host.textContent).toContain("Server 0.9.79");
    expect(host.querySelector("h4")?.textContent).toBe("One program, one icon");
    expect(host.querySelector(".about__notes strong")?.textContent).toBe("A single LANcast.");
    expect(host.querySelector(".about__notes code")?.textContent).toBe("LANcast-Server.exe");
    expect(host.querySelector(".about__notes li li")?.textContent).toBe("nested point");
    expect(host.querySelector(".about__notice pre")?.textContent).toContain("modernc.org/sqlite");
    // No phone-home: the only request is to this server.
    expect(requested.every((u) => u.startsWith("/api/"))).toBe(true);
  });

  it("says so when no notes were recorded for this version", async () => {
    stub({ ...base, version: "dev" });
    await render();
    expect(host.textContent).toContain("No release notes are recorded for this version.");
  });

  it("draws markup in the notes as text, never as HTML", async () => {
    stub({ ...base, notes: '- <img src=x onerror="alert(1)"> **bold**' });
    await render();
    expect(host.querySelector(".about__notes img")).toBeNull();
    expect(host.querySelector(".about__notes li")?.textContent).toContain("<img src=x");
  });
});
