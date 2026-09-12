import { useState } from "react";
import { Link } from "react-router-dom";
import { DisplayPicker } from "@/components/DisplayPicker";
import {
  gamesSupported,
  hiddenCount,
  sortGames,
  useGameArt,
  useGames,
  useLaunchGame,
  useRescanGames,
  useSetGameDisplay,
  useSetGameFlags,
  visibleGames,
  type GameRow,
  type GameSort,
} from "@/lib/games";
import "./Games.css";

/*
 * The Games tab (ADR 0066).
 *
 * The one screen in LANcast that is about this computer rather than about the
 * server. Nothing here is streamed, nothing here is in a library, and the
 * server does not know any of it exists — the window reads Steam's own files on
 * this disk and starts a game through Steam.
 *
 * So the failure states matter more than usual, and each of them says something
 * different: no bindings means you are not in the desktop app, disabled means
 * you have not asked for this, not-installed means there is no Steam here. An
 * empty grid would say all three at once and none of them clearly.
 */
export function Games() {
  const { data, isLoading } = useGames();
  const rescan = useRescanGames();
  const launch = useLaunchGame();
  const setDisplay = useSetGameDisplay();
  const [sort, setSort] = useState<GameSort>(rememberedSort);
  const [filter, setFilter] = useState("");
  const [showHidden, setShowHidden] = useState(false);
  /*
   * One picker for the page, holding the game it was opened for.
   *
   * Not one per tile: two open at once is a state this screen should not be
   * able to reach, and a modal owned by the thing that triggered it is how that
   * happens.
   */
  const [picking, setPicking] = useState<GameRow | null>(null);

  if (!gamesSupported()) {
    return (
      <GamesShell>
        <p className="browse__message">
          Games are part of the LANcast desktop app. Open LANcast on the
          computer your games are installed on, and they will be here.
        </p>
      </GamesShell>
    );
  }

  const status = data?.status;
  const all = data?.games ?? [];
  const shown = sortGames(visibleGames(all, { filter, showHidden }), sort);
  const hidden = hiddenCount(all);
  const favourites = shown.filter((g) => g.favourite);

  /*
   * Play asks first, once.
   *
   * A game nobody has answered for raises the picker instead of starting; every
   * launch after that goes straight through. The alternative — a dialog between
   * you and every game, every time — is a tax on the thing this tab exists to
   * make quick.
   */
  const play = (g: GameRow) => {
    if (!g.display) setPicking(g);
    else launch.mutate(g.id);
  };

  // Saved before launching, not after: the client reads the answer out of
  // games.json at launch, so a launch that raced the save would open on
  // whatever the previous answer was — which for a first launch is nowhere.
  const chooseThenPlay = async (device: string) => {
    const g = picking;
    if (!g) return;
    setPicking(null);
    try {
      await setDisplay.mutateAsync({ id: g.id, device });
    } catch {
      // Saying where it should open is a convenience; failing to record it is
      // not a reason to refuse to start the game.
    }
    launch.mutate(g.id);
  };

  return (
    <GamesShell
      count={status === "ok" ? all.length : undefined}
      onRescan={status === "ok" ? () => rescan() : undefined}
    >
      {isLoading && !data && <p className="browse__message">Looking…</p>}

      {status === "disabled" && (
        <p className="browse__message">
          Games are switched off. Turn on <strong>Show my installed games</strong>{" "}
          in Settings, under This computer.
        </p>
      )}

      {status === "not-installed" && (
        <p className="browse__message">
          No Steam installation was found on this computer. LANcast reads
          Steam's own files — it never signs in to your account.
        </p>
      )}

      {status === "error" && (
        <p className="browse__message">
          Steam is installed but could not be read: {data?.error}
        </p>
      )}

      {status === "ok" && all.length === 0 && (
        <p className="browse__message">
          Steam is here, with no games installed yet.
        </p>
      )}

      {/* One place for a failed launch, now that the button that starts one is
          shared by every tile. */}
      {launch.isError && (
        <p className="games__error">{String(launch.error.message)}</p>
      )}

      {status === "ok" && all.length > 0 && (
        <>
          <div className="games__controls">
            <input
              className="games__filter"
              type="search"
              placeholder="Filter by name"
              aria-label="Filter games by name"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            />
            <label className="games__sort">
              <span className="games__sort-label">Sort</span>
              <select
                aria-label="Sort games"
                value={sort}
                onChange={(e) => {
                  const next = e.target.value as GameSort;
                  setSort(next);
                  rememberSort(next);
                }}
              >
                <option value="name">Name</option>
                <option value="played">Last played</option>
                <option value="size">Size</option>
              </select>
            </label>
            {hidden > 0 && (
              <button
                className="games__hidden-toggle"
                onClick={() => setShowHidden((v) => !v)}
              >
                {showHidden ? "Hide hidden" : `Show hidden (${hidden})`}
              </button>
            )}
          </div>

          {/* Favourites are a shelf rather than a badge on a tile, because the
              point of marking one is to stop scrolling for it. */}
          {favourites.length > 0 && !filter && (
            <section className="games__shelf">
              <span className="section-label">Favourites</span>
              <div className="games__grid">
                {favourites.map((g) => (
                  <GameTile
                    key={`fav-${g.id}`}
                    game={g}
                    onPlay={play}
                    starting={launch.isPending && launch.variables === g.id}
                  />
                ))}
              </div>
            </section>
          )}

          <section className="games__shelf">
            {favourites.length > 0 && !filter && (
              <span className="section-label">All games</span>
            )}
            <div className="games__grid">
              {shown.map((g) => (
                <GameTile
                  key={g.id}
                  game={g}
                  onPlay={play}
                  starting={launch.isPending && launch.variables === g.id}
                />
              ))}
            </div>
            {shown.length === 0 && (
              <p className="browse__message">Nothing matches that.</p>
            )}
          </section>
        </>
      )}

      {picking && (
        <DisplayPicker
          game={picking}
          onChoose={chooseThenPlay}
          onCancel={() => setPicking(null)}
        />
      )}
    </GamesShell>
  );
}

function GamesShell({
  children,
  count,
  onRescan,
}: {
  children: React.ReactNode;
  count?: number;
  onRescan?: () => void;
}) {
  return (
    <div className="browse games">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Games</h1>
        <span className="browse__count">{count ? count : ""}</span>
        {onRescan && (
          <button className="games__rescan" onClick={onRescan}>
            Rescan
          </button>
        )}
      </div>
      <p className="games__note">
        Installed on this computer, read from Steam's own files. LANcast starts
        them; it does not stream them.
      </p>
      {children}
    </div>
  );
}

function GameTile({
  game,
  onPlay,
  starting,
}: {
  game: GameRow;
  onPlay: (g: GameRow) => void;
  starting: boolean;
}) {
  const { data: art } = useGameArt(game.id, "poster", game.has_poster);
  const flags = useSetGameFlags();

  return (
    <div className={"games__tile" + (game.hidden ? " is-hidden" : "")}>
      <Link className="games__art" to={`/games/${game.id}`} title={game.name}>
        {art ? (
          <img src={art} alt="" />
        ) : (
          // A lettered placeholder rather than a broken image: Steam caches
          // artwork lazily, so a game installed and never looked at has none.
          <span className="games__placeholder" aria-hidden="true">
            {game.name.slice(0, 1).toUpperCase()}
          </span>
        )}
      </Link>
      <div className="games__meta">
        <Link className="games__name" to={`/games/${game.id}`}>
          {game.name}
        </Link>
        <div className="games__actions">
          <button
            className="games__play"
            onClick={() => onPlay(game)}
            disabled={starting}
          >
            {starting ? "Starting…" : "Play"}
          </button>
          {/*
            A star, never gold. Gold means where you are and nothing else
            (docs/design.md) — the moment it also means favourite, the focus
            signal is dead.
          */}
          <button
            className={"games__flag" + (game.favourite ? " is-on" : "")}
            aria-label={game.favourite ? "Remove favourite" : "Make favourite"}
            aria-pressed={game.favourite}
            onClick={() =>
              flags.mutate({
                id: game.id,
                hidden: game.hidden,
                favourite: !game.favourite,
              })
            }
          >
            ★
          </button>
          <button
            className="games__flag"
            aria-label={game.hidden ? "Unhide" : "Hide"}
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
      </div>
    </div>
  );
}

// The chosen sort is a convenience, not state worth a file: it lives in this
// browser profile and a missing or unreadable value is simply the default.
const SORT_KEY = "lancast.games.sort";

function rememberedSort(): GameSort {
  try {
    const v = localStorage.getItem(SORT_KEY);
    if (v === "name" || v === "played" || v === "size") return v;
  } catch {
    // A private window, or site data switched off. Not worth a word to anyone.
  }
  return "name";
}

function rememberSort(v: GameSort) {
  try {
    localStorage.setItem(SORT_KEY, v);
  } catch {
    // As above.
  }
}
