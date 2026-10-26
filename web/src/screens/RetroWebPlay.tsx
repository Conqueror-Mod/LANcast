import { useCallback, useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { apiGet } from "@/api/client";
import { useItem, useWebCores } from "@/api/hooks";
import { useBackHandler, useFocusScope, useSuspendFocus } from "@/focus/FocusController";
import {
  bootWebCore,
  installedCoreFor,
  statePlayer,
  statePlayerName,
  webStateCore,
  type Booted,
} from "@/playback/retroWeb";
import { retroSupported, slotLabel, STATE_SLOTS } from "@/playback/retro";
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

/** One stored save, as the save list gives it. */
interface StoredSave {
  slot: string;
  core?: string;
  updated_at?: number;
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
  const [params] = useSearchParams();
  const resume = params.get("resume") === "1";
  // The save last stored, so an unchanged one is not sent again.
  const lastSRAM = useRef<Uint8Array | null>(null);
  const canvas = useRef<HTMLCanvasElement | null>(null);
  const booted = useRef<Booted | null>(null);
  const started = useRef(false);
  const [phase, setPhase] = useState<Phase>({ kind: "loading", what: "Getting the game ready…" });
  // This person's saves for the game, for the states panel.
  const [stored, setStored] = useState<StoredSave[]>([]);
  const [states, setStates] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  // The arrows and buttons are the game's while it is on screen, and the
  // panel's while the states panel is open over it.
  useSuspendFocus(!states);

  useEffect(() => {
    if (!notice) return;
    const t = window.setTimeout(() => setNotice(null), 3000);
    return () => window.clearTimeout(t);
  }, [notice]);

  const core = installedCoreFor(item?.platform, cores);

  const version = cores?.archive?.version ?? "";

  // Stores the game's own save when it has changed since the last store.
  const storeSRAM = useCallback(async () => {
    const b = booted.current;
    if (!b) return;
    const now = b.readSRAM();
    if (!now || now.length === 0 || sameBytes(now, lastSRAM.current)) return;
    if (await putSave(itemID, "sram", now)) lastSRAM.current = now;
  }, [itemID]);

  const leaving = useRef(false);
  const leave = useCallback(async () => {
    if (leaving.current) return;
    leaving.current = true;
    /*
     * Saved on the way out: the game's own save, shared with the desktop
     * player, and a state of this moment for Continue in a browser. Bounded,
     * so a server that has gone away costs a few seconds and not the way
     * out.
     */
    const b = booted.current;
    if (b && started.current && core) {
      setPhase({ kind: "loading", what: "Saving…" });
      const work = (async () => {
        await storeSRAM();
        const state = await b.saveState();
        if (state) await putSave(itemID, "auto", state, webStateCore(core), version);
      })();
      await Promise.race([work, new Promise((r) => setTimeout(r, 6000))]);
    }
    // A full load of the game's page: see "Leaving reloads the page".
    window.location.assign(`/item/${itemID}`);
  }, [itemID, core, version, storeSRAM]);
  useBackHandler(() => void leave());

  // While playing, the game's own save is stored when it changes. RetroArch
  // writes it every ten seconds (retroarchConfig); a closed tab loses at most
  // that and this interval, and the saves the game itself made before them
  // are already on the server.
  useEffect(() => {
    if (phase.kind !== "playing") return;
    const t = window.setInterval(() => void storeSRAM(), 15_000);
    return () => window.clearInterval(t);
  }, [phase.kind, storeSRAM]);

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

        // The game's own save, from whichever player made it last, and a
        // browser state to continue from when asked for one that is ours.
        const saves = await listSaves(itemID);
        if (!cancelled) setStored(saves);
        const sram = saves.some((s) => s.slot === "sram") ? await getSave(itemID, "sram") : null;
        lastSRAM.current = sram;
        const auto = saves.find((s) => s.slot === "auto");
        const state =
          resume && auto && auto.core === webStateCore(core) ? await getSave(itemID, "auto") : null;

        setPhase({ kind: "loading", what: "Loading the emulator…" });
        if (cancelled || !canvas.current) return;
        booted.current = await bootWebCore({
          core,
          canvas: canvas.current,
          rom: { name: file.name, bytes },
          sram,
          resume: state,
          onExit: () => void leave(),
        });
        // Development only: a handle for measuring what RetroArch writes.
        if (import.meta.env.DEV) (window as unknown as { __rweb?: unknown }).__rweb = booted.current;
        if (!cancelled) setPhase({ kind: "ready" });
      } catch (err) {
        if (!cancelled) setPhase({ kind: "error", message: (err as Error).message || "The game could not start." });
      }
    })();
    return () => {
      cancelled = true;
    };
    // Booted once: a later change to what leave closes over must not boot the
    // game a second time.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item, cores, core, itemID]);

  const start = () => {
    if (!booted.current) return;
    started.current = true;
    booted.current.start();
    setPhase({ kind: "playing" });
    // RetroArch reads the keyboard from the page; the canvas holding focus
    // keeps the browser from taking the arrows for scrolling.
    canvas.current?.focus();
  };

  /*
   * Numbered states (ADR 0077, part c's "still to come").
   *
   * The same three slots the desktop's game menu offers, kept on the server
   * per person and game. A browser state and a desktop one cannot load in
   * each other's player (they are different formats), so the panel loads only
   * what this browser made, and says which player made the rest.
   *
   * The game pauses while the panel is open, and resumes before a save or a
   * load runs: RetroArch writes and reads a state across frames, and a paused
   * game runs none.
   */
  const openStates = () => {
    booted.current?.pause();
    setStates(true);
  };
  const closeStates = useCallback(() => {
    setStates(false);
    booted.current?.resume();
    canvas.current?.focus();
  }, []);

  const saveTo = async (slot: string) => {
    closeStates();
    const b = booted.current;
    if (!b || !core) return;
    const state = await b.saveState();
    if (state && (await putSave(itemID, slot, state, webStateCore(core), version))) {
      setNotice(`Saved to ${slotLabel(slot)}.`);
      setStored(await listSaves(itemID));
    } else {
      setNotice(`${slotLabel(slot)} could not be saved.`);
    }
  };

  const loadFrom = async (slot: string) => {
    closeStates();
    const b = booted.current;
    if (!b) return;
    const state = await getSave(itemID, slot);
    setNotice(state && b.loadState(state) ? `Loaded ${slotLabel(slot)}.` : `${slotLabel(slot)} could not be loaded.`);
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
        <button className="rweb__btn" onClick={() => void leave()}>
          ← Back
        </button>
        <span className="rweb__title">{item?.title ?? ""}</span>
        {phase.kind === "playing" && (
          <>
            <button className="rweb__btn" onClick={openStates}>
              Save states
            </button>
            <button className="rweb__btn" onClick={fullscreen}>
              Fullscreen
            </button>
          </>
        )}
      </div>

      {states && core && (
        <StatesPanel
          stored={stored}
          ours={webStateCore(core)}
          onSave={(slot) => void saveTo(slot)}
          onLoad={(slot) => void loadFrom(slot)}
          onClose={closeStates}
        />
      )}

      {notice && (
        <div className="rweb__notice" role="status">
          {notice}
        </div>
      )}

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
            arrows to move, X and Z for the main buttons, Enter for Start. The
            game&apos;s own saves are kept on the server and shared with the
            desktop app, and leaving keeps your place to continue in a browser.
          </p>
        </div>
      )}

      {phase.kind === "error" && (
        <div className="rweb__overlay" role="alert">
          <p>{phase.message}</p>
          <button className="rweb__btn" onClick={() => void leave()}>
            Back to the game
          </button>
        </div>
      )}
    </div>
  );
}

/*
 * The states panel: each numbered slot, what is in it, and what this browser
 * can do with it. Its own component so its Back closes it rather than leaving
 * the game, and so the pad's arrows stay inside it while it is open.
 */
export function StatesPanel({
  stored,
  ours,
  onSave,
  onLoad,
  onClose,
}: {
  stored: StoredSave[];
  /** The label this browser's states carry (webStateCore). */
  ours: string;
  onSave(slot: string): void;
  onLoad(slot: string): void;
  onClose(): void;
}) {
  useBackHandler(onClose);
  const scope = useFocusScope();
  const bySlot = new Map(stored.map((s) => [s.slot, s]));
  return (
    <div className="rweb__overlay" role="dialog" aria-label="Save states" ref={scope}>
      <div className="rweb__states">
        <p className="rweb__states-title">Save states</p>
        {STATE_SLOTS.map((slot, i) => {
          const s = bySlot.get(slot);
          const player = statePlayer(s?.core);
          const mine = !!s && s.core === ours;
          const status = !player
            ? "Empty"
            : mine
              ? "Saved in a browser"
              : player === "browser"
                ? "Made by another emulator, which this one can't load"
                : `Made in ${statePlayerName(player)}, which a browser can't load`;
          return (
            <div className="rweb__slot" key={slot} data-slot={slot}>
              <div className="rweb__slot-text">
                <span className="rweb__slot-name">{slotLabel(slot)}</span>
                <span className="rweb__slot-status">{status}</span>
              </div>
              <button className="rweb__btn" onClick={() => onSave(slot)} autoFocus={i === 0}>
                {player && !mine ? `Save over ${slotLabel(slot)}` : `Save to ${slotLabel(slot)}`}
              </button>
              {mine && (
                <button className="rweb__btn" onClick={() => onLoad(slot)}>
                  Load {slotLabel(slot)}
                </button>
              )}
            </div>
          );
        })}
        <button className="rweb__btn" onClick={onClose}>
          Back to the game
        </button>
      </div>
    </div>
  );
}

/** This person's saves for one game; empty when the server cannot say. */
async function listSaves(itemID: number): Promise<StoredSave[]> {
  return apiGet<{ saves: StoredSave[] }>(`/api/items/${itemID}/saves`)
    .then((r) => r?.saves ?? [])
    .catch(() => []);
}

/** One save's bytes, or null when the slot is empty or unreadable. */
async function getSave(itemID: number, slot: string): Promise<Uint8Array | null> {
  try {
    const res = await fetch(`/api/items/${itemID}/saves/${slot}`, { credentials: "same-origin" });
    if (!res.ok) return null;
    return new Uint8Array(await res.arrayBuffer());
  } catch {
    return null;
  }
}

/*
 * Stores one save. A state names the core and version that wrote it, as the
 * server requires (romsaves.go); the game's own save names neither, because
 * every player reads it.
 */
async function putSave(itemID: number, slot: string, data: Uint8Array, core?: string, version?: string): Promise<boolean> {
  const q = core ? `?core=${encodeURIComponent(core)}&core_version=${encodeURIComponent(version || "unknown")}` : "";
  try {
    const res = await fetch(`/api/items/${itemID}/saves/${slot}${q}`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/octet-stream" },
      body: new Blob([data as BlobPart]),
    });
    return res.ok;
  } catch {
    return false;
  }
}

function sameBytes(a: Uint8Array, b: Uint8Array | null): boolean {
  if (!b || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}
