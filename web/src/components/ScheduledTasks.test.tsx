/*
 * Status › Scheduled tasks: every background job, what sets it off, when it
 * last ran and how, and Run now.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ScheduledTasks } from "./ScheduledTasks";
import { when } from "@/lib/when";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let runs: string[];

const now = new Date();
const earlier = new Date(now.getTime() - 60 * 60 * 1000).toISOString();

const list = [
  { id: "scan", name: "Scan libraries", description: "Finds files.", trigger: "interval", every_hours: 24, enabled: true, running: false, last_finished_at: earlier, last_started_at: earlier, next_at: earlier },
  { id: "probe", name: "Read media files", description: "Reads streams.", trigger: "after-scan", enabled: true, running: true },
  { id: "markers", name: "Detect intros and credits", description: "Skips.", trigger: "after-scan", enabled: false, running: false },
  { id: "update", name: "Check for updates", description: "Asks GitHub.", trigger: "interval", every_hours: 24, enabled: true, running: false, last_finished_at: earlier, last_error: "no route to host" },
];

beforeEach(() => {
  runs = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const m = url.match(/^\/api\/tasks\/([^/]+)\/run$/);
      if (m && init?.method === "POST") {
        runs.push(m[1]);
        return new Response(null, { status: 202 });
      }
      return new Response(JSON.stringify({ tasks: list }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
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
        <ScheduledTasks />
      </QueryClientProvider>,
    );
  });
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const row = (name: string) =>
  [...host.querySelectorAll<HTMLElement>(".tasks__row")].find((r) =>
    r.querySelector(".tasks__name")?.textContent?.startsWith(name),
  );

describe("the scheduled tasks pane", () => {
  it("says what sets each job off, and when it last ran", async () => {
    await render();
    expect(row("Scan libraries")?.textContent).toContain("Daily");
    expect(row("Read media files")?.textContent).toContain("After each scan");
    expect(row("Read media files")?.textContent).toContain("Running now");
    expect(row("Detect intros and credits")?.textContent).toContain("Off — set in Settings");
    expect(row("Check for updates")?.textContent).toContain("Failed: no route to host");
  });

  it("runs a job now, and will not start one already running", async () => {
    await render();
    const scan = row("Scan libraries")!.querySelector("button")!;
    await act(async () => scan.click());
    expect(runs).toEqual(["scan"]);
    // Said, so a job that finishes in milliseconds still visibly answered.
    expect(row("Scan libraries")!.querySelector("button")!.textContent).toBe("Started");
    const probe = row("Read media files")!.querySelector("button")!;
    expect(probe.disabled).toBe(true);
    expect(probe.textContent).toBe("Running…");
  });

  it("offers no Run now on a job switched off, and says a past-due job is due now", async () => {
    await render();
    expect(row("Detect intros and credits")!.querySelector("button")!.disabled).toBe(true);
    // The scan's next run (an hour ago in the fixture) is due now.
    expect(row("Scan libraries")?.textContent).toContain("Due now");
  });
});

/*
 * The server sends UTC; the day a person sees must be their own. A date taken
 * from the ISO string is tomorrow every evening in the US, which has shipped
 * in this owner's other projects before.
 */
describe("when", () => {
  it("names today, yesterday and tomorrow by the local calendar", () => {
    const base = new Date(2026, 9, 12, 21, 30); // 12 Oct, 21:30 local
    const at = (d: number, h: number) => new Date(2026, 9, d, h, 5).toISOString();
    expect(when(at(12, 3), base)).toMatch(/^Today /);
    expect(when(at(11, 23), base)).toMatch(/^Yesterday /);
    expect(when(at(13, 3), base)).toMatch(/^Tomorrow /);
    expect(when(at(9, 3), base)).not.toMatch(/^(Today|Yesterday|Tomorrow)/);
    expect(when(new Date(base.getTime() - 20_000).toISOString(), base)).toBe("Just now");
  });
});
