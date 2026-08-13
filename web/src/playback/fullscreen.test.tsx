/*
 * What goes fullscreen.
 *
 * Reported as "the player breaks and all the buttons disappear": pressing
 * fullscreen showed the video and nothing else. It was not a fault — fullscreen
 * was doing exactly what it was told. The media surface holds the media element
 * and nothing else; the player's controls are a *sibling* of it, in the screen
 * the provider renders as children. Fullscreening the surface therefore hides
 * every control by construction, and keeps the mousemove that would wake them
 * outside the fullscreened subtree, so they cannot come back either.
 *
 * This asserts the target rather than the appearance, because the appearance is
 * what jsdom cannot tell you and the target is the whole decision.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { PlaybackProvider, usePlayback } from "./PlaybackProvider";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let requested: Element[];
let exited: number;

function Harness() {
  const pb = usePlayback();
  return (
    <button id="fs" onClick={pb.toggleFullscreen}>
      fullscreen
    </button>
  );
}

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  requested = [];
  exited = 0;

  Element.prototype.requestFullscreen = function () {
    requested.push(this);
    return Promise.resolve();
  };
  document.exitFullscreen = () => {
    exited++;
    return Promise.resolve();
  };
  Object.defineProperty(document, "fullscreenElement", {
    configurable: true,
    get: () => null,
  });
  vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 200 })));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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

describe("fullscreen", () => {
  it("takes the document, so the controls come with the picture", () => {
    render();
    act(() => {
      host.querySelector<HTMLButtonElement>("#fs")!.click();
    });

    expect(requested).toHaveLength(1);
    expect(requested[0]).toBe(document.documentElement);

    // The specific mistake: the media surface holds no controls, so
    // fullscreening it is a picture with nothing to press.
    const surface = document.querySelector(".playback");
    expect(surface).not.toBeNull();
    expect(requested[0]).not.toBe(surface);
  });

  it("exits when something is already fullscreen", () => {
    Object.defineProperty(document, "fullscreenElement", {
      configurable: true,
      get: () => document.documentElement,
    });
    render();
    act(() => {
      host.querySelector<HTMLButtonElement>("#fs")!.click();
    });
    expect(exited).toBe(1);
    expect(requested).toHaveLength(0);
  });
});
