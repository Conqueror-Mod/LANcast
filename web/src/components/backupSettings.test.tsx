/*
 * The Backup pane.
 *
 * The jsdom suite cannot see layout, so nothing here is about how this looks.
 * What it can prove is wiring and behaviour, which is where this pane's real
 * risks are: a delete that arms every row at once, a list that goes stale
 * after a write, a download link pointing at the wrong path, and a backup this
 * build cannot restore being shown as though it could.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BackupSettings, humanBytes, takenAt } from "./BackupSettings";
import type { BackupsResponse } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

const RESTORABLE = {
  name: "lancast-backup-20260903-144529.db",
  bytes: 102834176,
  taken_at: 1788452729,
  schema_version: 39,
  restorable: true,
};

const FROM_THE_FUTURE = {
  name: "lancast-backup-20260904-090000.db",
  bytes: 102834176,
  taken_at: 1788500000,
  schema_version: 40,
  restorable: false,
  problem: "taken by a newer LANcast — update before restoring it",
};

function body(backups: BackupsResponse["backups"]): BackupsResponse {
  return {
    backups,
    folder: "C:\\ProgramData\\LANcast\\backups",
    restore_command: "LANcast-Server.exe restore -from <file> -yes",
  };
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.restoreAllMocks();
});

// render mounts the pane against a stubbed fetch and waits for the list.
async function render(
  backups: BackupsResponse["backups"],
  onFetch?: typeof fetch,
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const stub =
    onFetch ??
    (vi.fn(
      async () =>
        new Response(JSON.stringify(body(backups)), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch);
  vi.stubGlobal("fetch", stub);

  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <BackupSettings />
      </QueryClientProvider>,
    );
  });
  await settle();
  return stub;
}

/*
 * settle drains the query. A single microtask is not enough: react-query
 * resolves the fetch, sets state, and re-renders across several turns, and a
 * test that checks after one tick asserts against an empty pane and passes for
 * the wrong reason when the assertion is negative.
 */
async function settle() {
  for (let i = 0; i < 20; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

function buttons(): HTMLButtonElement[] {
  return [...host.querySelectorAll<HTMLButtonElement>("button")];
}

function click(el: Element) {
  act(() => {
    el.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
}

describe("the backup pane", () => {
  it("says what a backup does and does not contain", async () => {
    await render([]);
    const text = host.textContent ?? "";
    // The honesty requirement: somebody who believes this file holds their
    // films has been given a false sense of a backup, not a backup.
    expect(text).toContain("not included");
    expect(text.toLowerCase()).toContain("watch history");
  });

  it("says plainly when nothing is protected yet", async () => {
    await render([]);
    expect(host.textContent).toContain("No backups yet");
  });

  it("lists a backup with its size and a download link to its own name", async () => {
    await render([RESTORABLE]);
    const link = host.querySelector<HTMLAnchorElement>("a[download]");
    expect(link).toBeTruthy();
    expect(link?.getAttribute("href")).toBe(`/api/backups/${RESTORABLE.name}`);
    expect(host.textContent).toContain("98.1 MB");
  });

  /*
   * A backup this build cannot restore is the most important thing this list
   * can say. Shown on the row, so it is known now rather than during a restore.
   */
  it("marks a backup it cannot restore, and says why", async () => {
    await render([FROM_THE_FUTURE]);
    expect(host.textContent).toContain("Cannot be restored");
    expect(host.textContent).toContain("newer LANcast");
  });

  /*
   * Arming one row must not arm the others. Every entry in this list is called
   * nearly the same thing, so a boolean would put "Delete for good?" on all of
   * them and the next click would delete whichever the mouse was over.
   */
  it("arms only the row that was pressed", async () => {
    await render([RESTORABLE, FROM_THE_FUTURE]);
    const deletes = buttons().filter((b) => b.textContent === "Delete");
    expect(deletes.length).toBe(2);

    click(deletes[0]);

    const armed = buttons().filter((b) => b.textContent === "Delete for good?");
    const unarmed = buttons().filter((b) => b.textContent === "Delete");
    expect(armed.length).toBe(1);
    expect(unarmed.length).toBe(1);
  });

  it("does not delete on the first press", async () => {
    const calls: string[] = [];
    const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      calls.push(`${init?.method ?? "GET"} ${String(input)}`);
      return new Response(JSON.stringify(body([RESTORABLE])), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
    await render([RESTORABLE], stub);

    click(buttons().find((b) => b.textContent === "Delete")!);
    expect(calls.some((c) => c.startsWith("DELETE"))).toBe(false);
  });

  /*
   * The most-repeated bug in this project is a write that changes what a list
   * holds without invalidating that list. Taking a backup changes this list, so
   * the list must be re-read afterwards.
   */
  it("re-reads the list after taking a backup", async () => {
    const calls: string[] = [];
    const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      calls.push(`${method} ${String(input)}`);
      if (method === "POST") {
        return new Response(JSON.stringify(RESTORABLE), {
          status: 201,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify(body([RESTORABLE])), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;

    await render([], stub);
    const before = calls.filter((c) => c.startsWith("GET")).length;

    await act(async () => {
      buttons()
        .find((b) => b.textContent === "Take a backup")!
        .dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await settle();

    expect(calls.some((c) => c.startsWith("POST"))).toBe(true);
    const after = calls.filter((c) => c.startsWith("GET")).length;
    expect(after).toBeGreaterThan(before);
  });

  it("shows the restore command rather than a restore button", async () => {
    await render([RESTORABLE]);
    // No control may offer to restore: it would have to lie, because
    // restoring replaces the database the server is reading.
    const labels = buttons().map((b) => b.textContent?.toLowerCase() ?? "");
    expect(labels.some((l) => l.includes("restore"))).toBe(false);
    expect(host.textContent).toContain("restore -from");
  });

  it("shows where the files are kept", async () => {
    await render([RESTORABLE]);
    expect(host.textContent).toContain("ProgramData");
  });
});

describe("humanBytes", () => {
  it("reads at a glance", () => {
    expect(humanBytes(0)).toBe("0 bytes");
    expect(humanBytes(512)).toBe("512 bytes");
    expect(humanBytes(2048)).toBe("2.0 KB");
    expect(humanBytes(102834176)).toBe("98.1 MB");
  });
});

describe("takenAt", () => {
  /*
   * Local, not UTC. A date built from UTC components reads as tomorrow's
   * backup all evening in every US timezone, and that has shipped in this
   * project before.
   */
  it("formats in local time", () => {
    const epoch = 1788452729;
    const local = new Date(epoch * 1000);
    expect(takenAt(epoch)).toContain(String(local.getFullYear()));
    // The date part must match the *local* calendar day, which is the whole
    // point — an ISO slice would answer the UTC one.
    expect(takenAt(epoch)).toBe(local.toLocaleString());
  });
});
