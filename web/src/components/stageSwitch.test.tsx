/*
 * The stage switcher under Now Playing's stage (NowPlaying.tsx).
 *
 * On a phone it was wider than the stage and sat over it (v0.9.92). The fix is
 * CSS, which jsdom cannot lay out, so what these hold is the structure the CSS
 * depends on: the choices in their own group, which may scroll sideways on a
 * narrow screen, with the full-screen button after it and outside it, so it
 * stays in reach; the preset's stepper after the row; and the column told
 * which rows there are, since it starts under them from another layer.
 *
 * And the failure the MilkDrop stage can now report: said in words, with the
 * cover offered back.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { getPrefs, setPrefs } from "@/playback/prefs";
import { resetMilkdropPickedForTests, setMilkdropStatus } from "@/playback/milkdropStage";
import { setWebGL2ForTests } from "@/playback/trails";

const pb = { setVizFull: vi.fn() };
vi.mock("@/playback/PlaybackProvider", () => ({ usePlayback: () => pb }));

import { StageSwitch, useStageRows, VizStep } from "./NowPlaying";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const summary = (id: string, name: string) => ({
  id,
  name,
  source: "milk",
  shaders: "none",
  size_bytes: 100,
  uploaded_at: 1_760_000_000,
});

let host: HTMLDivElement;
let root: Root;
let rows: ReturnType<typeof useStageRows>;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  pb.setVizFull.mockReset();
  setWebGL2ForTests(true);
  resetMilkdropPickedForTests();
  setPrefs({ stage: "cover" });
  // Web Audio is what the switcher is offered on.
  vi.stubGlobal("AudioContext", function AudioContext() {});
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      new Response(JSON.stringify({ presets: [summary("p1", "Aurora"), summary("p2", "Bloom")] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  setWebGL2ForTests(undefined);
  resetMilkdropPickedForTests();
});

function Rows() {
  rows = useStageRows();
  return null;
}

async function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
  });
  for (let i = 0; i < 5; i++) await act(async () => await new Promise((r) => setTimeout(r, 0)));
}

describe("the stage switcher", () => {
  it("keeps the choices in their own group, then the full-screen button, then the stepper", async () => {
    setPrefs({ stage: "milkdrop" });
    await mount(<StageSwitch />);
    const stage = host.querySelector(".np__stage")!;
    const row = stage.querySelector(".np__stage-row")!;
    const group = row.querySelector('[role="radiogroup"]')!;
    const full = row.querySelector(".np__stage-full")!;

    expect([...group.querySelectorAll("button")].map((b) => b.textContent)).toEqual([
      "Cover",
      "Spectrum",
      "Scope",
      "Trails",
      "MilkDrop",
    ]);
    // Outside the group that may scroll, and after it: always in reach.
    expect(group.contains(full)).toBe(false);
    expect(row.lastElementChild).toBe(full);
    expect(full.getAttribute("aria-label")).toBe("Full-screen visualizer");
    // The stepper comes under the row, not before it.
    expect([...stage.children].map((c) => c.className)).toEqual(["np__stage-row", "np__step"]);
  });

  it("reaches every control from the keyboard, in reading order", async () => {
    setPrefs({ stage: "tunnel" });
    await mount(<StageSwitch />);
    const order = [...host.querySelectorAll("button")].map((b) => b.getAttribute("aria-label") ?? b.textContent);
    expect(order).toEqual([
      "Cover",
      "Spectrum",
      "Scope",
      "Trails",
      "MilkDrop",
      "Full-screen visualizer",
      "Previous visualizer",
      "Next visualizer",
    ]);
    for (const b of host.querySelectorAll("button")) {
      expect(b.hasAttribute("disabled")).toBe(false);
      expect(b.tabIndex).toBe(0);
    }
  });

  it("opens the full-screen visualizer from the button outside the choices", async () => {
    await mount(<StageSwitch />);
    act(() => (host.querySelector(".np__stage-full") as HTMLButtonElement).click());
    expect(pb.setVizFull).toHaveBeenCalledWith(true);
  });

  it("tells the column how many rows to start under", async () => {
    await mount(<Rows />);
    expect(rows).toMatchObject({ fx: true, step: false });
    act(() => setPrefs({ stage: "tunnel" }));
    expect(rows.step).toBe(true);
    act(() => setPrefs({ stage: "spectrum" }));
    expect(rows.step).toBe(false);
    act(() => setPrefs({ stage: "milkdrop" }));
    expect(rows.step).toBe(true);
  });

  it("reserves no rows where there is no switcher", async () => {
    vi.stubGlobal("AudioContext", undefined);
    setPrefs({ stage: "tunnel" });
    await mount(<Rows />);
    expect(rows).toMatchObject({ fx: false, step: false });
  });
});

describe("MilkDrop's state under the stage", () => {
  it("says it is loading beside the preset's name", async () => {
    setPrefs({ stage: "milkdrop", milkdrop: "p1" });
    await mount(<VizStep only={3} />);
    act(() => setMilkdropStatus("loading"));
    expect(host.querySelector(".np__step-name")?.textContent).toBe("Aurora · loading");
    act(() => setMilkdropStatus("ready"));
    expect(host.querySelector(".np__step-name")?.textContent).toBe("Aurora");
  });

  it("says a failure in words and offers the cover back", async () => {
    setPrefs({ stage: "milkdrop", milkdrop: "p1" });
    await mount(<VizStep only={3} />);
    act(() => setMilkdropStatus("failed"));
    expect(host.querySelector('[role="status"]')?.textContent).toBe("MilkDrop could not load");
    const back = [...host.querySelectorAll("button")].find((b) => b.textContent === "Show the cover")!;
    expect(back).toBeTruthy();
    act(() => back.click());
    expect(getPrefs().stage).toBe("cover");
    // From the full-screen visualizer too, back to where the cover is.
    expect(pb.setVizFull).toHaveBeenCalledWith(false);
  });
});
