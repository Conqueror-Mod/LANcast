/*
 * The language controls on the Account pane.
 *
 * jsdom sees no layout, so this is about wiring — which is where this control's
 * risk sits. The server takes all three fields together, because a subtitle
 * language with no mode is silently inert: subtitles simply never appear, and
 * somebody reports it as the setting not working.
 *
 * So the assertions are mostly about *the request*, in the same spirit as
 * libraryQueue.test.ts: a perfectly good PUT that omits a field is still a
 * setting that does nothing.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LanguagePreferences } from "./LanguagePreferences";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; body: string }[];

function mount(initial: {
  preferred_audio_lang?: string;
  preferred_subtitle_lang?: string;
  subtitle_mode?: string;
}) {
  sent = [];
  const state = {
    preferred_audio_lang: initial.preferred_audio_lang ?? "",
    preferred_subtitle_lang: initial.preferred_subtitle_lang ?? "",
    subtitle_mode: initial.subtitle_mode ?? "off",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      if (method !== "GET") {
        sent.push({ url: String(url), body: String(init?.body ?? "") });
        Object.assign(state, JSON.parse(String(init?.body ?? "{}")));
      }
      return new Response(JSON.stringify(state), {
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

async function settle() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function render() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <LanguagePreferences />
      </QueryClientProvider>,
    );
  });
  await settle();
}

const selects = () => [...host.querySelectorAll("select")];
const byLabel = (text: string) =>
  selects().find((s) => s.parentElement?.textContent?.startsWith(text));
const text = () => host.textContent ?? "";

async function choose(el: HTMLSelectElement, value: string) {
  el.value = value;
  await act(async () => {
    el.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await settle();
}

describe("the language controls", () => {
  it("shows no preference until one is set", async () => {
    mount({});
    await render();
    expect(byLabel("Preferred audio")?.value).toBe("");
  });

  it("sends all three fields when only one changes", async () => {
    /*
     * The fault this file exists for. The server takes them together, and a
     * request naming only the field that moved would clear the other two — or,
     * worse, be accepted and leave a mode with no language, which shows no
     * subtitles and looks like the feature being broken.
     */
    mount({ subtitle_mode: "always", preferred_subtitle_lang: "en" });
    await render();

    await choose(byLabel("Preferred audio")!, "ja");

    expect(sent.length).toBe(1);
    const body = JSON.parse(sent[0].body);
    expect(body).toEqual({
      preferred_audio_lang: "ja",
      preferred_subtitle_lang: "en",
      subtitle_mode: "always",
    });
  });

  it("hides the subtitle language while subtitles are off", async () => {
    // A control that cannot affect anything is worse than no control: it
    // invites somebody to set it and wonder why nothing happens.
    mount({ subtitle_mode: "off" });
    await render();
    expect(byLabel("Subtitle language")).toBeUndefined();
  });

  it("reveals the subtitle language once a mode is chosen", async () => {
    mount({ subtitle_mode: "off" });
    await render();

    await choose(byLabel("Subtitles")!, "foreign");
    expect(byLabel("Subtitle language")).toBeTruthy();
  });

  it("explains that foreign is judged on what plays", async () => {
    /*
     * The one sentence worth pinning, because the other reading of "foreign" —
     * "this is a foreign film" — is what people expect, and it is the version
     * that puts subtitles over an English dub they chose deliberately.
     */
    mount({ subtitle_mode: "foreign", preferred_subtitle_lang: "en" });
    await render();
    expect(text()).toContain("audio that ends up playing");
  });

  it("offers a way back to no preference", async () => {
    // Empty is a real answer, not a missing one. Without it the choice is
    // one-way.
    mount({ preferred_audio_lang: "ja" });
    await render();

    await choose(byLabel("Preferred audio")!, "");
    expect(JSON.parse(sent[0].body).preferred_audio_lang).toBe("");
  });
});
