/*
 * How the server runs (ADR 0092).
 *
 * What is worth asserting is who sees the control and what clicking it does:
 * a member must not be offered it, a browser tab must not be offered it, and
 * ticking the box must ask before anything moves — the switch stops the
 * server and copies the whole library, so a click that went straight through
 * would be the most expensive misclick in the app.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import {
  ServerModeSettings,
  confirmText,
  type ServerModeState,
  type ServerModeProgress,
} from "./ServerModeSettings";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

type W = Record<string, unknown>;

function stub(state: Partial<ServerModeState> = {}, progress: ServerModeProgress = { running: false, stage: "" }) {
  const w = window as unknown as W;
  const sw = vi.fn(async () => ({ ok: true }));
  w.lancastServerMode = vi.fn(async () => ({
    mode: "app",
    leftover_service: false,
    addr: ":8080",
    switching: false,
    ...state,
  }));
  w.lancastServerModeProgress = vi.fn(async () => progress);
  w.lancastSwitchServerMode = sw;
  return sw;
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  const w = window as unknown as W;
  delete w.lancastServerMode;
  delete w.lancastServerModeProgress;
  delete w.lancastSwitchServerMode;
});

async function render(isAdmin = true) {
  await act(async () => {
    root.render(<ServerModeSettings isAdmin={isAdmin} />);
  });
  await act(async () => {
    await Promise.resolve();
  });
}

function box(): HTMLInputElement | null {
  return host.querySelector<HTMLInputElement>('input[type="checkbox"]');
}

describe("ServerModeSettings", () => {
  it("is not offered to a member", async () => {
    stub();
    await render(false);
    expect(host.textContent).not.toContain("Run as a Windows service");
  });

  it("is not offered outside the desktop window", async () => {
    await render(true);
    expect(host.textContent).toBe("");
  });

  it("shows the mode the machine is in", async () => {
    stub({ mode: "service" });
    await render();
    expect(box()?.checked).toBe(true);
    expect(host.textContent).toContain("starts with Windows");
  });

  it("asks before switching, and switches only when confirmed", async () => {
    const sw = stub({ mode: "app" });
    await render();

    await act(async () => box()!.click());
    expect(sw).not.toHaveBeenCalled();
    const dialog = host.querySelector('[role="alertdialog"]');
    expect(dialog?.textContent).toContain("Run LANcast as a service?");
    expect(dialog?.textContent).toContain("Windows asks for permission");

    const go = Array.from(host.querySelectorAll("button")).find((b) => b.textContent === "Switch to a service");
    await act(async () => go!.click());
    expect(sw).toHaveBeenCalledWith("service");
  });

  it("cancelling changes nothing", async () => {
    const sw = stub({ mode: "service" });
    await render();
    await act(async () => box()!.click());
    const cancel = Array.from(host.querySelectorAll("button")).find((b) => b.textContent === "Cancel");
    await act(async () => cancel!.click());
    expect(sw).not.toHaveBeenCalled();
    expect(host.querySelector('[role="alertdialog"]')).toBeNull();
    expect(box()?.checked).toBe(true);
  });

  it("says why it cannot switch, and does not let it be ticked", async () => {
    stub({ blocked: "This window is showing a server on another computer." });
    await render();
    expect(box()?.disabled).toBe(true);
    expect(host.textContent).toContain("another computer");
  });

  it("shows a copy's progress", async () => {
    stub({}, {
      running: true,
      stage: "elevating",
      progress: { phase: "copying", done_bytes: 512 * 1048576, total_bytes: 1024 * 1048576 },
    });
    await render();
    expect(host.textContent).toContain("Copying your library");
    expect(host.textContent).toContain("512 MB of 1.0 GB");
    expect(box()?.disabled).toBe(true);
  });

  it("reports a failed switch as the old way still standing", async () => {
    stub({}, {
      running: false,
      stage: "",
      outcome: { ok: false, to: "service", error: "Permission was not given, so nothing was changed." },
    });
    await render();
    expect(host.textContent).toContain("LANcast runs the way it did before");
    expect(host.textContent).toContain("Permission was not given");
  });

  it("says where the old copy is after a switch", async () => {
    stub({ mode: "service" }, {
      running: false,
      stage: "",
      outcome: {
        ok: true,
        to: "service",
        result: {
          to: "service",
          library: "C:\\ProgramData\\LANcast",
          old_folder: "C:\\Users\\A123456\\AppData\\Roaming\\LANcast",
          old_bytes: 10 * 1073741824,
        },
      },
    });
    await render();
    expect(host.textContent).toContain("LANcast now runs as a service");
    expect(host.textContent).toContain("about 10.0 GB");
  });

  it("offers to finish an interrupted switch", async () => {
    const sw = stub({ mode: "app", leftover_service: true });
    await render();
    const remove = Array.from(host.querySelectorAll("button")).find((b) => b.textContent === "Remove it");
    await act(async () => remove!.click());
    expect(sw).toHaveBeenCalledWith("app");
  });
});

describe("confirmText", () => {
  it("warns that a moved port does not survive the next start", () => {
    expect(confirmText("app", ":9090")).toContain("port 9090");
    expect(confirmText("app", ":8080")).not.toContain("port");
    expect(confirmText("service", ":9090")).not.toContain("port 9090");
  });
});
