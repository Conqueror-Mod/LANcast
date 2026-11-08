import { describe, it, expect, beforeEach, vi } from "vitest";
import type { Item, RemoteCommand, RemotePlayer } from "@/api/types";
import {
  canSend,
  defaultName,
  deviceKind,
  enabledByDefault,
  getRemoteDevice,
  otherPlayers,
  playerPosition,
  resetRemoteDeviceForTests,
  runCommand,
  setRemoteDevice,
  type Playable,
} from "./remote";

/*
 * Play on another of your own devices (ADR 0117): what a command does to the
 * player, which devices offer themselves by default, and what can be sent.
 */

const CHROME_WIN =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36";
const ANDROID =
  "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36";
const IPHONE =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

function fakePlayer(over: Partial<Playable> = {}): Playable & Record<string, ReturnType<typeof vi.fn>> {
  return {
    itemID: 7,
    playing: true,
    stop: vi.fn(),
    togglePlay: vi.fn(),
    seekTo: vi.fn(),
    playNext: vi.fn(),
    playPrev: vi.fn(),
    changeVolume: vi.fn(),
    ...over,
  } as Playable & Record<string, ReturnType<typeof vi.fn>>;
}

function cmd(c: Partial<RemoteCommand>): RemoteCommand {
  return { action: "pause", sent_at: 0, ...c } as RemoteCommand;
}

describe("runCommand", () => {
  it("opens the player for a play, with the queue it was sent", () => {
    const pb = fakePlayer();
    expect(runCommand(cmd({ action: "play", item_id: 12, queue: [12, 13] }), pb)).toEqual({
      kind: "open",
      itemID: 12,
      queue: [12, 13],
      fromStart: false,
      seekMS: null,
    });
  });

  it("a play with no queue queues the item alone", () => {
    const e = runCommand(cmd({ action: "play", item_id: 12 }), fakePlayer());
    expect(e.kind === "open" && e.queue).toEqual([12]);
  });

  /*
   * "Move here" sends the exact moment. It is not "from the start": that asks
   * the server for a pre-roll, and a film moved at minute forty would open on
   * a trailer.
   */
  it("a play at a position carries it, without claiming to start from the top", () => {
    const e = runCommand(cmd({ action: "play", item_id: 12, position_ms: 90_000 }), fakePlayer());
    expect(e).toMatchObject({ kind: "open", seekMS: 90_000, fromStart: false });
  });

  it("a play with no item is ignored", () => {
    expect(runCommand(cmd({ action: "play" }), fakePlayer()).kind).toBe("ignored");
  });

  it("pause pauses only what is playing", () => {
    const playing = fakePlayer();
    runCommand(cmd({ action: "pause" }), playing);
    expect(playing.togglePlay).toHaveBeenCalledTimes(1);

    // Already paused: pausing again must not toggle it back on.
    const paused = fakePlayer({ playing: false });
    runCommand(cmd({ action: "pause" }), paused);
    expect(paused.togglePlay).not.toHaveBeenCalled();
  });

  it("resume plays only what is paused", () => {
    const paused = fakePlayer({ playing: false });
    runCommand(cmd({ action: "resume" }), paused);
    expect(paused.togglePlay).toHaveBeenCalledTimes(1);

    const playing = fakePlayer();
    runCommand(cmd({ action: "resume" }), playing);
    expect(playing.togglePlay).not.toHaveBeenCalled();
  });

  it("seek goes to the position in seconds", () => {
    const pb = fakePlayer();
    runCommand(cmd({ action: "seek", position_ms: 61_500 }), pb);
    expect(pb.seekTo).toHaveBeenCalledWith(61.5);
  });

  it("nothing playing: seek, stop, next and previous do nothing", () => {
    const pb = fakePlayer({ itemID: 0, playing: false });
    expect(runCommand(cmd({ action: "seek", position_ms: 1 }), pb).kind).toBe("ignored");
    for (const action of ["stop", "next", "previous", "pause", "resume"] as const)
      runCommand(cmd({ action }), pb);
    expect(pb.seekTo).not.toHaveBeenCalled();
    expect(pb.stop).not.toHaveBeenCalled();
    expect(pb.playNext).not.toHaveBeenCalled();
    expect(pb.playPrev).not.toHaveBeenCalled();
    expect(pb.togglePlay).not.toHaveBeenCalled();
  });

  it("stop, next, previous and volume reach the player", () => {
    const pb = fakePlayer();
    runCommand(cmd({ action: "stop" }), pb);
    runCommand(cmd({ action: "next" }), pb);
    runCommand(cmd({ action: "previous" }), pb);
    runCommand(cmd({ action: "volume", volume: 0.4 }), pb);
    expect(pb.stop).toHaveBeenCalled();
    expect(pb.playNext).toHaveBeenCalled();
    expect(pb.playPrev).toHaveBeenCalled();
    expect(pb.changeVolume).toHaveBeenCalledWith(0.4);
  });

  it("an action from a newer server is ignored, not thrown", () => {
    expect(runCommand(cmd({ action: "explode" as RemoteCommand["action"] }), fakePlayer()).kind).toBe(
      "ignored",
    );
  });
});

describe("which devices offer themselves", () => {
  it("the desktop window does, a browser and a phone do not", () => {
    expect(deviceKind(CHROME_WIN, true)).toBe("desktop");
    expect(deviceKind(CHROME_WIN, false)).toBe("browser");
    expect(deviceKind(ANDROID, false)).toBe("phone");
    expect(deviceKind(IPHONE, false)).toBe("phone");
    expect(enabledByDefault("desktop")).toBe(true);
    expect(enabledByDefault("browser")).toBe(false);
    expect(enabledByDefault("phone")).toBe(false);
  });

  it("names a device by what it can tell", () => {
    expect(defaultName(CHROME_WIN, true)).toBe("LANcast app");
    expect(defaultName(CHROME_WIN, false)).toBe("Chrome on Windows");
    expect(defaultName(ANDROID, false)).toBe("Android phone");
    expect(defaultName(IPHONE, false)).toBe("iPhone");
  });
});

describe("this device's identity", () => {
  beforeEach(() => {
    localStorage.clear();
    resetRemoteDeviceForTests();
  });

  it("keeps the same id across reloads", () => {
    const first = getRemoteDevice();
    expect(first.id).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    resetRemoteDeviceForTests();
    expect(getRemoteDevice().id).toBe(first.id);
  });

  it("remembers a name and a choice", () => {
    setRemoteDevice({ name: "Den TV", enabled: true });
    resetRemoteDeviceForTests();
    expect(getRemoteDevice()).toMatchObject({ name: "Den TV", enabled: true });
  });

  it("replaces a stored id the server would refuse", () => {
    localStorage.setItem("lancast:remote-device", JSON.stringify({ id: "bad id!" }));
    expect(getRemoteDevice().id).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
  });
});

describe("what can be sent", () => {
  const item = (kind: string, child_count = 0) => ({ id: 1, kind, child_count }) as unknown as Item;

  it("a film, an episode and a track can", () => {
    for (const k of ["movie", "episode", "track", "featurette"]) expect(canSend(item(k)), k).toBe(true);
  });

  it("a container, a book, a game, a comic and a photo cannot", () => {
    for (const k of ["show", "season", "album", "artist", "collection", "book", "book_part", "rom", "comic", "photo"])
      expect(canSend(item(k)), k).toBe(false);
    // A multi-part film is a movie with children.
    expect(canSend(item("movie", 2))).toBe(false);
  });
});

describe("a remote's view of a player", () => {
  const p = (over: Partial<RemotePlayer>): RemotePlayer =>
    ({
      id: "tv-0123456789",
      name: "TV",
      kind: "desktop",
      item_id: 4,
      position_ms: 10_000,
      duration_ms: 60_000,
      paused: false,
      volume: 1,
      age_ms: 1_000,
      pending: 0,
      ...over,
    }) as RemotePlayer;

  it("moves a playing position on by its age and the time since it arrived", () => {
    expect(playerPosition(p({}), 5_000, 7_000)).toBe(13_000);
  });

  it("holds a paused position still", () => {
    expect(playerPosition(p({ paused: true }), 5_000, 50_000)).toBe(10_000);
  });

  it("never runs past the end", () => {
    expect(playerPosition(p({}), 0, 1_000_000)).toBe(60_000);
  });

  it("leaves this window out of the list", () => {
    expect(otherPlayers([p({ id: "self-01234567" }), p({ id: "tv-0123456789" })], "self-01234567").map((x) => x.id)).toEqual([
      "tv-0123456789",
    ]);
  });
});
