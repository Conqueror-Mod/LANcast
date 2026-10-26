import { describe, it, expect, vi, afterEach } from "vitest";
import type { WebCores } from "@/api/types";
import {
  WEB_STATE_PREFIX,
  retroarchConfig,
  savePaths,
  webStateCore,
  statePlayer,
  bootWebCore,
  contentName,
  coreURL,
  installedCoreFor,
  webAvailability,
  type CoreModule,
  type ModuleOptions,
} from "./retroWeb";
import { retroAvailability } from "./retro";

/*
 * The browser's game player (ADR 0077 part b), without a browser's
 * WebAssembly. jsdom cannot run a core, so a fake module stands in, and what
 * is proved is the part that can be wrong silently: which core a console
 * gets, what the page says when it cannot play, where the module is fetched
 * from, what is written into its filesystem, and that nothing starts before
 * somebody presses Start. That a real core boots is checked in a browser.
 */

const cores = (installed: boolean): WebCores => ({
  cores: [
    { name: "fceumm", display: "FCEUmm", licence: "GPL-2.0", source_url: "x", consoles: ["nes"], size_bytes: 1, installed },
    { name: "mgba", display: "mGBA", licence: "MPL-2.0", source_url: "x", consoles: ["gb", "gbc", "gba"], size_bytes: 1, installed },
  ],
  archive: { url: "x", size_bytes: 1, version: "1.22.2" },
  job: { running: false, stage: "", done: 0, total: 0 },
});

const desk = { coarse: false, gamepads: 0 };

describe("which consoles play here", () => {
  it("plays a console whose core is installed", () => {
    expect(webAvailability("gba", cores(true), desk)).toMatchObject({ available: true, core: "mgba" });
    expect(installedCoreFor("nes", cores(true))).toBe("fceumm");
  });

  it("says an administrator can fetch the cores when they are not here", () => {
    const a = webAvailability("nes", cores(false), desk);
    expect(a.available).toBe(false);
    expect(a.reason).toContain("Settings → Retro games");
    expect(installedCoreFor("nes", cores(false))).toBeNull();
  });

  // SNES, N64 and PlayStation by decision; the server lists no core for them.
  it("sends a console with no web core to the desktop app", () => {
    for (const p of ["snes", "n64", "ps1"]) {
      expect(webAvailability(p, cores(true), desk).reason).toBe("Plays in the LANcast desktop app.");
    }
  });

  it("does not know an unknown console", () => {
    expect(webAvailability(null, cores(true), desk).available).toBe(false);
  });

  // ADR 0077 Decision 6: no play on a phone until on-screen controls exist,
  // unless a controller is connected.
  it("needs a controller on a touch screen", () => {
    expect(webAvailability("nes", cores(true), { coarse: true, gamepads: 0 }).reason).toBe(
      "Plays with a controller, or in the LANcast desktop app.",
    );
    expect(webAvailability("nes", cores(true), { coarse: true, gamepads: 1 }).available).toBe(true);
  });
});

describe("a game file's name inside the module", () => {
  it("keeps an ordinary name and its extension", () => {
    expect(contentName("Super Mario Bros. (World).nes")).toBe("Super Mario Bros. (World).nes");
  });
  it("drops any folder and anything a path could use", () => {
    expect(contentName("../../etc/passwd")).toBe("passwd");
    expect(contentName("sub\\dir\\Zelda;rm.gba")).toBe("Zelda_rm.gba");
    expect(contentName(".hidden")).toBe("game.hidden");
  });
});

const sramPath = "/home/web_user/retroarch/userdata/saves/Zelda.srm";
const statePath = "/home/web_user/retroarch/userdata/states/Zelda.state";
let stateWrites = 0;

function fakeCore() {
  const files = new Map<string, Uint8Array | string>();
  const dirs: string[] = [];
  const calls: string[][] = [];
  const sent: string[] = [];
  const cmds: string[] = [];
  let opts: ModuleOptions | undefined;
  const module: CoreModule = {
    FS: {
      mkdirTree: (p) => void dirs.push(p),
      writeFile: (p, d) => void files.set(p, d as Uint8Array),
      readFile: (p) => {
        const d = files.get(p);
        if (d === undefined) throw new Error("ENOENT");
        return typeof d === "string" ? new TextEncoder().encode(d) : d;
      },
      readdir: () => [],
    },
    ENV: {},
    callMain: (args) => void calls.push(args),
    EmscriptenSendCommand: (m) => void sent.push(m),
    // RetroArch writing the game's save and a state, as its web build does.
    _cmd_savefiles: () => void files.set(sramPath, new Uint8Array([7, 7, 7])),
    _cmd_save_state: () => {
      setTimeout(() => files.set(statePath, new Uint8Array([9, (stateWrites += 1)])), 20);
    },
    // What RetroArch was given to load, read at the moment it was asked.
    _cmd_load_state: () => void cmds.push(`load:${Array.from(files.get(statePath) as Uint8Array).join(",")}`),
    _cmd_pause: () => void cmds.push("pause"),
    _cmd_unpause: () => void cmds.push("unpause"),
  };
  const imported: string[] = [];
  const importCore = vi.fn(async (url: string) => {
    imported.push(url);
    return {
      default: async (o: ModuleOptions) => {
        opts = o;
        for (const fn of o.preRun) fn(module);
        return module;
      },
    };
  });
  return { files, dirs, calls, sent, cmds, imported, importCore, module, opts: () => opts! };
}

describe("booting a web core", () => {
  const rom = { name: "Zelda.gba", bytes: new Uint8Array([1, 2, 3]) };

  it("loads the core from this server, and its module from the core's route", async () => {
    const f = fakeCore();
    await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    expect(f.imported).toEqual(["/api/retro/web-cores/mgba/js"]);
    expect(f.opts().locateFile("mgba_libretro.wasm", "/api/retro/web-cores/mgba/")).toBe(coreURL("mgba", "wasm"));
    expect(f.opts().noInitialRun).toBe(true);
  });

  it("points RetroArch at its built-in core and writes the game and config", async () => {
    const f = fakeCore();
    await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    const corePath = "/home/web_user/retroarch/cores/mgba_libretro.core";
    expect(f.module.ENV.LIBRARY_PATH).toBe(corePath);
    expect(f.files.has(corePath)).toBe(true);
    expect(f.files.get("/home/web_user/retroarch/userdata/content/Zelda.gba")).toEqual(rom.bytes);
    expect(f.files.get("/home/web_user/retroarch/userdata/retroarch.cfg")).toBe(retroarchConfig(false));
    // RGUI needs no asset bundle; the default menu does.
    expect(retroarchConfig(false)).toContain('menu_driver = "rgui"');
  });

  // A browser keeps a page silent until somebody interacts, so the game
  // starts from the Start press and nothing earlier.
  it("does not start until asked, and starts once", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "fceumm", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    expect(f.calls).toEqual([]);
    b.start();
    b.start();
    expect(f.calls).toEqual([
      ["-v", "/home/web_user/retroarch/userdata/content/Zelda.gba", "-c", "/home/web_user/retroarch/userdata/retroarch.cfg"],
    ]);
    b.send("MENU_TOGGLE");
    expect(f.sent).toEqual(["MENU_TOGGLE"]);
  });
});

describe("retroAvailability outside the desktop app", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("asks the server which web cores are installed", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(cores(true)), { status: 200, headers: { "Content-Type": "application/json" } })),
    );
    vi.stubGlobal("matchMedia", () => ({ matches: false }));
    const a = await retroAvailability("nes");
    expect(a).toMatchObject({ available: true, web: true, core: "fceumm" });
  });

  // Found by an existing test: an answer with no core list threw, the query
  // failed, and the game's page said nothing at all.
  it("treats an answer without a core list as no web cores", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } })),
    );
    const a = await retroAvailability("nes");
    expect(a.reason).toBe("Plays in the LANcast desktop app.");
  });

  // A server from before part a has no such route: no web cores, not a crash.
  it("treats a server that cannot answer as one with no web cores", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", { status: 404 })));
    const a = await retroAvailability("nes");
    expect(a.available).toBe(false);
    expect(a.reason).toBe("Plays in the LANcast desktop app.");
  });
});

/*
 * Saves (ADR 0077 part c). The paths are the ones RetroArch was measured
 * writing on 2026-10-11, with sorting turned off: flat, named after the
 * content file as contentName left it.
 */
describe("saves in the browser", () => {
  const rom = { name: "Zelda.gba", bytes: new Uint8Array([1, 2, 3]) };

  it("names a game's save and states after its cleaned content name", () => {
    const p = savePaths("Fire Emblem (USA, Australia).zip");
    expect(p.sram).toBe("/home/web_user/retroarch/userdata/saves/Fire Emblem (USA_ Australia).srm");
    expect(p.state).toBe("/home/web_user/retroarch/userdata/states/Fire Emblem (USA_ Australia).state");
    expect(p.autoLoad).toBe("/home/web_user/retroarch/userdata/states/Fire Emblem (USA_ Australia).state.auto");
  });

  it("keeps saves flat and auto-loads only when continuing", () => {
    for (const k of ["sort_savefiles_enable", "sort_savestates_enable"]) {
      expect(retroarchConfig(false)).toContain(`${k} = "false"`);
    }
    expect(retroarchConfig(false)).toContain('savestate_auto_load = "false"');
    expect(retroarchConfig(true)).toContain('savestate_auto_load = "true"');
  });

  it("puts the game's save and a state to continue from where RetroArch reads them", async () => {
    const f = fakeCore();
    const sram = new Uint8Array([5, 5]);
    const state = new Uint8Array([6, 6]);
    await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, sram, resume: state, importCore: f.importCore });
    expect(f.files.get(sramPath)).toEqual(sram);
    expect(f.files.get("/home/web_user/retroarch/userdata/states/Zelda.state.auto")).toEqual(state);
    expect(f.files.get("/home/web_user/retroarch/userdata/retroarch.cfg")).toBe(retroarchConfig(true));
  });

  // Play, not Continue: nothing to auto-load, and no auto-load switched on.
  it("starts fresh when not continuing", async () => {
    const f = fakeCore();
    await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    expect(f.files.has("/home/web_user/retroarch/userdata/states/Zelda.state.auto")).toBe(false);
    expect(f.files.get("/home/web_user/retroarch/userdata/retroarch.cfg")).toBe(retroarchConfig(false));
  });

  it("asks RetroArch for the current save, and only once the game is running", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    expect(b.readSRAM()).toBeNull();
    b.start();
    expect(b.readSRAM()).toEqual(new Uint8Array([7, 7, 7]));
  });

  // State saving runs across frames: the call returns at once, the file comes later.
  it("waits for a new state to be written", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    b.start();
    const first = await b.saveState(1000);
    const second = await b.saveState(1000);
    expect(first).not.toBeNull();
    expect(second).not.toBeNull();
    expect(second).not.toEqual(first);
  });

  it("labels a browser state so no desktop core will take it", () => {
    expect(webStateCore("mgba")).toBe("retroarch-web/mgba");
    expect(webStateCore("mgba").startsWith(WEB_STATE_PREFIX)).toBe(true);
  });

  // Numbered states: a slot's bytes go through the slot-0 file RetroArch loads from.
  it("loads a state by writing it where RetroArch reads one, then asking it to load", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    b.start();
    expect(b.loadState(new Uint8Array([4, 2]))).toBe(true);
    expect(f.files.get(statePath)).toEqual(new Uint8Array([4, 2]));
    expect(f.cmds).toEqual(["load:4,2"]);
  });

  it("loads nothing before the game runs, or from an empty state", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    expect(b.loadState(new Uint8Array([4, 2]))).toBe(false);
    b.start();
    expect(b.loadState(new Uint8Array())).toBe(false);
    expect(f.cmds).toEqual([]);
  });

  it("pauses and resumes the game, and only once it is running", async () => {
    const f = fakeCore();
    const b = await bootWebCore({ core: "mgba", canvas: document.createElement("canvas"), rom, importCore: f.importCore });
    b.pause();
    expect(f.cmds).toEqual([]);
    b.start();
    b.pause();
    b.resume();
    expect(f.cmds).toEqual(["pause", "unpause"]);
  });

  it("says which player made a state", () => {
    expect(statePlayer("retroarch-web/fceumm")).toBe("browser");
    expect(statePlayer("mgba")).toBe("desktop");
    expect(statePlayer(undefined)).toBeNull();
    expect(statePlayer("")).toBeNull();
  });
});
