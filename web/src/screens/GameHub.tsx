import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { useIsAdmin, useLibraries, useRetroDatabase } from "@/api/hooks";
import { useFocusable } from "@/focus/FocusController";
import { GamesIcon, LibraryIcon } from "@/components/LibraryIcon";
import { gamesSupported, useGames, useGamesTab } from "@/lib/games";
import { pcHalf, retroHalf, type PCHalf, type RetroHalf } from "@/lib/gameHub";
import type { Library } from "@/api/types";
import "./GameHub.css";

/*
 * The Game Hub (docs/game-hub-plan.md): one place to go to play, split into
 * the games installed on this computer and the retro games on the server.
 *
 * A front door, not a replacement. Each half opens the screen it always
 * opened — PC Games, or a retro library — and those screens are unchanged.
 * What the hub adds is the sentence for every case where there is nothing to
 * open yet: no games installed, the list switched off, a phone that could
 * never start one, no retro library, no ROM database.
 */
export function GameHub() {
  const navigate = useNavigate();
  const isAdmin = useIsAdmin();
  const { data: libraries } = useLibraries();
  const desktop = gamesSupported();
  const enabled = useGamesTab();
  const { data: games } = useGames();
  const db = useRetroDatabase(isAdmin);

  const pc = pcHalf({ supported: desktop, enabled, result: enabled ? games : undefined });
  const retro = retroHalf<Library>({
    libraries,
    databaseInstalled: isAdmin ? db.data?.installed : undefined,
  });

  return (
    <div className="game-hub">
      <h1 className="game-hub__title">Game Hub</h1>
      <div className="game-hub__halves">
        <PCPanel half={pc} isAdmin={isAdmin} navigate={navigate} />
        <RetroPanel half={retro} isAdmin={isAdmin} navigate={navigate} />
      </div>
    </div>
  );
}

function PCPanel({
  half,
  navigate,
}: {
  half: PCHalf;
  isAdmin: boolean;
  navigate: (to: string) => void;
}) {
  const open = useCallback(() => {
    if (half.kind === "ready" || half.kind === "none" || half.kind === "error") navigate("/games");
    else if (half.kind === "off") navigate("/settings?pane=games");
  }, [half.kind, navigate]);
  const actionable = half.kind !== "not-desktop" && half.kind !== "loading";

  let line: string;
  let action: string | null = null;
  switch (half.kind) {
    case "ready":
      line = `${half.count} installed ${half.count === 1 ? "game" : "games"}`;
      action = "Open PC Games";
      break;
    case "none":
      line =
        "No installed games found. LANcast looks for games from Steam, Epic, Battle.net, the Xbox app, GOG and the EA app on this computer.";
      action = "Open PC Games";
      break;
    case "error":
      line = `The launchers could not be read: ${half.reason}`;
      action = "Open PC Games";
      break;
    case "off":
      line = "Show the games installed on this computer here — Steam, Epic, Battle.net, the Xbox app, GOG and the EA app.";
      action = "Turn on in Settings";
      break;
    case "not-desktop":
      line = "PC games play in the LANcast desktop app, on the computer they are installed on.";
      break;
    case "loading":
      line = "Looking for installed games…";
      break;
  }

  return (
    <HubPanel
      side="pc"
      title="PC Games"
      icon={<GamesIcon />}
      line={line}
      action={action}
      onOpen={actionable ? open : undefined}
    />
  );
}

function RetroPanel({
  half,
  isAdmin,
  navigate,
}: {
  half: RetroHalf<Library>;
  isAdmin: boolean;
  navigate: (to: string) => void;
}) {
  if (half.kind === "ready" && half.libraries.length > 1) {
    // Several retro libraries: each one is its own way in.
    return (
      <section className="game-hub__panel game-hub__panel--retro game-hub__panel--list" aria-label="Retro Games">
        <div className="game-hub__head">
          <LibraryIcon kind="retro" />
          <h2 className="game-hub__name">Retro Games</h2>
        </div>
        <div className="game-hub__libs">
          {half.libraries.map((lib) => (
            <LibraryRow key={lib.id} lib={lib} onOpen={() => navigate(`/library/${lib.id}`)} />
          ))}
        </div>
        {half.needsDatabase && <DatabaseNudge onOpen={() => navigate("/settings?pane=retro")} />}
      </section>
    );
  }

  if (half.kind === "ready") {
    const lib = half.libraries[0];
    return (
      <HubPanel
        side="retro"
        title="Retro Games"
        icon={<LibraryIcon kind="retro" />}
        line={`${lib.item_count} ${lib.item_count === 1 ? "game" : "games"} in ${lib.name}`}
        action="Open Retro Games"
        onOpen={() => navigate(`/library/${lib.id}`)}
        footer={half.needsDatabase ? <DatabaseNudge onOpen={() => navigate("/settings?pane=retro")} /> : null}
      />
    );
  }

  if (half.kind === "loading") {
    return <HubPanel side="retro" title="Retro Games" icon={<LibraryIcon kind="retro" />} line="Loading…" />;
  }

  // No retro library: how to add one, which is an admin's to do.
  return (
    <HubPanel
      side="retro"
      title="Retro Games"
      icon={<LibraryIcon kind="retro" />}
      line="No retro games library yet."
      steps={[
        "Add a library of the type Retro games.",
        "Point it at a folder of ROMs — one folder per console works best.",
        "Download the ROM database, so every game is named and gets its box art.",
      ]}
      action={isAdmin ? "Add a library in Settings" : null}
      onOpen={isAdmin ? () => navigate("/settings?pane=libraries") : undefined}
      note={isAdmin ? null : "Ask whoever runs this server to add one."}
    />
  );
}

function HubPanel({
  side,
  title,
  icon,
  line,
  steps,
  action,
  onOpen,
  footer,
  note,
}: {
  side: "pc" | "retro";
  title: string;
  icon: React.ReactNode;
  line: string;
  steps?: string[];
  action?: string | null;
  onOpen?: () => void;
  footer?: React.ReactNode;
  note?: string | null;
}) {
  const f = useFocusable(onOpen);
  const body = (
    <>
      <div className="game-hub__head">
        {icon}
        <h2 className="game-hub__name">{title}</h2>
      </div>
      <p className="game-hub__line">{line}</p>
      {steps && (
        <ol className="game-hub__steps">
          {steps.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ol>
      )}
      {action && <span className="game-hub__action">{action}</span>}
      {note && <p className="game-hub__note">{note}</p>}
    </>
  );
  return (
    <section className={`game-hub__panel game-hub__panel--${side}`} aria-label={title}>
      {onOpen ? (
        <button
          type="button"
          className="game-hub__open"
          ref={f.ref}
          tabIndex={f.tabIndex}
          data-focus-id={f["data-focus-id"]}
          onClick={onOpen}
        >
          {body}
        </button>
      ) : (
        <div className="game-hub__open game-hub__open--inert">{body}</div>
      )}
      {footer}
    </section>
  );
}

function LibraryRow({ lib, onOpen }: { lib: Library; onOpen: () => void }) {
  const f = useFocusable(onOpen);
  return (
    <button
      type="button"
      className="game-hub__lib"
      ref={f.ref}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
      onClick={onOpen}
    >
      <span className="game-hub__lib-name">{lib.name}</span>
      <span className="game-hub__lib-count">
        {lib.item_count} {lib.item_count === 1 ? "game" : "games"}
      </span>
    </button>
  );
}

function DatabaseNudge({ onOpen }: { onOpen: () => void }) {
  const f = useFocusable(onOpen);
  return (
    <button
      type="button"
      className="game-hub__nudge"
      ref={f.ref}
      tabIndex={f.tabIndex}
      data-focus-id={f["data-focus-id"]}
      onClick={onOpen}
    >
      The ROM database is not installed, so games are named by their files and have no box art. Download it
      in Settings → Retro games.
    </button>
  );
}
