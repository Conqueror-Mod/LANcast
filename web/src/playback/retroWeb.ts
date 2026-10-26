import type { WebCores } from "@/api/types";
import type { RetroAvailability } from "./retro";

/*
 * Playing a retro game in a browser
 * ([ADR 0077](../../../docs/adr/0077-a-browser-can-play-a-game.md) part b).
 *
 * The desktop app has its own player and never uses this (ADR 0077 Decision
 * 7). Everywhere else, a game plays here when the server has fetched its
 * console's web core (part a).
 *
 * # What a web core is, and what that leaves for this file
 *
 * Each web core is all of RetroArch compiled to WebAssembly with one core
 * built in. RetroArch already draws through WebGL, plays sound through Web
 * Audio, and reads the keyboard and the Gamepad API itself. So this is not an
 * emulator host. It loads the module from this server, hands it the game's
 * bytes and a small configuration, and starts it, the way libretro's own web
 * player does:
 *
 *   1. import the core's loader; its default export is a factory;
 *   2. call it with a canvas, `noInitialRun`, and a preRun that points
 *      RetroArch at the core built into it;
 *   3. write the game and the config into its in-memory filesystem;
 *   4. callMain with the game's path.
 *
 * Saves are part c. Until then a game's in-game save lasts as long as the tab.
 */

/** RetroArch's home inside the module's filesystem. */
const HOME = "/home/web_user/retroarch";
const CONFIG = `${HOME}/userdata/retroarch.cfg`;
const CONTENT = `${HOME}/userdata/content`;
const SAVES = `${HOME}/userdata/saves`;
const STATES = `${HOME}/userdata/states`;

/*
 * The configuration a game starts with.
 *
 * RGUI because it is drawn by RetroArch itself: the default menu needs an
 * asset bundle LANcast does not fetch. Not pausing when the tab loses focus,
 * because a person reaching for a controller should not find the game
 * frozen behind a notification.
 *
 * Saves flat in two folders, not sorted into one per core, so savePaths can
 * name them (see there). Auto-load only when continuing, so Play starts the
 * game fresh. RetroArch's own autosave writes the game's save every few
 * seconds; the page uploads it when it changes.
 */
export function retroarchConfig(resume: boolean): string {
  return [
    'menu_driver = "rgui"',
    'pause_nonactive = "false"',
    'video_font_enable = "false"',
    `savefile_directory = "${SAVES}"`,
    `savestate_directory = "${STATES}"`,
    'sort_savefiles_enable = "false"',
    'sort_savestates_enable = "false"',
    'sort_savefiles_by_content_enable = "false"',
    'sort_savestates_by_content_enable = "false"',
    'autosave_interval = "10"',
    `savestate_auto_load = "${resume ? "true" : "false"}"`,
    'savestate_auto_save = "false"',
  ].join("\n");
}

/** The part of an Emscripten module this file uses. */
export interface CoreModule {
  FS: {
    mkdirTree(path: string): void;
    writeFile(path: string, data: Uint8Array | string): void;
    readFile(path: string): Uint8Array;
    readdir(path: string): string[];
  };
  ENV: Record<string, string>;
  callMain(args: string[]): void;
  EmscriptenSendCommand?: (msg: string) => void;
  // RetroArch's commands, exported as functions by its web build (measured
  // 2026-10-11). Synchronous to call; a state is then written across frames.
  _cmd_savefiles?: () => void;
  _cmd_save_state?: () => void;
  // Loading reads the same slot-0 file a save writes. Measured present in all
  // four web cores (fceumm, gearsystem, mgba, clownmdemu) on 2026-10-25.
  _cmd_load_state?: () => void;
  _cmd_pause?: () => void;
  _cmd_unpause?: () => void;
}

export interface ModuleOptions {
  noInitialRun: boolean;
  canvas: HTMLCanvasElement;
  locateFile(path: string, prefix: string): string;
  preRun: ((m: CoreModule) => void)[];
  print(text: string): void;
  printErr(text: string): void;
  retroArchExit?: () => void;
}

export type CoreFactory = (opts: ModuleOptions) => Promise<CoreModule>;

/** Where this server serves a web core's two files (part a). */
export function coreURL(core: string, kind: "js" | "wasm"): string {
  return `/api/retro/web-cores/${encodeURIComponent(core)}/${kind}`;
}

/*
 * The core a console plays with here, if it is installed.
 *
 * Read from the server's answer rather than a table in the client, so the
 * console list lives in one place (internal/retro/webcores).
 */
export function installedCoreFor(platform: string | null | undefined, cores: WebCores | undefined): string | null {
  if (!platform || !Array.isArray(cores?.cores)) return null;
  const c = cores.cores.find((c) => c.consoles?.includes(platform));
  return c && c.installed ? c.name : null;
}

/*
 * Whether this browser can play a console now, and if not, why not.
 *
 * Phones are offered play: the game screen gives a touch screen on-screen
 * controls (ADR 0077 Decision 6, RetroTouchControls.tsx), and steps them aside
 * for a controller. `env` stays in the signature for the reason it was added:
 * this is where a device that cannot play would be told so.
 */
export function webAvailability(
  platform: string | null | undefined,
  cores: WebCores | undefined,
  env: { coarse: boolean; gamepads: number },
): RetroAvailability {
  if (!platform) {
    return { available: false, reason: "LANcast does not know which console this game is for." };
  }
  // An answer without a core list (a server too old for the route, or one
  // answering something else) is a server with no web cores, never a throw:
  // a throw here left the game's page saying nothing at all.
  const list = Array.isArray(cores?.cores) ? cores.cores : [];
  const c = list.find((c) => c.consoles?.includes(platform));
  if (!c) {
    // SNES, N64 and PlayStation, by decision; or a server too old to say.
    return { available: false, reason: "Plays in the LANcast desktop app." };
  }
  if (!c.installed) {
    return {
      available: false,
      reason:
        "Plays in a browser once an administrator downloads the browser cores, in Settings → Retro games. Or play it in the LANcast desktop app.",
    };
  }
  void env;
  return { available: true, core: c.name, licence: c.licence };
}

/** The touch screen and controller facts webAvailability needs, read now. */
export function browserEnv(): { coarse: boolean; gamepads: number } {
  const coarse =
    typeof window !== "undefined" &&
    typeof window.matchMedia === "function" &&
    window.matchMedia("(pointer: coarse)").matches;
  let gamepads = 0;
  try {
    gamepads = (navigator.getGamepads?.() ?? []).filter(Boolean).length;
  } catch {
    // A browser that refuses the Gamepad API has none to count.
  }
  return { coarse, gamepads };
}

/** A game file's name made safe to be a path inside the module. */
export function contentName(name: string): string {
  const base = name.split(/[\\/]/).pop() ?? "game";
  const safe = base.replace(/[^A-Za-z0-9._ ()[\]-]/g, "_");
  return safe === "" || safe.startsWith(".") ? "game" + safe : safe;
}


/*
 * Where RetroArch keeps one game's saves, measured on 2026-10-11 rather than
 * assumed.
 *
 * RetroArch names a save after the content file it loaded, which is the name
 * contentName gave it, so the base here must come from the same function. By
 * default it also sorts saves into a folder per core (`saves/mGBA/…`); the
 * config turns that off, because predicting each core's folder name is a
 * guess and a wrong guess loads nothing.
 */
export function savePaths(romName: string) {
  const file = contentName(romName);
  const dot = file.lastIndexOf(".");
  const base = dot > 0 ? file.slice(0, dot) : file;
  return {
    sram: `${SAVES}/${base}.srm`,
    // Slot 0, what _cmd_save_state writes with no slot chosen.
    state: `${STATES}/${base}.state`,
    // What savestate_auto_load reads when the content loads.
    autoLoad: `${STATES}/${base}.state.auto`,
  };
}

/*
 * The label a browser state is stored under.
 *
 * Measured: RetroArch writes states RZIP-compressed (`#RZIPv1#`), and the
 * desktop player stores a core's raw serialization. A state from one cannot
 * be given to the other, so a browser state never carries a desktop core's
 * name: the desktop refuses a state whose core is not its own, and the
 * browser offers to continue only from a state with this label.
 */
export const WEB_STATE_PREFIX = "retroarch-web/";

export function webStateCore(core: string): string {
  return `${WEB_STATE_PREFIX}${core}`;
}

/*
 * Which player made a stored state, for saying so in a save list, or null for
 * a slot that holds no state. The game's own save (`sram`) names no core,
 * because every player reads it.
 */
export function statePlayer(core: string | null | undefined): "browser" | "desktop" | null {
  if (!core) return null;
  return core.startsWith(WEB_STATE_PREFIX) ? "browser" : "desktop";
}

/** How a state's player reads in a sentence: "the desktop app", "a browser". */
export function statePlayerName(player: "browser" | "desktop"): string {
  return player === "browser" ? "a browser" : "the desktop app";
}

export interface Booted {
  /** The running module, for reading what RetroArch wrote. */
  module: CoreModule;
  /** Starts the game. Call from a user gesture, or the browser keeps it silent. */
  start(): void;
  /** Sends RetroArch a command (MENU_TOGGLE, FULLSCREEN_TOGGLE, …). */
  send(command: string): void;
  /**
   * The game's own save as it is now, or null if it keeps none. Asks
   * RetroArch to write it first, so it is current rather than as of the last
   * autosave.
   */
  readSRAM(): Uint8Array | null;
  /**
   * A save state of this moment, or null if none was written in time. State
   * saving runs across frames, so this waits for the file to change.
   */
  saveState(timeoutMs?: number): Promise<Uint8Array | null>;
  /**
   * Loads a state this browser made, from its bytes. False when the game is
   * not running or the core cannot load one; it does not check the state is
   * this core's, which is the caller's to decide (statePlayer).
   */
  loadState(state: Uint8Array): boolean;
  /** Pauses the game, as RetroArch's own pause does: sound and all. */
  pause(): void;
  resume(): void;
}

/*
 * Boots a web core with one game in it, ready to start.
 *
 * `sram` is the game's own save to start with, and `resume` a browser state to
 * continue from. `importCore` is the dynamic import, passed in so tests can
 * hand over a fake module; the default imports the loader from this server,
 * where the session cookie travels with it, as it does for every same-origin
 * module script.
 */
export async function bootWebCore(opts: {
  core: string;
  canvas: HTMLCanvasElement;
  rom: { name: string; bytes: Uint8Array };
  sram?: Uint8Array | null;
  resume?: Uint8Array | null;
  onExit?: () => void;
  log?: (line: string) => void;
  importCore?: (url: string) => Promise<{ default: CoreFactory }>;
}): Promise<Booted> {
  const importCore =
    opts.importCore ?? ((url: string) => import(/* @vite-ignore */ url) as Promise<{ default: CoreFactory }>);
  const log = opts.log ?? (() => {});
  const corePath = `${HOME}/cores/${opts.core}_libretro.core`;

  const loader = await importCore(coreURL(opts.core, "js"));
  const module = await loader.default({
    noInitialRun: true,
    canvas: opts.canvas,
    // The loader asks for its module beside itself, by file name. This server
    // serves it at the core's route instead.
    locateFile: (path, prefix) => (path.endsWith(".wasm") ? coreURL(opts.core, "wasm") : prefix + path),
    // RetroArch finds the core it is built with through this. The file it
    // names only has to exist: the core is inside the module, not in it.
    preRun: [(m) => {
      m.ENV["LIBRARY_PATH"] = corePath;
    }],
    print: (t) => log(t),
    printErr: (t) => log(t),
    retroArchExit: opts.onExit,
  });

  const game = `${CONTENT}/${contentName(opts.rom.name)}`;
  const paths = savePaths(opts.rom.name);
  for (const dir of [`${HOME}/cores`, CONTENT, SAVES, STATES]) {
    module.FS.mkdirTree(dir);
  }
  module.FS.writeFile(corePath, new Uint8Array());
  const resume = !!opts.resume && opts.resume.length > 0;
  module.FS.writeFile(CONFIG, retroarchConfig(resume));
  module.FS.writeFile(game, opts.rom.bytes);
  if (opts.sram && opts.sram.length > 0) module.FS.writeFile(paths.sram, opts.sram);
  if (resume) module.FS.writeFile(paths.autoLoad, opts.resume!);

  const read = (path: string): Uint8Array | null => {
    try {
      return module.FS.readFile(path);
    } catch {
      return null;
    }
  };

  let started = false;
  return {
    module,
    start() {
      if (started) return;
      started = true;
      module.callMain(["-v", game, "-c", CONFIG]);
    },
    send(command) {
      module.EmscriptenSendCommand?.(command);
    },
    readSRAM() {
      if (!started) return null;
      module._cmd_savefiles?.();
      return read(paths.sram);
    },
    async saveState(timeoutMs = 3000) {
      if (!started || !module._cmd_save_state) return null;
      const before = read(paths.state);
      module._cmd_save_state();
      const deadline = Date.now() + timeoutMs;
      while (Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 100));
        const now = read(paths.state);
        if (now && now.length > 0 && !sameBytes(now, before)) return now;
      }
      return null;
    },
    loadState(state) {
      if (!started || !module._cmd_load_state || state.length === 0) return false;
      // The slot-0 file is what RetroArch loads from with no slot chosen,
      // and what saveState watches; a numbered slot is LANcast's, kept on
      // the server, and passes through this one file both ways.
      module.FS.writeFile(paths.state, state);
      module._cmd_load_state();
      return true;
    },
    pause() {
      if (started) module._cmd_pause?.();
    },
    resume() {
      if (started) module._cmd_unpause?.();
    },
  };
}

function sameBytes(a: Uint8Array, b: Uint8Array | null): boolean {
  if (!b || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
