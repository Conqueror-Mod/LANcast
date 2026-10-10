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
  filmChannels: 0,
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
    filmChannels: 0,
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

  it("offers neither in a browser tab before it knows what the film carries", () => {
    pb.native = false;
    pb.audioFX = false;
    render();
    expect(labels()).not.toContain("Night mode");
    expect(labels()).not.toContain("Dialogue boost");
  });

  /*
   * A film in a browser tab: night mode on Web Audio when the element receives
   * mono or stereo, which every converted soundtrack is. Dialogue boost stays
   * the desktop's, since the element has no centre channel to find.
   */
  it("offers night mode, and not dialogue boost, on a stereo film in a browser tab", () => {
    Object.assign(pb, { native: false, audioFX: false, filmChannels: 2 });
    render();
    expect(labels()).toContain("Night mode");
    expect(labels()).not.toContain("Dialogue boost");
    act(() => {
      const sel = selectFor("Night mode");
      sel.value = "on";
      sel.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ nightVideo: true });
  });

  // Surround is not folded to stereo without saying so.
  it("says why there is no night mode on a surround film in a browser tab", () => {
    Object.assign(pb, { native: false, audioFX: false, filmChannels: 6 });
    render();
    expect(labels()).not.toContain("Night mode");
    expect(host.textContent).toContain("surround");
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

/*
 * The equaliser (audio-pass-plan.md Phase 3): music only, a preset that sets
 * all five bands, and a band that once moved makes the setting Custom.
 */
describe("PlaybackSettings equaliser", () => {
  beforeEach(() => {
    Object.assign(pb, { isAudio: true, native: false, audioFX: false, musicChannels: 2 });
    vi.stubGlobal("AudioContext", class {});
  });
  afterEach(() => vi.unstubAllGlobals());

  const slider = (label: string) =>
    host.querySelector(`input[aria-label="Equaliser ${label}"]`) as HTMLInputElement;

  it("offers a preset and five bands for music, and nothing for a film", () => {
    render();
    expect(labels()).toEqual(expect.arrayContaining(["Equaliser", "60 Hz", "230 Hz", "910 Hz", "3.6 kHz", "14 kHz"]));
    act(() => root.unmount());
    root = createRoot(host);
    pb.isAudio = false;
    render();
    expect(labels()).not.toContain("Equaliser");
  });

  it("sets all five bands from a preset", () => {
    render();
    const sel = selectFor("Equaliser");
    act(() => {
      sel.value = "bass";
      sel.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ eqPreset: "bass", eq: [6, 3, 0, 0, 0] });
  });

  it("makes it Custom when one band moves, keeping the others", () => {
    pb.prefs = { ...DEFAULTS, eq: [6, 3, 0, 0, 0], eqPreset: "bass" };
    render();
    const s = slider("14 kHz");
    act(() => {
      const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
      set.call(s, "4");
      s.dispatchEvent(new Event("input", { bubbles: true }));
    });
    expect(pb.setPrefs).toHaveBeenLastCalledWith({ eq: [6, 3, 0, 0, 4], eqPreset: "custom" });
  });

  it("shows each band's level with its sign", () => {
    pb.prefs = { ...DEFAULTS, eq: [6, -3, 0, 0, 0], eqPreset: "custom" };
    render();
    const values = [...host.querySelectorAll(".pbset__value")].map((v) => v.textContent);
    expect(values).toEqual(expect.arrayContaining(["+6 dB", "-3 dB", "0 dB"]));
    expect(selectFor("Equaliser").value).toBe("custom");
  });
});
