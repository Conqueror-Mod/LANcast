import { useState } from "react";
import { useTranscodes, useStopTranscode } from "@/api/hooks";
import type { Transcode } from "@/api/types";
import { formatBytes } from "@/lib/format";
import "./Transcodes.css";

/*
 * What the server is converting, right now — and the button that ends one.
 *
 * This exists because of a morning spent without it. A film refused to play,
 * the app said only that it could not, and the answer — three sessions holding
 * every slot, none of which had ever delivered a byte to anybody — could be had
 * exclusively by reading lancastd.log by hand. The server knew everything
 * needed to explain itself and had no way to say it.
 *
 * The column that matters is Served. Zero means the slot is held for nobody,
 * and it is what tells an abandoned session apart from a paused film.
 *
 * Administrator only, in the API as well as here, and that is a privacy
 * decision: a conversion names an account and a film, so a list of them is a
 * list of who is watching what.
 */
export function Transcodes() {
  const { data, isLoading } = useTranscodes(true);
  const stop = useStopTranscode();
  const [stopping, setStopping] = useState<string | null>(null);

  const sessions = data?.sessions ?? [];
  const max = data?.max ?? 0;

  return (
    <section className="settings__section">
      <span className="section-label">Conversions</span>
      <p className="trans__intro">
        Files the server is converting for a player that cannot take them as
        they are. A slot is freed when the last one is read from stops, which
        takes a few minutes — stopping it here is immediate.
      </p>

      {/* "Two of three" is information; "two" is a number. */}
      <p className="trans__count">
        {isLoading
          ? "Looking…"
          : `${sessions.length} of ${max} slot${max === 1 ? "" : "s"} in use`}
      </p>

      {!isLoading && sessions.length === 0 && (
        <p className="trans__empty">Nothing is being converted.</p>
      )}

      {sessions.length > 0 && (
        <div className="trans__list">
          {sessions.map((s) => (
            <Row
              key={s.id}
              session={s}
              busy={stopping === s.id && stop.isPending}
              onStop={() => {
                setStopping(s.id);
                stop.mutate(s.id);
              }}
            />
          ))}
        </div>
      )}

      {stop.isError && (
        <p className="trans__error">
          That conversion could not be stopped. It may have already ended.
        </p>
      )}
    </section>
  );
}

function Row({
  session,
  busy,
  onStop,
}: {
  session: Transcode;
  busy: boolean;
  onStop: () => void;
}) {
  const s = session;
  // A title is resolved server-side; a channel never has one, and an item that
  // has since been removed has lost one. Neither is worth an empty cell.
  const name = s.live ? "Live channel" : s.title || `Item ${s.item_id}`;
  const held = s.served_bytes === 0;

  return (
    <div className={"trans__row" + (held ? " is-held" : "")}>
      <div className="trans__what">
        <span className="trans__title">{name}</span>
        <span className="trans__meta">
          {s.encoding ? "Re-encoding" : "Remuxing"} · {s.output}
          {s.owner ? ` · ${s.owner}` : ""}
          {s.start_at > 0 ? ` · from ${clock(s.start_at)}` : ""}
          {s.finished ? " · ffmpeg finished" : ""}
        </span>
        {s.error && <span className="trans__rowerror">{s.error}</span>}
      </div>

      <span className="trans__served" title="How much picture has been handed over">
        {held ? "nothing served" : formatBytes(s.served_bytes)}
      </span>
      <span className="trans__idle">{idle(s.idle_seconds)}</span>

      <button
        type="button"
        className="trans__stop"
        onClick={onStop}
        disabled={busy}
      >
        {busy ? "Stopping…" : "Stop"}
      </button>
    </div>
  );
}

// Idle is the reading somebody is here for, so it is said in words rather than
// a raw count of seconds.
function idle(seconds: number): string {
  if (seconds < 10) return "active";
  if (seconds < 60) return `idle ${seconds}s`;
  const mins = Math.floor(seconds / 60);
  if (mins < 60) return `idle ${mins}m`;
  return `idle ${Math.floor(mins / 60)}h ${mins % 60}m`;
}

function clock(seconds: number): string {
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const sec = total % 60;
  const two = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${two(m)}:${two(sec)}` : `${m}:${two(sec)}`;
}
