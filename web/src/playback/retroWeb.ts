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

/*
 * The configuration a game starts with.
 *
 * RGUI because it is drawn by RetroArch itself: the default menu needs an
 * asset bundle LANcast does not fetch. Not pausing when the tab loses focus,
 * because a person reaching for a controller should not find the game
 * frozen behind a notification. The rest is RetroArch's own default.
 */
export const RETROARCH_CONFIG = [
  'menu_driver = "rgui"',
  'pause_nonactive = "false"',
  'video_font_enable = "false"',
  'savefile_directory = "' + HOME + '/userdata/saves"',
  'savestate_directory = "' + HOME + '/userdata/states"',
].join("\n");

/** The part of an Emscripten module this file uses. */
export interface CoreModule {
  FS: {
    mkdirTree(path: string): void;
    writeFile(path: string, data: Uint8Array | string): void;
  };
  ENV: Record<string, string>;
  callMain(args: string[]): void;
  EmscriptenSendCommand?: (msg: string) => void;
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
 * Phones are not offered play until there are on-screen controls (ADR 0077
 * Decision 6); one with a controller connected is, because then it is just a
 * small screen.
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
  if (env.coarse && env.gamepads === 0) {
    return { available: false, reason: "Plays with a controller, or in the LANcast desktop app." };
  }
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

export interface Booted {
  /** Starts the game. Call from a user gesture, or the browser keeps it silent. */
  start(): void;
  /** Sends RetroArch a command (MENU_TOGGLE, FULLSCREEN_TOGGLE, …). */
  send(command: string): void;
}

/*
 * Boots a web core with one game in it, ready to start.
 *
 * `importCore` is the dynamic import, passed in so tests can hand over a fake
 * module; the default imports the loader from this server, where the session
 * cookie travels with it, as it does for every same-origin module script.
 */
export async function bootWebCore(opts: {
  core: string;
  canvas: HTMLCanvasElement;
  rom: { name: string; bytes: Uint8Array };
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
  for (const dir of [`${HOME}/cores`, CONTENT, `${HOME}/userdata/saves`, `${HOME}/userdata/states`]) {
    module.FS.mkdirTree(dir);
  }
  module.FS.writeFile(corePath, new Uint8Array());
  module.FS.writeFile(CONFIG, RETROARCH_CONFIG);
  module.FS.writeFile(game, opts.rom.bytes);

  let started = false;
  return {
    start() {
      if (started) return;
      started = true;
      module.callMain(["-v", game, "-c", CONFIG]);
    },
    send(command) {
      module.EmscriptenSendCommand?.(command);
    },
  };
}
