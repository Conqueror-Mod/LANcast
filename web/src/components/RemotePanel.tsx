import { useEffect, useState } from "react";
import { Link } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { Item, RemoteCommandRequest, RemotePlayer } from "@/api/types";
import { useFocusable } from "@/focus/FocusController";
import { useItem } from "@/api/hooks";
import { clock } from "@/lib/format";
import {
  canSend,
  otherPlayers,
  playerPosition,
  sendCommand,
  useRemoteDevice,
  useRemotePlayers,
} from "@/playback/remote";
import { NextGlyph, PrevGlyph, StopGlyph } from "./PlayerGlyphs";
import "./RemotePanel.css";

/*
 * Your other devices: send something to one, and drive it like a remote
 * (ADR 0117).
 *
 * One component in three places. From the player it moves what is playing to
 * another device at the same moment, and stops here. From an item page it
 * sends that item to start where Play would start it. On the Devices page it
 * sends nothing and is only the remote.
 */

export interface SendSpec {
  /** The button's word: "Move here", "Play here". */
  label: string;
  itemID: number;
  queue: number[];
  /** Where to start, or null for wherever Play would start it there. */
  positionMS: number | null;
  fromStart?: boolean;
  /** Called once the command is queued: the player stops here. */
  after?: () => void;
}

export function RemotePanel({
  send,
  onClose,
  page = false,
}: {
  send?: SendSpec;
  onClose?: () => void;
  /** Drawn on its own page rather than as a popover. */
  page?: boolean;
}) {
  const [self] = useRemoteDevice();
  const players = useRemotePlayers(true);
  const others = otherPlayers(players.data, self.id);
  // When this window received the list, for extrapolating positions.
  const [receivedAt, setReceivedAt] = useState(() => Date.now());
  useEffect(() => setReceivedAt(Date.now()), [players.dataUpdatedAt]);
  const [error, setError] = useState<string | null>(null);

  return (
    <div
      className={"remote" + (page ? " remote--page" : "")}
      role={page ? undefined : "dialog"}
      aria-label="Play on another device"
    >
      {!page && (
        <div className="remote__head">
          <span className="section-label">Play on another device</span>
          {onClose && (
            <button className="remote__x" onClick={onClose} aria-label="Close">
              ✕
            </button>
          )}
        </div>
      )}

      {players.isLoading && <p className="remote__lead">Looking for your devices…</p>}

      {!players.isLoading && others.length === 0 && (
        <div className="remote__empty">
          <p className="remote__lead">None of your other devices is offering itself right now.</p>
          <p className="remote__hint">
            The LANcast app on a PC offers itself while it is open. On a phone or in a browser,
            turn on <em>Let my other devices play here</em> in{" "}
            <Link to="/settings?pane=remoteplay" onClick={onClose}>
              Settings › Remote play
            </Link>
            .
          </p>
        </div>
      )}

      {others.length > 0 && (
        <ul className="remote__list">
          {others.map((p) => (
            <PlayerRow
              key={p.id}
              player={p}
              receivedAt={receivedAt}
              send={send}
              onError={setError}
            />
          ))}
        </ul>
      )}

      {error && (
        <p className="remote__error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

const KIND_WORD: Record<string, string> = {
  desktop: "LANcast app",
  browser: "Browser",
  phone: "Phone",
};

function PlayerRow({
  player,
  receivedAt,
  send,
  onError,
}: {
  player: RemotePlayer;
  receivedAt: number;
  send?: SendSpec;
  onError: (msg: string | null) => void;
}) {
  const qc = useQueryClient();
  const item = useItem(player.item_id);
  const [busy, setBusy] = useState(false);
  // The playing position moves on between reports; redraw it each second.
  const [now, setNow] = useState(() => Date.now());
  const playing = player.item_id > 0 && !player.paused;
  useEffect(() => {
    if (!playing) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [playing]);

  const pos = playerPosition(player, receivedAt, now);

  const go = async (cmd: RemoteCommandRequest, after?: () => void) => {
    setBusy(true);
    onError(null);
    try {
      await sendCommand(player.id, cmd);
      after?.();
    } catch (e) {
      onError(
        `${player.name} did not take that: ${e instanceof Error ? e.message : String(e)}`,
      );
      // A device that has gone is the likeliest reason; the list says so.
      void qc.invalidateQueries({ queryKey: ["remote-players"] });
    } finally {
      setBusy(false);
    }
  };

  const doSend = () => {
    if (!send) return;
    const cmd: RemoteCommandRequest = {
      action: "play",
      item_id: send.itemID,
      queue: send.queue,
      from_start: send.fromStart || undefined,
    };
    if (send.positionMS !== null) cmd.position_ms = Math.max(0, Math.round(send.positionMS));
    void go(cmd, send.after);
  };

  const seekBy = (deltaMS: number) => {
    const max = player.duration_ms > 0 ? player.duration_ms : Number.MAX_SAFE_INTEGER;
    void go({ action: "seek", position_ms: Math.round(Math.min(max, Math.max(0, pos + deltaMS))) });
  };

  const title = player.item_id > 0 ? (item.data?.title ?? "…") : null;

  return (
    <li className="remote__player">
      <div className="remote__who">
        <span className="remote__name">{player.name}</span>
        <span className="remote__kind">{KIND_WORD[player.kind] ?? player.kind}</span>
      </div>

      <div className="remote__now">
        {title ? (
          <>
            <span className="remote__title">{title}</span>
            <span className="remote__time">
              {player.paused ? "Paused · " : ""}
              {clock(pos / 1000)}
              {player.duration_ms > 0 ? ` / ${clock(player.duration_ms / 1000)}` : ""}
            </span>
          </>
        ) : (
          <span className="remote__idle">Nothing playing</span>
        )}
      </div>

      {send && (
        <button className="remote__send" disabled={busy} onClick={doSend}>
          {send.label}
        </button>
      )}

      {player.item_id > 0 && (
        <div className="remote__controls" role="group" aria-label={`Control ${player.name}`}>
          <button className="remote__btn" disabled={busy} onClick={() => void go({ action: "previous" })} aria-label="Previous" title="Previous">
            <PrevGlyph size={18} />
          </button>
          <button className="remote__btn" disabled={busy} onClick={() => seekBy(-10_000)} aria-label="Back 10 seconds" title="Back 10 seconds">
            −10
          </button>
          <button
            className="remote__btn remote__btn--main"
            disabled={busy}
            onClick={() => void go({ action: player.paused ? "resume" : "pause" })}
            aria-label={player.paused ? "Play" : "Pause"}
            title={player.paused ? "Play" : "Pause"}
          >
            {player.paused ? "▶" : "❚❚"}
          </button>
          <button className="remote__btn" disabled={busy} onClick={() => seekBy(30_000)} aria-label="Forward 30 seconds" title="Forward 30 seconds">
            +30
          </button>
          <button className="remote__btn" disabled={busy} onClick={() => void go({ action: "next" })} aria-label="Next" title="Next">
            <NextGlyph size={18} />
          </button>
          <button className="remote__btn" disabled={busy} onClick={() => void go({ action: "stop" })} aria-label="Stop" title="Stop">
            <StopGlyph size={18} />
          </button>
          <label className="remote__volume">
            <span className="remote__volume-label">Volume</span>
            <input
              type="range"
              min={0}
              max={100}
              step={1}
              defaultValue={Math.round(player.volume * 100)}
              key={`${player.id}-${Math.round(player.volume * 100)}`}
              aria-label={`Volume on ${player.name}`}
              onChange={(e) => {
                // Sent on release rather than on every step of a drag:
                // each one is a command for the device to collect.
                (e.target as HTMLInputElement).dataset.v = e.target.value;
              }}
              onPointerUp={(e) => {
                const v = Number((e.target as HTMLInputElement).value);
                void go({ action: "volume", volume: v / 100 });
              }}
              onKeyUp={(e) => {
                const v = Number((e.target as HTMLInputElement).value);
                void go({ action: "volume", volume: v / 100 });
              }}
            />
          </label>
        </div>
      )}
    </li>
  );
}

/*
 * "Play on…" for an item page.
 *
 * Offered only once another of your devices is offering itself. The action
 * row had grown to ten controls once already; a button that could only ever
 * open an empty list does not earn a place in it.
 *
 * Sends the item alone, to start wherever Play would start it on that
 * device: the same answer the Play button beside it gives here.
 */
export function PlayOnButton({ item }: { item: Item }) {
  const [self] = useRemoteDevice();
  const players = useRemotePlayers(true, false);
  const [open, setOpen] = useState(false);
  const focusable = useFocusable(() => setOpen((o) => !o));
  if (!canSend(item) || item.missing || otherPlayers(players.data, self.id).length === 0) return null;
  return (
    <div className="remote-anchor">
      <button
        {...focusable}
        className="detail__play detail__play--secondary"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        Play on…
      </button>
      {open && (
        <RemotePanel
          onClose={() => setOpen(false)}
          send={{
            label: "Play here",
            itemID: item.id,
            queue: [item.id],
            positionMS: null,
            after: () => setOpen(false),
          }}
        />
      )}
    </div>
  );
}
