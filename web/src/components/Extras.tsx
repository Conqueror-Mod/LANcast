import { useState } from "react";
import { useBackHandler, useFocusable } from "@/focus/FocusController";
import type { Extra } from "@/api/types";
import "./TrailerModal.css";
import "./Extras.css";

/*
 * A film's extras (ADR 0098): a row of tiles on its page, each playing in a
 * lightbox straight from the file. Nothing is recorded — an extra has no
 * progress and is never watched — so this is a plain <video>, not the film
 * player, and leaving it leaves nothing behind.
 */

const KIND_LABEL: Record<string, string> = {
  trailer: "Trailer",
  featurette: "Featurette",
  behind_the_scenes: "Behind the scenes",
  deleted_scene: "Deleted scene",
  interview: "Interview",
  scene: "Scene",
  short: "Short",
  other: "Extra",
};

export function extraURL(itemID: number, key: string): string {
  return `/api/items/${itemID}/extras/${encodeURIComponent(key)}`;
}

export function ExtrasRow({
  itemID,
  extras,
  onPlay,
}: {
  itemID: number;
  extras: Extra[];
  onPlay: (extra: Extra) => void;
}) {
  if (extras.length === 0) return null;
  return (
    <div className="detail__extras" data-item={itemID}>
      <span className="section-label">Extras</span>
      <div className="detail__extras-row">
        {extras.map((x) => (
          <ExtraTile key={x.key} extra={x} onPlay={() => onPlay(x)} />
        ))}
      </div>
    </div>
  );
}

function ExtraTile({ extra, onPlay }: { extra: Extra; onPlay: () => void }) {
  const focusable = useFocusable(onPlay);
  return (
    <button {...focusable} className="extra-tile" onClick={onPlay}>
      <span className="extra-tile__kind">{KIND_LABEL[extra.kind] ?? "Extra"}</span>
      <span className="extra-tile__title">{extra.title}</span>
    </button>
  );
}

/*
 * ExtraPlayer plays one extra in the trailer lightbox. A file the engine cannot
 * play says so, with its format, rather than sitting black: the browser decodes
 * what it can and there is no conversion for extras.
 */
export function ExtraPlayer({
  itemID,
  extra,
  filmTitle,
  onClose,
}: {
  itemID: number;
  extra: Extra;
  filmTitle: string;
  onClose: () => void;
}) {
  useBackHandler(onClose);
  const [failed, setFailed] = useState(false);
  return (
    <div className="trailer__overlay" onClick={onClose}>
      <div className="trailer__box" onClick={(e) => e.stopPropagation()}>
        <button className="trailer__x" onClick={onClose} aria-label={`Close ${extra.title}`}>
          ×
        </button>
        <div className="trailer__frame">
          {failed ? (
            <p className="trailer__unsupported">
              This {extra.container.toUpperCase()} file can&rsquo;t be played here.
            </p>
          ) : (
            <video
              src={extraURL(itemID, extra.key)}
              title={`${filmTitle} — ${extra.title}`}
              controls
              autoPlay
              onError={() => setFailed(true)}
            />
          )}
        </div>
      </div>
    </div>
  );
}
