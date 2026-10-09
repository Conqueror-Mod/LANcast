import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useFocusable } from "@/focus/FocusController";
import { formatBytes } from "@/lib/format";
import type { RetroInstallStatus } from "@/playback/retro";
import "./CoreDownload.css";

/*
 * Fetching the emulator cores (ADR 0073), from a game's page or Settings.
 *
 * The client does the work on its own thread; the page starts it, asks how
 * it is going twice a second while it runs, and says so. Pressing the button
 * is the one confirmation the ADR asks for, which is why the size is on it.
 *
 * When it finishes, every console's "can this play here" answer is stale —
 * the game page that offered the download is the page the person is looking
 * at — so those are invalidated, and onInstalled lets Settings re-read its own.
 */
export function CoreDownload({
  bytes,
  onInstalled,
}: {
  bytes?: number;
  onInstalled?: () => void;
}) {
  const qc = useQueryClient();
  const [st, setSt] = useState<RetroInstallStatus | null>(null);
  const wasRunning = useRef(false);
  const finished = useRef(onInstalled);
  finished.current = onInstalled;

  const read = useCallback(async () => {
    const s = (await window.lancastRetroInstallStatus?.().catch(() => null)) ?? null;
    setSt(s);
    if (s && wasRunning.current && !s.running && !s.error && s.stage === "done") {
      await qc.invalidateQueries({ queryKey: ["retro-available"] });
      finished.current?.();
    }
    wasRunning.current = !!s?.running;
  }, [qc]);

  useEffect(() => {
    void read();
  }, [read]);

  useEffect(() => {
    if (!st?.running) return;
    const t = window.setInterval(() => void read(), 500);
    return () => window.clearInterval(t);
  }, [st?.running, read]);

  const start = async () => {
    await window.lancastRetroInstallCores?.();
    wasRunning.current = true;
    await read();
  };
  const cancel = async () => {
    await window.lancastRetroCancelInstallCores?.();
    await read();
  };

  if (!window.lancastRetroInstallCores) return null;
  const size = formatBytes(st?.bytes || bytes || 0);
  const release = st?.version ? `RetroArch ${st.version}` : "RetroArch";
  const source = `From libretro's ${release} stable release. Each core is checked against a recorded checksum before it is used.`;

  if (st?.running) {
    const fraction = st.total > 0 ? Math.min(1, st.done / st.total) : 0;
    const what =
      st.stage === "unpack"
        ? `Unpacking cores… ${st.done} of ${st.total}`
        : `Downloading cores… ${formatBytes(st.done)} of ${formatBytes(st.total)}`;
    return (
      <div className="core-download" role="status" aria-live="polite">
        <div className="core-download__line">
          <span>{what}</span>
          {st.stage !== "unpack" && <Btn label="Cancel" onPress={() => void cancel()} />}
        </div>
        <div className="core-download__bar">
          <div className="core-download__fill" style={{ width: `${Math.round(fraction * 100)}%` }} />
        </div>
      </div>
    );
  }
  return (
    <div className="core-download">
      <Btn
        label={st?.error ? "Try the download again" : `Download emulator cores (${size})`}
        onPress={() => void start()}
        primary
      />
      {st?.error ? (
        <p className="core-download__note core-download__note--warn">{st.error}</p>
      ) : (
        <p className="core-download__note">{source}</p>
      )}
    </div>
  );
}

function Btn({ label, onPress, primary }: { label: string; onPress: () => void; primary?: boolean }) {
  const focusable = useFocusable(onPress);
  return (
    <button
      {...focusable}
      className={primary ? "core-download__btn core-download__btn--go" : "core-download__btn"}
      onClick={onPress}
    >
      {label}
    </button>
  );
}
