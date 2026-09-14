/*
 * The desktop window's own log, in Settings.
 *
 * The load-bearing assertion is the first one: this section must not exist
 * where the binding does not. The server has never seen this file, so a browser
 * tab or a phone offering to show it would be offering to show nothing — which
 * is the same feature-detection rule the lifecycle section beside it keeps.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ClientLog } from "./ClientLog";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: number;

function bind(answer: Record<string, unknown>) {
  calls = 0;
  (window as unknown as Record<string, unknown>).lancastClientLog = async () => {
    calls++;
    return answer;
  };
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <ClientLog />
      </QueryClientProvider>,
    );
  });
  await settle();
}

// React Query notifies on a macrotask, so a resolved promise is not enough.
async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

const text = () => host.textContent ?? "";
const button = (label: string) =>
  [...host.querySelectorAll("button")].find((b) =>
    (b.textContent ?? "").includes(label),
  );

async function click(label: string) {
  await act(async () => {
    button(label)!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  });
  await settle();
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  delete (window as unknown as Record<string, unknown>).lancastClientLog;
  vi.unstubAllGlobals();
});

describe("the window's own log", () => {
  it("does not exist in a browser tab", async () => {
    // No binding, no file, nothing to show. Offering it anyway would be
    // offering to read a file on somebody else's machine.
    await render();
    expect(text()).toBe("");
  });

  it("does not read the file until it is asked to", async () => {
    /*
     * This is read after something has already gone wrong. Fetching four
     * hundred lines every time somebody opens Settings is a cost paid for ever
     * for the rare case.
     */
    bind({ path: "C:/x/lancast-client.log", lines: ["a"], complete: true });
    await render();
    expect(calls).toBe(0);
    await click("Show log");
    expect(calls).toBe(1);
  });

  it("shows the lines and where they came from", async () => {
    bind({
      path: "C:/x/lancast-client.log",
      lines: ["client started", "waiting for the service"],
      complete: true,
    });
    await render();
    await click("Show log");
    expect(text()).toContain("client started");
    expect(text()).toContain("waiting for the service");
    // The path is here to be copied into a bug report.
    expect(text()).toContain("lancast-client.log");
  });

  it("says when it is showing the end of the file rather than the file", async () => {
    // The difference between "this is the log" and "this is the end of it".
    bind({ path: "C:/x/lancast-client.log", lines: ["late line"], complete: false });
    await render();
    await click("Show log");
    expect(text()).toContain("Showing the last");
  });

  it("does not call an empty log a fault", async () => {
    // A window that has only ever run from a terminal may never have one.
    bind({ path: "C:/x/lancast-client.log", lines: [], complete: true });
    await render();
    await click("Show log");
    expect(text()).toContain("Nothing written yet");
    expect(text()).not.toContain("Could not read");
  });

  it("reports a real failure as one", async () => {
    bind({ error: "access is denied" });
    await render();
    await click("Show log");
    expect(text()).toContain("access is denied");
  });
});
