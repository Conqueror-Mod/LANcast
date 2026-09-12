import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { DisplayPicker } from "@/components/DisplayPicker";
import {
  displayAnswerLabel,
  gamesSupported,
  useDisplays,
  useGameArt,
  useGames,
  useLaunchGame,
  useOpenGameFolder,
  useSetGameDisplay,
  useSetGameFlags,
} from "@/lib/games";
import { formatBytes } from "@/lib/format";
import "./Games.css";

/*
 * One game.
 *
 * It reads the same ["games"] query the grid does rather than asking for a
 * single game of its own. There is no per-game binding to ask — a scan answers
 * for the whole library at once — and a second key would be a sibling of the
 * one every mutation invalidates, which is how a page ends up showing a
 * favourite that was un-favourited a moment ago.
 */
export function GameDetail() {
  const { id = "" } = useParams();
  const { data, isLoading } = useGames();
  const launch = useLaunchGame();
  const openFolder = useOpenGameFolder();
  const flags = useSetGameFlags();
  const setDisplay = useSetGameDisplay();
  // Only fetched once this page needs to name a screen, which is why the hook
  // takes a flag rather than always enumerating monitors on mount.
  const { data: displays = [] } = useDisplays();
  const [picking, setPicking] = useState(false);
  /*
   * Two ways into the picker, two different endings.
   *
   * Opened by Play, choosing a screen starts the game. Opened by Change, it
   * only records the answer — somebody editing a preference has not asked for
   * a game to start, and starting one anyway would be the page deciding
   * something it was never told.
   *
   * Declared here with the other hooks rather than beside the handler that
   * uses it: everything below runs after an early return for a game that is no
   * longer installed, and a hook called after a return is a hook that is
   * sometimes not called at all.
   */
  const [pickerStartsGame, setPickerStartsGame] = useState(false);

  const game = data?.games?.find((g) => g.id === id);
  const { data: art } = useGameArt(id, "header", !!game?.has_header);

  if (!gamesSupported()) {
    return (
      <div className="browse games">
        <p className="browse__message">
          Games are part of the LANcast desktop app.
        </p>
      </div>
    );
  }

  if (!game) {
    return (
      <div className="browse games">
        <div className="browse__head">
          <Link className="games__back" to="/games">
            ← Games
          </Link>
        </div>
        <p className="browse__message">
          {isLoading
            ? "Looking…"
            : "That game is not installed on this computer any more."}
        </p>
      </div>
    );
  }

  const play = () => {
    if (!game.display) setPicking(true);
    else launch.mutate(game.id);
  };

  const chose = async (device: string) => {
    setPicking(false);
    try {
      await setDisplay.mutateAsync({ id: game.id, device });
    } catch {
      // Recording the answer is a convenience; failing to is not a reason to
      // refuse to start the game.
    }
    if (pickerStartsGame) launch.mutate(game.id);
  };

  return (
    <div className="browse games game-detail">
      <div className="browse__head">
        <Link className="games__back" to="/games">
          ← Games
        </Link>
      </div>

      {art && (
        <div className="game-detail__hero">
          <img src={art} alt="" />
        </div>
      )}

      <h1 className="game-detail__title">{game.name}</h1>

      <div className="game-detail__facts">
        {game.size_bytes > 0 && <span>{formatBytes(game.size_bytes)}</span>}
        <span>
          {game.last_played
            ? `Last played ${new Date(game.last_played * 1000).toLocaleDateString()}`
            : "Not played yet"}
        </span>
      </div>

      <div className="game-detail__display">
        <span>
          Opens on <strong>{displayAnswerLabel(game.display, displays)}</strong>
        </span>
        <button
          className="games__flag"
          onClick={() => {
            setPickerStartsGame(false);
            setPicking(true);
          }}
        >
          Change
        </button>
      </div>

      <div className="game-detail__actions">
        <button
          className="games__play games__play--big"
          onClick={() => {
            setPickerStartsGame(true);
            play();
          }}
          disabled={launch.isPending}
        >
          {launch.isPending ? "Starting…" : "Play"}
        </button>
        <button
          className="games__secondary"
          onClick={() => openFolder.mutate(game.id)}
        >
          Open folder
        </button>
        <button
          className={"games__secondary" + (game.favourite ? " is-on" : "")}
          aria-pressed={game.favourite}
          onClick={() =>
            flags.mutate({
              id: game.id,
              hidden: game.hidden,
              favourite: !game.favourite,
            })
          }
        >
          {game.favourite ? "Remove favourite" : "Make favourite"}
        </button>
        <button
          className="games__secondary"
          aria-pressed={game.hidden}
          onClick={() =>
            flags.mutate({
              id: game.id,
              hidden: !game.hidden,
              favourite: game.favourite,
            })
          }
        >
          {game.hidden ? "Unhide" : "Hide"}
        </button>
      </div>

      {(launch.isError || openFolder.isError) && (
        <p className="games__error">
          {String((launch.error ?? openFolder.error)?.message)}
        </p>
      )}

      {/* The install path is shown because it answers "which drive did this
          end up on", which is the question somebody has when a disk is full.
          It is text, not a link: the page never handles a path, and Open
          folder goes through the client, which checks it first. */}
      <p className="game-detail__path">{game.install_path}</p>

      {picking && (
        <DisplayPicker
          game={game}
          onChoose={chose}
          onCancel={() => setPicking(false)}
        />
      )}
    </div>
  );
}
