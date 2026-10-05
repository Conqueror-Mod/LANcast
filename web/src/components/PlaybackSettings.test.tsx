/*
 * The sound rows of the playback panel (docs/audio-pass-plan.md, Phase 1).
 *
 * The panel's rule is that a row is absent rather than present and inert, so
 * what is asserted here is *where the rows appear*: only on the desktop's own
 * player, which is the only engine with the filters, and dialogue boost never
 * on a mono track. jsdom performs no layout, so nothing here says the rows are
 * on screen — only that they exist when they should and write what they say.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { DEFAULTS, type Prefs } from "@/playback/prefs";
import type { MediaStream } from "@/api/types";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const pb = {
  native: true,
  audioFX: true,
  isAudio: false,
  itemID: 1,
  item: undefined,
  audioTracks: [] as MediaStream[],
  audioIndex: null as number | null,
  musicChannels: 0,
  subtitles: [],
  activeSub: null,
  subKey: null,
  speed: 1,
  prefs: { ...DEFAULTS } as Prefs,
  setPrefs: vi.fn(),
  selectAudio: vi.fn(),
  selectSub: vi.fn(),
  setSpeed: vi.fn(),
};

vi.mock("@/playback/PlaybackProvider", () => ({ usePlayback: () => pb }));

const { PlaybackSettings } = await import("./PlaybackSettings");

let host: HTMLDivElement;
let root: Root;

function render() {
  act(() => root.render(<PlaybackSettings onClose={() => {}} />));
}

const labels = () =>
  [...host.querySelectorAll(".pbset__label")].map((l) => l.textContent);
const selectFor = (label: string) =>
  [...host.querySelectorAll(".pbset__row")]
    .find((r) => r.querySelector(".pbset__label")?.textContent === label)
    ?.querySelector("select") as HTMLSelectElement;
const track = (channels: number): MediaStream =>
  ({ index: 1, kind: "audio", channels, default: true }) as MediaStream;

beforeEach(() => {
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  Object.assign(pb, {
    native: true,
    audioFX: true,
    isAudio: false,
    audioTracks: [track(6)],
    musicChannels: 0,
    prefs: { ...DEFAULTS },
  });
  pb.setPrefs.mockClear();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("PlaybackSettings sound rows", () => {
  it("offers night mode and dialogue boost on the desktop's own player", () => {
    render();
    expect(labels()).toContain("Night mode");
    expect(labels()).toContain("Dialogue boost");
  });

  it("offers neither where the element plays, which has no filters yet", () => {
    pb.native = false;
    pb.audioFX = false;
    render();
    expect(labels()).not.toContain("Night mode");
    expect(labels()).not.toContain("Dialogue boost");
  });

  it("offers neither on a native client too old to apply them", () => {
    // v0.9.44's client against a newer server: mpv plays, but the commands
    // are refused. The rows showed, did nothing, and an evening of listening
    // tested nothing.
    pb.native = true;
    pb.audioFX = false;
    render();
    expect(labels()).not.toContain("Night mode");
    expect(labels()).not.toContain("Dialogue boost");
  });

  it("offers neither for music, which never plays through mpv", () => {
    pb.isAudio = true;
    render();
    expect(labels()).not.toContain("Night mode");
  });

  it("keeps night mode and drops dialogue boost on a mono track", () => {
    pb.audioTracks = [track(1)];
    render();
    expect(labels()).toContain("Night mode");
    expect(labels()).not.toContain("Dialogue boost");
  });

  it("writes what it says", () => {
    render();
    const dialogue = selectFor("Dialogue boost");
    act(() => {
      dialogue.value = "2";
      dialogue.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ dialogueVideo: 2 });

    const night = selectFor("Night mode");
    act(() => {
      night.value = "on";
      night.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ nightVideo: true });
  });

  it("says a boosted film gets quieter, and only while boosting", () => {
    render();
    expect(host.textContent).not.toMatch(/turns everything else down/);
    pb.prefs = { ...DEFAULTS, dialogueVideo: 1 };
    render();
    expect(host.textContent).toMatch(/turns everything else down/);
    // Night mode lifts quiet speech; it must not borrow the boost's warning.
    pb.prefs = { ...DEFAULTS, nightVideo: true };
    render();
    expect(host.textContent).not.toMatch(/turns everything else down/);
  });
});

/*
 * Music's night mode (Phase 2). Web Audio on the element, so not tied to the
 * desktop's player: offered wherever music plays, once the probe has said how
 * many channels the track has. Vocals was removed after the listening test.
 */
describe("PlaybackSettings music rows", () => {
  beforeEach(() => {
    Object.assign(pb, { isAudio: true, native: false, audioFX: false, musicChannels: 2 });
  });

  it("offers night mode on stereo and mono, wherever music plays", () => {
    for (const n of [1, 2]) {
      pb.musicChannels = n;
      render();
      expect(labels()).toContain("Night mode");
      expect(labels()).not.toContain("Dialogue boost");
    }
  });

  it("offers no vocals control: it made no audible difference", () => {
    render();
    expect(labels()).not.toContain("Vocals");
  });

  it("offers nothing while the channel count is unknown, or on surround", () => {
    for (const n of [0, 6]) {
      pb.musicChannels = n;
      render();
      expect(labels()).not.toContain("Night mode");
    }
  });

  it("writes music's own preference, never the film's", () => {
    render();
    const night = selectFor("Night mode");
    act(() => {
      night.value = "on";
      night.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ nightMusic: true });
  });
});
