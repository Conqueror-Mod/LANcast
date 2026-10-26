/*
 * Settings › About shows what the server says it is: the version, the
 * release notes committed for it, and the notice — all embedded at build
 * time, so the pane asks nobody else's server.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { AboutSettings, parseNotice, splitSections } from "./AboutSettings";

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
  notice: "LANcast\nThird-party components\n\nGo dependencies (see go.mod):\n\n  modernc.org/sqlite  BSD 3-Clause\n",
};

describe("the About pane", () => {
  it("shows the version, this release's notes and the notice, from the server only", async () => {
    stub({
      ...base,
      notes: "## One program, one icon\n\n- **A single LANcast.** It replaces `LANcast-Server.exe`.\n  - nested point\n",
    });
    await render();
    const server = [...host.querySelectorAll(".about__facts tr")].find(
      (r) => r.querySelector("th")?.textContent === "Server",
    );
    expect(server?.querySelector("td")?.textContent).toBe("0.9.79");
    // Each section of the notes is its own card, titled by its heading.
    expect(host.querySelector(".about__card-title")?.textContent).toBe("One program, one icon");
    expect(host.querySelector(".about__notes strong")?.textContent).toBe("A single LANcast.");
    expect(host.querySelector(".about__notes code")?.textContent).toBe("LANcast-Server.exe");
    expect(host.querySelector(".about__notes li li")?.textContent).toBe("nested point");
    expect(host.querySelector(".about__notice pre")?.textContent).toContain("modernc.org/sqlite");
    // And the components are a table, read out of the notice.
    expect(host.querySelector(".about__components td")?.textContent).toBe("modernc.org/sqlite");
    // No phone-home: the only request is to this server.
    expect(requested.every((u) => u.startsWith("/api/"))).toBe(true);
  });

  it("names the developer, and reports by mail with the version filled in", async () => {
    stub(base);
    await render();
    const links = [...host.querySelectorAll<HTMLAnchorElement>("a")];
    const report = links.find((a) => a.textContent === "Report an issue");
    expect(report?.href.startsWith("mailto:conqueror.conqueredtime@gmail.com?")).toBe(true);
    expect(decodeURIComponent(report!.href)).toContain("LANcast version: 0.9.79");
    const kofi = links.find((a) => a.textContent === "Support on Ko-fi");
    expect(kofi?.getAttribute("href")).toBe("https://ko-fi.com/christheconqueror");
    // Links the person follows; the pane itself still asks only this server.
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

describe("reading the notes and the notice", () => {
  it("draws italics, and code inside bold, rather than their markers", async () => {
    stub({
      ...base,
      notes: '## Updating\n\nRun **`LANcast-Setup-0.9.80.exe`**, ("because you watched *the previous one*").\n',
    });
    await render();
    const notes = host.querySelector(".about__notes")!;
    expect(notes.querySelector("strong code")?.textContent).toBe("LANcast-Setup-0.9.80.exe");
    expect(notes.querySelector("em")?.textContent).toBe("the previous one");
    expect(notes.textContent).not.toContain("*");
    expect(notes.textContent).not.toContain("`");
  });

  it("cuts the notes into one section per heading", () => {
    const s = splitSections("## A\n- one\n\n## B\n- two\n");
    expect(s.map((x) => x.title)).toEqual(["A", "B"]);
  });

  /*
   * Against the real NOTICE, so a change to its layout that the parser cannot
   * read fails here rather than leaving the About pane with an empty table.
   */
  it("reads every group and its rows out of the real NOTICE", () => {
    const notice = readFileSync(resolve(process.cwd(), "../NOTICE"), "utf8");
    const groups = parseNotice(notice);
    const titles = groups.map((g) => g.title);
    expect(titles[0]).toBe("Go dependencies");
    expect(titles).toContain("Not bundled, and deliberately so");
    const go = groups[0].rows;
    expect(go.find((r) => r.name === "modernc.org/sqlite")?.licence).toBe("BSD 3-Clause");
    const all = groups.flatMap((g) => g.rows);
    // A name wrapped across lines is one row.
    const mpv = all.find((r) => r.name.startsWith("libmpv"));
    expect(mpv?.name).toBe("libmpv (mpv, FFmpeg, libplacebo, dav1d, shaderc, SPIRV-Cross, zlib)");
    expect(mpv?.licence).toBe("GNU Lesser General Public License v2.1");
    // A name that runs up to the licence column, with one space before it.
    const cores = all.find((r) => r.name.startsWith("Emulator cores"));
    expect(cores?.name).toBe(
      "Emulator cores (mGBA, Mesen, bsnes, BlastEm, Gearsystem, Mupen64Plus-Next, SwanStation)",
    );
    expect(cores?.licence).toBe("GPL-2.0, GPL-3.0 or MPL-2.0 each.");
    // And nothing is a fragment of a paragraph.
    expect(all.every((r) => r.name.length > 0 && r.licence.length > 0)).toBe(true);
  });
});
