/*
 * Subtitle appearance reaches the pop-out window.
 *
 * The preferences existed, the `::cue` rules read them, and the pop-out showed
 * subtitles at the shipped defaults anyway. Every part was right about its own
 * half: the properties were written to `document.documentElement` — the page's
 * root — and the pop-out (ADR 0029) is a second document with a root of its
 * own. `copyStyles` carries the stylesheets across, and an inline style on an
 * element is not a stylesheet, so every `var(--cue-…, fallback)` in the copied
 * rules resolved to its fallback.
 *
 * cueVars.test.ts covers what the values are. This covers whether they arrive,
 * which is the half that was broken and the half that had no test — the same
 * shape as the close handler this project installed a line too late, and the
 * developer-tools switch that opened a pane it had already disabled.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";
import { usePrefs } from "./prefs";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let pipDoc: Document;

/*
 * jsdom has no documentPictureInPicture and it cannot be polyfilled honestly,
 * so this stands in for it — a real second document from
 * `implementation.createHTMLDocument`, which is what matters here: a separate
 * root, with none of the page's inline styles on it.
 */
function fakePiP() {
  pipDoc = document.implementation.createHTMLDocument("popout");
  const win = {
    document: pipDoc,
    close: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    innerWidth: 640,
    innerHeight: 360,
  } as unknown as Window;
  return {
    requestWindow: async () => win,
    window: null,
  };
}

function Harness() {
  const pb = usePlayback();
  const [, setPrefs] = usePrefs();
  return (
    <>
      <button id="pop" onClick={pb.togglePopout}>
        pop out
      </button>
      <button
        id="yellow"
        onClick={() => setPrefs({ subColor: "#ffe066", subFont: "mono" })}
      >
        yellow
      </button>
    </>
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 200 })));
  (
    window as unknown as { documentPictureInPicture: unknown }
  ).documentPictureInPicture = fakePiP();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  delete (window as unknown as { documentPictureInPicture?: unknown })
    .documentPictureInPicture;
  localStorage.clear();
});

function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  act(() => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <PlaybackProvider>
            <Harness />
          </PlaybackProvider>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
}

function click(id: string) {
  act(() => {
    host.querySelector<HTMLButtonElement>(`#${id}`)!.click();
  });
}

describe("subtitle appearance in the pop-out window", () => {
  it("writes the properties onto the page's own root", () => {
    render();
    expect(
      document.documentElement.style.getPropertyValue("--cue-color"),
    ).not.toBe("");
  });

  /*
   * The fault. Opening the pop-out has to apply them, not only changing a
   * preference while it is already open — which is why the effect depends on
   * the window as well as on the preferences.
   */
  it("applies them to the pop-out when it opens", async () => {
    render();
    click("yellow");
    click("pop");
    await act(async () => {
      await Promise.resolve();
    });

    expect(
      pipDoc.documentElement.style.getPropertyValue("--cue-color"),
      "the pop-out renders subtitles at the shipped defaults whatever was " +
        "chosen, because the properties only ever reached the page",
    ).toBe("#ffe066");
    expect(
      pipDoc.documentElement.style.getPropertyValue("--cue-font"),
    ).toContain("monospace");
  });

  // And a change made while it is already open follows it across, rather than
  // taking effect in the tab nobody is looking at.
  it("follows a change made while the pop-out is open", async () => {
    render();
    click("pop");
    await act(async () => {
      await Promise.resolve();
    });
    click("yellow");

    expect(
      pipDoc.documentElement.style.getPropertyValue("--cue-color"),
    ).toBe("#ffe066");
  });
});
