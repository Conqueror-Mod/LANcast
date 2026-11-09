/*
 * Home preferences moving from the device to the account (ADR 0120,
 * settings-3-plan step 3).
 *
 * What can go wrong here is quiet in both directions: a carry-over that never
 * sends leaves the choice stranded on one machine, and one that sends over an
 * account's own choice undoes what somebody set on another machine. Both
 * succeed as requests, so neither would fail anything but this.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { writeDevice, forgetDevice } from "./device";
import {
  HomePrefsCarryOver,
  LEGACY_PINNED_KEY,
  LEGACY_SPOILERS_KEY,
  LEGACY_SPOTLIGHT_KEY,
} from "./homePrefs";
import { useHeroMode } from "./heroMode";
import { useSpoilerMode } from "./spoilers";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let puts: unknown[];
let account: Record<string, unknown>;
let failPut: boolean;

function stub() {
  puts = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/api/profile/home")) {
        if (init?.method === "PUT") {
          if (failPut) return new Response('{"error":"no"}', { status: 500 });
          const body = JSON.parse(String(init.body));
          puts.push(body);
          account = { ...account, ...body };
        }
        return new Response(JSON.stringify(account), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
}

async function render(ui: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  await act(async () => {
    root.render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
  });
  await settle();
}

async function settle() {
  for (let i = 0; i < 8; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

function Probe() {
  const [mode, setMode] = useHeroMode();
  const [spoilers] = useSpoilerMode();
  return (
    <button onClick={() => setMode("recent")}>
      {mode}/{spoilers}
    </button>
  );
}

const fresh = () => ({
  account: true, spotlight: null, spotlight_item: null, spoilers: null, hidden_shelves: [],
});

beforeEach(() => {
  for (const k of [LEGACY_SPOTLIGHT_KEY, LEGACY_PINNED_KEY, LEGACY_SPOILERS_KEY]) forgetDevice(k);
  account = fresh();
  failPut = false;
  stub();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("carrying a device's choice over", () => {
  it("sends what the account has never chosen, then forgets the device copy", async () => {
    writeDevice(LEGACY_SPOTLIGHT_KEY, "pinned");
    writeDevice(LEGACY_PINNED_KEY, 42);
    writeDevice(LEGACY_SPOILERS_KEY, "all");
    await render(<><HomePrefsCarryOver /><Probe /></>);

    expect(puts).toEqual([{ spotlight: "pinned", spotlight_item: 42, spoilers: "all" }]);
    expect(localStorage.getItem(LEGACY_SPOTLIGHT_KEY)).toBeNull();
    expect(localStorage.getItem(LEGACY_SPOILERS_KEY)).toBeNull();
    // Still the same choice on screen, now read from the account.
    expect(host.textContent).toBe("pinned/all");
  });

  it("does not overwrite what the account already holds", async () => {
    account = { ...fresh(), spotlight: "recommended" };
    writeDevice(LEGACY_SPOTLIGHT_KEY, "recent");
    writeDevice(LEGACY_SPOILERS_KEY, "show");
    await render(<><HomePrefsCarryOver /><Probe /></>);

    expect(puts).toEqual([{ spoilers: "show" }]);
    expect(localStorage.getItem(LEGACY_SPOTLIGHT_KEY)).toBeNull();
    expect(host.textContent).toBe("recommended/show");
  });

  it("sends nothing when the device never chose", async () => {
    await render(<HomePrefsCarryOver />);
    expect(puts).toEqual([]);
  });

  it("keeps the device copy when the save fails, to try again", async () => {
    failPut = true;
    writeDevice(LEGACY_SPOILERS_KEY, "all");
    await render(<><HomePrefsCarryOver /><Probe /></>);
    expect(JSON.parse(localStorage.getItem(LEGACY_SPOILERS_KEY)!)).toBe("all");
    expect(host.textContent).toBe("continue/all");
  });

  it("leaves a server with no accounts on the device", async () => {
    account = { ...fresh(), account: false };
    writeDevice(LEGACY_SPOILERS_KEY, "all");
    await render(<><HomePrefsCarryOver /><Probe /></>);
    expect(puts).toEqual([]);
    expect(JSON.parse(localStorage.getItem(LEGACY_SPOILERS_KEY)!)).toBe("all");
  });
});

describe("a choice made after the move", () => {
  it("goes to the account, not the device", async () => {
    await render(<Probe />);
    await act(async () => {
      host.querySelector("button")!.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });
    await settle();
    expect(puts).toEqual([{ spotlight: "recent" }]);
    expect(localStorage.getItem(LEGACY_SPOTLIGHT_KEY)).toBeNull();
    expect(host.textContent).toBe("recent/synopsis");
  });
});
