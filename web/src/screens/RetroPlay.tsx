import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { useGameSaves, useItem } from "@/api/hooks";
import { useBackHandler, useFocusable, useFocusScope } from "@/focus/FocusController";
import { holdNativeVideo, releaseNativeVideo } from "@/playback/mpvBackend";
import { usePlayback } from "@/playback/PlaybackProvider";
import { CORNER_LABEL, SIZE_LABEL, nextCorner, nextSize, setDock, useDock } from "@/lib/dock";
import {
  STATE_SLOTS,
  curatedOptions,
  gameVolume,
  nextValue,
  nextVolume,
  onRetroEvent,
  setGameOnScreen,
  setGameVolume,
  setRetroOption,
  openGame,
  retroCommand,
  slotLabel,
  stopGame,
  volumeLabel,
  type RetroEvent,
  type RetroOption,
} from "@/playback/retro";
import { platformLabel } from "@/lib/platforms";
import "./RetroPlay.css";

/*
 * Playing a retro game (ADR 0073, stage 2).
 *
 * The desktop client draws the game into its native video window and this
 * page sits above it, transparent, as it does over a film: the menu, the
 * loading line and any error are drawn on the picture. The page sends
 * commands and listens to events; it never touches a file, a core or a save.
 *
 * Escape (or Back) opens the menu, and the client opens it too from a pad —
 * Guide, or Select and Start held — so whichever way somebody asks, the game
 * pauses on that frame and the same menu appears.
 */
export function RetroPlay() {
  const { id } = useParams();
  const itemID = Number(id);
  const [params] = useSearchParams();
  const resume = params.get("resume") === "1";
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: item } = useItem(itemID);
  const { data: saves } = useGameSaves(itemID);
  const pb = usePlayback();
  const dock = useDock();

  const [loading, setLoading] = useState<{ done: number; total: number } | null>({ done: 0, total: 0 });
  const [running, setRunning] = useState(false);
  const [menu, setMenu] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [options, setOptions] = useState<RetroOption[]>([]);

  // The page is see-through over the picture while this screen is up, and a
  // docked film floats above it rather than above the main window (ADR 0076).
  // Held, not set: a film in the corner needs the page see-through too, and
  // goes on after this screen (mpvBackend.holdNativeVideo).
  useEffect(() => {
    holdNativeVideo("game");
    setGameOnScreen(true);
    return () => {
      releaseNativeVideo("game");
      setGameOnScreen(false);
    };
  }, []);

  // The game's own volume, read once; the menu changes it from there.
  const [volume, setVolume] = useState(1);
  useEffect(() => {
    let live = true;
    void gameVolume().then((v) => live && setVolume(v));
    return () => {
      live = false;
    };
  }, []);
  const cycleVolume = useCallback(() => {
    const next = nextVolume(volume);
    setVolume(next);
    void setGameVolume(next).catch(() => setNotice("The game's volume could not be changed."));
  }, [volume]);

  // Events from the client, for the life of the screen.
  useEffect(
    () =>
      onRetroEvent((e: RetroEvent) => {
        switch (e.kind) {
          case "loading":
            setLoading({ done: e.done ?? 0, total: e.total ?? 0 });
            break;
          case "started":
            setLoading(null);
            setRunning(true);
            break;
          case "menu":
            setMenu(true);
            break;
          case "options":
            setOptions(curatedOptions(e.options));
            break;
          case "state-saved":
            if (e.slot !== "auto") setNotice(`Saved to ${slotLabel(e.slot ?? "")}.`);
            qc.invalidateQueries({ queryKey: ["game-saves", itemID] });
            break;
          case "state-loaded":
            setNotice(`Loaded ${slotLabel(e.slot ?? "")}.`);
            setMenu(false);
            break;
          case "error":
            if (!running && loading) {
              setLoading(null);
              setError(e.text ?? "The game could not start.");
            } else {
              setNotice(e.text ?? "Something went wrong.");
            }
            break;
          case "nav":
            // The pad, while the game is paused: the client reads it and the
            // page hears it as the keys the focus controller already obeys.
            pressKey(e.text);
            break;
          case "stopped":
            setRunning(false);
            qc.invalidateQueries({ queryKey: ["game-saves", itemID] });
            break;
        }
      }),
    [itemID, qc, running, loading],
  );

  // Start the game once the item is known, and stop it when the screen goes.
  useEffect(() => {
    if (!item) return;
    let cancelled = false;
    openGame(itemID, item.platform ?? "", resume).catch((err: Error) => {
      if (!cancelled) {
        setLoading(null);
        setError(err.message || "The game could not start.");
      }
    });
    return () => {
      cancelled = true;
      // Only the game. The video window belongs to whatever film is docked,
      // which may be playing in the corner and goes on playing after this
      // screen (ADR 0076); the client hides the game's own window itself.
      void stopGame();
    };
  }, [item, itemID, resume]);

  // A notice fades by itself; a menu or an error is dismissed by the person.
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(null), 3000);
    return () => clearTimeout(t);
  }, [notice]);

  const openMenu = useCallback(() => {
    void retroCommand("pause");
    setMenu(true);
  }, []);
  const closeMenu = useCallback(() => {
    setMenu(false);
    void retroCommand("resume");
  }, []);
  const quit = useCallback(() => {
    void stopGame();
    navigate(`/item/${itemID}`, { replace: true });
  }, [navigate, itemID]);

  // Back toggles the menu while a game runs; with nothing running it leaves.
  const back = useCallback(() => {
    if (error || !running) quit();
    else if (menu) closeMenu();
    else openMenu();
  }, [error, running, menu, quit, openMenu, closeMenu]);
  useBackHandler(back);

  const filled = new Set((saves ?? []).map((s) => s.slot));
  // The menu and the error panel keep arrows and the pad to themselves: the
  // corner card's buttons are live behind them and otherwise one press away.
  const menuScope = useFocusScope();
  const errorScope = useFocusScope();

  return (
    <div className="retro-play" role="application" aria-label={item?.title ?? "Game"}>
      {loading && (
        <div className="retro-play__panel" role="status">
          <div className="retro-play__title">{item?.title}</div>
          <div className="retro-play__sub">
            {loading.total > 0
              ? `Fetching the game — ${Math.floor((loading.done / loading.total) * 100)}%`
              : "Starting…"}
          </div>
        </div>
      )}

      {error && (
        <div className="retro-play__panel" role="alert" ref={errorScope}>
          <div className="retro-play__title">This game could not start</div>
          <div className="retro-play__sub">{error}</div>
          <MenuButton label="Back" onSelect={quit} autoFocus />
        </div>
      )}

      {menu && !error && (
        <div className="retro-play__panel retro-play__menu" role="dialog" aria-label="Game menu" ref={menuScope}>
          <div className="retro-play__title">{item?.title}</div>
          <div className="retro-play__sub">{platformLabel(item?.platform)}</div>
          <MenuButton label="Resume" onSelect={closeMenu} autoFocus />
          <div className="retro-play__group" aria-label="Save">
            {STATE_SLOTS.map((slot) => (
              <MenuButton
                key={slot}
                label={`Save to ${slotLabel(slot)}`}
                onSelect={() => void retroCommand("save-state", slot)}
              />
            ))}
          </div>
          <div className="retro-play__group" aria-label="Load">
            {STATE_SLOTS.filter((slot) => filled.has(slot)).map((slot) => (
              <MenuButton
                key={slot}
                label={`Load ${slotLabel(slot)}`}
                onSelect={() => void retroCommand("load-state", slot)}
              />
            ))}
          </div>
          {options.length > 0 && (
            <div className="retro-play__group" aria-label="Picture">
              {options.map((o) => (
                <MenuButton
                  key={o.key}
                  label={`${o.description}: ${o.value}`}
                  onSelect={() => void setRetroOption(item?.platform ?? "", o.key, nextValue(o))}
                />
              ))}
              <p className="retro-play__hint">Some picture changes take effect when the game next starts.</p>
            </div>
          )}
          <MenuButton label={`Game volume: ${volumeLabel(volume)}`} onSelect={cycleVolume} />
          {pb.itemID !== 0 && (
            /*
             * Whatever is playing in the corner (ADR 0076), from the menu the
             * pad can reach — the card's own buttons answer to the mouse,
             * and somebody holding a controller should not need one.
             */
            <div className="retro-play__group" aria-label="In the corner">
              <MenuButton
                label={`${pb.playing ? "Pause" : "Play"} ${pb.item?.title ?? (pb.isAudio ? "the music" : "the film")}`}
                onSelect={pb.togglePlay}
              />
              <MenuButton label={pb.isAudio ? "Stop the music" : "Stop the film"} onSelect={pb.stop} />
              {/* Where the card sits and how big, for a pad: the strip's grip
                  and size button are the same choices for a mouse. */}
              <MenuButton
                label={`Corner: ${CORNER_LABEL[dock.corner]}`}
                onSelect={() => setDock({ corner: nextCorner(dock.corner) })}
              />
              <MenuButton
                label={`Size: ${SIZE_LABEL[dock.size]}`}
                onSelect={() => setDock({ size: nextSize(dock.size) })}
              />
            </div>
          )}
          <MenuButton
            label="Restart"
            onSelect={() => {
              void retroCommand("reset");
              closeMenu();
            }}
          />
          <MenuButton label="Quit" onSelect={quit} />
          <p className="retro-play__hint">
            In-game saves are kept on the server as you play. Quitting saves where you stopped.
          </p>
        </div>
      )}

      {notice && (
        <div className="retro-play__notice" role="status">
          {notice}
        </div>
      )}
    </div>
  );
}

const NAV_KEYS: Record<string, string> = {
  up: "ArrowUp",
  down: "ArrowDown",
  left: "ArrowLeft",
  right: "ArrowRight",
  select: "Enter",
  back: "Escape",
};

// pressKey hands a pad move to the focus controller as the key it stands for.
function pressKey(move: string | undefined) {
  const key = move ? NAV_KEYS[move] : undefined;
  if (key) document.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
}

function MenuButton({
  label,
  onSelect,
  autoFocus,
}: {
  label: string;
  onSelect: () => void;
  autoFocus?: boolean;
}) {
  const f = useFocusable(onSelect);
  const ref = useCallback(
    (el: HTMLButtonElement | null) => {
      f.ref(el);
      if (el && autoFocus) el.focus();
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [f.ref, autoFocus],
  );
  return (
    <button
      type="button"
      className="retro-play__button"
      ref={ref}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
      onClick={onSelect}
    >
      {label}
    </button>
  );
}
