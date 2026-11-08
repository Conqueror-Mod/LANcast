import { useCallback, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, apiPost, ApiFailure } from "@/api/client";
import type { RemoteCommand, RemotePlayer } from "@/api/types";
import { onTopic } from "@/api/serverEvents";
import { usePlayback } from "@/playback/PlaybackProvider";
import {
  deviceKind,
  REPORT_MS,
  runCommand,
  setRemoteNotice,
  useRemoteDevice,
  useRemoteNotice,
} from "@/playback/remote";
import { clientNote } from "@/lib/clientNote";
import "./RemotePlayerHost.css";

/*
 * This window as a player for your other devices (ADR 0117).
 *
 * While the device offers itself it reports what it is playing every
 * REPORT_MS, and at once when the item or play state changes, so a remote
 * shows a pause within a moment. It collects its commands when the event
 * stream says `remote`, and also whenever a report says some are waiting: a
 * missed event then costs one report, never a command.
 *
 * Mounted once, inside the router (a play command opens the player route) and
 * outside Routes, so it lives as long as the signed-in window does.
 */

const NOTICE_MS = 4000;
const SETTLE_MS = 600;

export function RemotePlayerHost() {
  const pb = usePlayback();
  const navigate = useNavigate();
  const [device] = useRemoteDevice();
  // Shared, so the player keeps its overlay up while this shows (ADR 0111).
  const notice = useRemoteNotice();

  // Read through refs so the timers are not rebuilt on every frame of
  // playback, which is how often displayTime changes.
  const pbRef = useRef(pb);
  pbRef.current = pb;
  const navigateRef = useRef(navigate);
  navigateRef.current = navigate;

  // A seek waiting for the item a play command opened to load.
  const pendingSeek = useRef<{ itemID: number; ms: number } | null>(null);

  const taking = useRef(false);
  const take = useCallback(async () => {
    if (taking.current) return;
    taking.current = true;
    try {
      const { commands } = await apiGet<{ commands: RemoteCommand[] }>(
        `/api/remote/players/${encodeURIComponent(device.id)}/commands`,
      );
      for (const cmd of commands ?? []) {
        const effect = runCommand(cmd, pbRef.current);
        if (effect.kind === "open") {
          pendingSeek.current = effect.seekMS !== null ? { itemID: effect.itemID, ms: effect.seekMS } : null;
          navigateRef.current(`/watch/${effect.itemID}`, {
            state: {
              queue: effect.queue,
              fromStart: effect.fromStart,
              continuing: effect.seekMS !== null,
            },
          });
          setRemoteNotice(cmd.from ? `Playing from ${cmd.from}` : "Playing from another device");
          clientNote("info", "remote", `play ${effect.itemID} from ${cmd.from || "another device"}`);
        } else if (effect.kind === "ignored") {
          clientNote("info", "remote", `ignored ${cmd.action}: ${effect.reason}`);
        }
      }
    } catch (e) {
      // A 404 is this player having dropped off; the next report puts it back.
      if (!(e instanceof ApiFailure && e.status === 404)) {
        clientNote("warn", "remote", `collecting commands failed: ${String(e)}`);
      }
    } finally {
      taking.current = false;
    }
  }, [device.id]);

  const report = useCallback(async () => {
    const p = pbRef.current;
    try {
      const answer = await apiPost<RemotePlayer>(
        `/api/remote/players/${encodeURIComponent(device.id)}`,
        {
          name: device.name,
          kind: deviceKind(),
          item_id: p.itemID || 0,
          position_ms: Math.round((p.displayTime || 0) * 1000),
          duration_ms: Math.round((p.totalDuration || 0) * 1000),
          paused: !p.playing,
          volume: p.volume,
        },
        "PUT",
      );
      if (answer && answer.pending > 0) void take();
    } catch {
      // A report that fails is retried on the next beat.
    }
  }, [device.id, device.name, take]);

  // Offer, report on a beat, and withdraw when switched off or closed.
  useEffect(() => {
    if (!device.enabled) return;
    void report();
    const timer = setInterval(() => void report(), REPORT_MS);
    const off = onTopic("remote", () => void take());
    const url = `/api/remote/players/${encodeURIComponent(device.id)}`;
    // A closing tab says so, so its owner's remote does not list it for the
    // twenty seconds the server would otherwise take to notice.
    const leave = () => {
      void fetch(url, { method: "DELETE", keepalive: true }).catch(() => {});
    };
    window.addEventListener("pagehide", leave);
    return () => {
      clearInterval(timer);
      off();
      window.removeEventListener("pagehide", leave);
      leave();
    };
  }, [device.enabled, device.id, report, take]);

  // A change a remote would show is reported at once, not on the next beat.
  useEffect(() => {
    if (device.enabled) void report();
    // report reads the rest through a ref; these are the things worth a call.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pb.itemID, pb.playing]);

  /*
   * The seek a "move here" asked for, once the item it opened is playing.
   *
   * Not on the render where the item id changes: the duration and the
   * playing flag can still be the previous item's for that moment, and the
   * new source's own start would land after the seek and undo it. So it waits
   * until the item is playing and has stayed settled for SETTLE_MS; any change
   * in between starts the wait again.
   */
  useEffect(() => {
    const want = pendingSeek.current;
    if (!want || pb.itemID !== want.itemID || pb.item?.id !== want.itemID) return;
    if (pb.loading || !pb.playing || !(pb.totalDuration > 0)) return;
    const t = setTimeout(() => {
      if (pendingSeek.current !== want) return;
      pendingSeek.current = null;
      pbRef.current.seekTo(want.ms / 1000);
    }, SETTLE_MS);
    return () => clearTimeout(t);
  }, [pb.itemID, pb.item?.id, pb.loading, pb.playing, pb.totalDuration]);

  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setRemoteNotice(null), NOTICE_MS);
    return () => clearTimeout(t);
  }, [notice]);

  if (!notice) return null;
  return (
    <div className="remote-notice" role="status">
      {notice}
    </div>
  );
}
