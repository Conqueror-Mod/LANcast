import type { SubtitleTrack } from "@/api/types";
import "./SubtitleMenu.css";

interface Props {
  tracks: SubtitleTrack[];
  activeKey: string | null;
  onSelect: (key: string | null) => void;
}

function detail(t: SubtitleTrack): string {
  if (!t.available) return t.reason ?? "unavailable";
  return [t.source, t.codec].filter(Boolean).join(" · ").toUpperCase();
}

// The in-player subtitle picker. Unavailable tracks (bitmap subtitles that
// cannot become WebVTT) stay listed with their reason rather than hidden —
// hiding them leaves a viewer wondering why a film they know has subtitles
// appears to have none.
export function SubtitleMenu({ tracks, activeKey, onSelect }: Props) {
  const row = (
    key: string | null,
    label: string,
    sub: string,
    disabled: boolean,
  ) => (
    <button
      key={key ?? "off"}
      className={"submenu__row" + (disabled ? " is-disabled" : "")}
      aria-selected={activeKey === key}
      disabled={disabled}
      onClick={() => onSelect(key)}
    >
      <span className="submenu__tick">{activeKey === key ? "✓" : ""}</span>
      <span className="submenu__label">
        {label}
        {sub && <span className="submenu__sub">{sub}</span>}
      </span>
    </button>
  );

  return (
    <div className="submenu" role="menu">
      <div className="submenu__head section-label">Subtitles</div>
      {row(null, "Off", "", false)}
      {tracks.length === 0 && (
        <p className="submenu__empty">No subtitle tracks for this file.</p>
      )}
      {tracks.map((t) => row(t.key, t.label, detail(t), !t.available))}
    </div>
  );
}
