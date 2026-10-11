import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { apiGet } from "@/api/client";
import { useItem, useWebCores } from "@/api/hooks";
import { useBackHandler, useSuspendFocus } from "@/focus/FocusController";
import { bootWebCore, installedCoreFor, type Booted } from "@/playback/retroWeb";
import { retroSupported } from "@/playback/retro";
import { platformLabel } from "@/lib/platforms";
import { RetroPlay } from "./RetroPlay";
import "./RetroWebPlay.css";

/*
 * /play/:id, whichever player this window has.
 *
 * The desktop app plays a game in its own native player (ADR 0073) and never
 * falls back to the browser's (ADR 0077 Decision 7). Anywhere else, the
 * browser plays it. Chosen once, at the route, so neither screen carries the
 * other's branches.
 */
export function RetroPlayRoute() {
  return retroSupported() ? <RetroPlay /> : <RetroWebPlay />;
}

type Phase =
  | { kind: "loading"; what: string }
  | { kind: "ready" }
  | { kind: "playing" }
  | { kind: "error"; message: string };

interface RomFile {
  name: string;
  size_bytes?: number;
  present?: boolean;
}

/*
 * Playing a retro game in a browser (ADR 0077 part b).
 *
 * The page fetches the game's file and the console's web core from this
 * server, boots the core with the game in it, and waits for a press: a
 * browser keeps a page silent until somebody has interacted with it, so the
 * game starts from the Start button rather than on its own.
 *
 * # Leaving reloads the page
 *
 * A running web core cannot be shut down: its loop, its WebGL context and its
 * audio have no off switch, and libretro's own web player reloads the page to
 * change cores for the same reason. So leaving this screen, by Back, by
 * Escape, or by any other navigation, reloads the destination. Anything less
 * leaves a game playing sound behind whatever page somebody went to.
 */
export function RetroWebPlay() {
  const { id } = useParams();
  const itemID = Number(id);
  const { data: item } = useItem(itemID);
  const { data: cores } = useWebCores();
  const canvas = useRef<HTMLCanvasElement | null>(null);
  const booted = useRef<Booted | null>(null);
  const started = useRef(false);
  const [phase, setPhase] = useState<Phase>({ kind: "loading", what: "Getting the game ready…" });

  // The arrows and buttons are the game's while it is on screen.
  useSuspendFocus();

  const core = installedCoreFor(item?.platform, cores);

  const leave = useCallback(() => {
    // A full load of the game's page: see "Leaving reloads the page".
    window.location.assign(`/item/${itemID}`);
  }, [itemID]);
  useBackHandler(leave);

  // Any other way out reloads where it went, for the same reason.
  useEffect(
    () => () => {
      if (started.current) window.setTimeout(() => window.location.reload(), 0);
    },
    [],
  );

  useEffect(() => {
    if (!item || !cores || booted.current) return;
    if (!core) {
      setPhase({
        kind: "error",
        message: `${platformLabel(item.platform ?? "") || "This console"} does not play in a browser on this server.`,
      });
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const { files } = await apiGet<{ files: RomFile[] }>(`/api/items/${itemID}/files`);
        const file = files.find((f) => f.present !== false);
        if (!file) throw new Error("The game's file is not on the server's disk.");

        setPhase({ kind: "loading", what: "Fetching the game…" });
        const res = await fetch(`/api/stream/${itemID}/files?name=${encodeURIComponent(file.name)}`, {
          credentials: "same-origin",
        });
        if (!res.ok) throw new Error("The server could not send the game's file.");
        const bytes = new Uint8Array(await res.arrayBuffer());

        setPhase({ kind: "loading", what: "Loading the emulator…" });
        if (cancelled || !canvas.current) return;
        booted.current = await bootWebCore({
          core,
          canvas: canvas.current,
          rom: { name: file.name, bytes },
          onExit: leave,
        });
        if (!cancelled) setPhase({ kind: "ready" });
      } catch (err) {
        if (!cancelled) setPhase({ kind: "error", message: (err as Error).message || "The game could not start." });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [item, cores, core, itemID, leave]);

  const start = () => {
    if (!booted.current) return;
    started.current = true;
    booted.current.start();
    setPhase({ kind: "playing" });
    // RetroArch reads the keyboard from the page; the canvas holding focus
    // keeps the browser from taking the arrows for scrolling.
    canvas.current?.focus();
  };

  const fullscreen = () => {
    const el = canvas.current?.parentElement;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
    else void el.requestFullscreen?.().catch(() => {});
    canvas.current?.focus();
  };

  return (
    <div className="rweb">
      <div className="rweb__stage">
        {/* RetroArch draws here through WebGL. Its id is the one the
            Emscripten glue looks for when it is not handed one. */}
        <canvas id="canvas" ref={canvas} className="rweb__canvas" tabIndex={0} aria-label="Game" />
      </div>

      <div className="rweb__bar">
        <button className="rweb__btn" onClick={leave}>
          ← Back
        </button>
        <span className="rweb__title">{item?.title ?? ""}</span>
        {phase.kind === "playing" && (
          <button className="rweb__btn" onClick={fullscreen}>
            Fullscreen
          </button>
        )}
      </div>

      {phase.kind === "loading" && (
        <div className="rweb__overlay" role="status">
          <p>{phase.what}</p>
        </div>
      )}

      {phase.kind === "ready" && (
        <div className="rweb__overlay">
          <button className="rweb__start" onClick={start} autoFocus>
            ▶ Start
          </button>
          <p className="rweb__hint">
            A controller works as soon as you press a button on it. On a keyboard:
            arrows to move, X and Z for the main buttons, Enter for Start. Saving
            in this browser comes in a later version; for now a game keeps its
            progress until you leave.
          </p>
        </div>
      )}

      {phase.kind === "error" && (
        <div className="rweb__overlay" role="alert">
          <p>{phase.message}</p>
          <button className="rweb__btn" onClick={leave}>
            Back to the game
          </button>
        </div>
      )}
    </div>
  );
}
