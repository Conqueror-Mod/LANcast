import { useOptimized, useSetOptimized } from "@/api/hooks";
import { humanBytes } from "@/components/BackupSettings";
import type { OptimizedVersion } from "@/api/types";

/*
 * Status › Conversions (ADR 0087): optimized copies — the one converting now
 * with its progress, the queue in the order it will run, and the finished and
 * failed. The waiting line comes first when the queue is stuck, because a
 * queue that is not moving should say why rather than look broken.
 */
export function ConversionsSection() {
  const { data, isError } = useOptimized(true);
  const set = useSetOptimized();
  if (isError) {
    return (
      <section className="settings__section">
        <p className="set-error">The server did not answer. Try again in a moment.</p>
      </section>
    );
  }
  if (!data) return null;
  const items = Array.isArray(data.items) ? data.items : [];
  const used = data.used_bytes ?? 0;
  const limit = data.limit_bytes || 1;
  const stateLabel = (v: OptimizedVersion) => {
    if (v.state === "converting") {
      return v.item_id === data.current ? `Converting · ${Math.round((data.progress ?? 0) * 100)}%` : "Converting";
    }
    if (v.state === "queued") return "Queued";
    if (v.state === "ready") return `Ready · ${humanBytes(v.size_bytes)}`;
    return "Failed";
  };
  return (
    <section className="settings__section">
      <span className="section-label">Optimized versions</span>
      <p className="set-row__sub set-row__sub--standalone">
        Copies converted once, ahead of time, so a phone or a browser plays them directly instead of
        the server converting the film every time. Ask for one from a title&rsquo;s More Options.
      </p>
      {data.waiting && <p className="set-row__note set-row__note--warn">Paused: {data.waiting}.</p>}
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {humanBytes(used)} of {humanBytes(limit)} used
          </div>
          <div className="set-row__sub">
            In <code>{data.folder}</code>. At the limit, new requests wait; nothing is deleted to make room.
          </div>
          <div className="set-conv__bar" aria-hidden="true">
            <span style={{ width: `${Math.min(100, (used / limit) * 100)}%` }} />
          </div>
        </div>
      </div>
      {items.length === 0 && (
        <p className="set-row__sub set-row__sub--standalone">No optimized versions yet.</p>
      )}
      {items.map((v) => (
        <div className="set-row" key={v.item_id}>
          <div className="set-row__main">
            <div className="set-row__title">{v.title}</div>
            <div className="set-row__sub">
              {stateLabel(v)}
              {v.state === "failed" && v.error ? ` · ${v.error}` : ""}
            </div>
            {v.state === "converting" && v.item_id === data.current && (
              <div className="set-conv__bar" aria-hidden="true">
                <span style={{ width: `${Math.round((data.progress ?? 0) * 100)}%` }} />
              </div>
            )}
          </div>
          <div className="set-row__actions">
            {v.state === "failed" && (
              <button className="set-btn" disabled={set.isPending} onClick={() => set.mutate({ id: v.item_id, on: true })}>
                Try again
              </button>
            )}
            <button className="set-btn" disabled={set.isPending} onClick={() => set.mutate({ id: v.item_id, on: false })}>
              {v.state === "ready" ? "Remove" : "Cancel"}
            </button>
          </div>
        </div>
      ))}
    </section>
  );
}
