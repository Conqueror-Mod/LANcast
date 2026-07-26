import { useState } from "react";
import {
  useSubtitleSearch,
  useDownloadSubtitle,
  useDeleteSubtitle,
} from "@/api/hooks";
import type { SubtitleCandidate, SubtitleTrack } from "@/api/types";
import "./SubtitleMenu.css";

interface Props {
  itemID: number;
  itemTitle: string;
  language: string;
  tracks: SubtitleTrack[];
  activeKey: string | null;
  onSelect: (key: string | null) => void;
}

function detail(t: SubtitleTrack): string {
  if (!t.available) return t.reason ?? "unavailable";
  return [t.source, t.codec].filter(Boolean).join(" · ").toUpperCase();
}

// The in-player subtitle picker, with two modes: the track list (Off plus each
// embedded/sidecar/downloaded track) and an online search. Unavailable bitmap
// tracks stay listed with their reason rather than hidden.
export function SubtitleMenu({
  itemID,
  itemTitle,
  language,
  tracks,
  activeKey,
  onSelect,
}: Props) {
  const [mode, setMode] = useState<"list" | "search">("list");
  const del = useDeleteSubtitle(itemID);

  if (mode === "search") {
    return (
      <SearchView
        itemID={itemID}
        itemTitle={itemTitle}
        language={language}
        onBack={() => setMode("list")}
        onApplied={(key) => {
          onSelect(key);
          setMode("list");
        }}
      />
    );
  }

  const row = (
    key: string | null,
    label: string,
    sub: string,
    disabled: boolean,
    removable = false,
  ) => (
    <div className="submenu__line" key={key ?? "off"}>
      <button
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
      {removable && (
        <button
          className="submenu__del"
          title="Remove this downloaded subtitle"
          aria-label="Remove subtitle"
          disabled={del.isPending}
          onClick={() => {
            if (activeKey === key) onSelect(null);
            del.mutate(key as string);
          }}
        >
          ×
        </button>
      )}
    </div>
  );

  return (
    <div className="submenu" role="menu">
      <div className="submenu__head section-label">Subtitles</div>
      {row(null, "Off", "", false)}
      {tracks.map((t) =>
        row(t.key, t.label, detail(t), !t.available, t.source === "downloaded"),
      )}
      <button
        className="submenu__row submenu__search-open"
        onClick={() => setMode("search")}
      >
        <span className="submenu__tick" />
        <span className="submenu__label">
          Search online…
          <span className="submenu__sub">Find a matching subtitle file</span>
        </span>
      </button>
    </div>
  );
}

function SearchView({
  itemID,
  itemTitle,
  language,
  onBack,
  onApplied,
}: {
  itemID: number;
  itemTitle: string;
  language: string;
  onBack: () => void;
  onApplied: (key: string) => void;
}) {
  const [text, setText] = useState(itemTitle);
  const [query, setQuery] = useState<string | null>(""); // "" = search by title/hash
  const search = useSubtitleSearch(itemID, query, language);
  const download = useDownloadSubtitle(itemID);

  const pick = (c: SubtitleCandidate) =>
    download.mutate(c, { onSuccess: (r) => onApplied(r.key) });

  return (
    <div className="submenu" role="menu">
      <div className="submenu__searchbar">
        <button className="submenu__back" onClick={onBack} aria-label="Back">
          ←
        </button>
        <input
          className="submenu__input"
          value={text}
          placeholder={itemTitle}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") setQuery(text.trim());
          }}
          autoFocus
        />
        <button className="submenu__go" onClick={() => setQuery(text.trim())}>
          Search
        </button>
      </div>

      {search.isFetching && <p className="submenu__empty">Searching…</p>}
      {search.isError && (
        <p className="submenu__empty">
          {(search.error as Error).message ?? "Search failed."}
        </p>
      )}
      {download.isError && (
        <p className="submenu__empty">
          {(download.error as Error).message ?? "Download failed."}
        </p>
      )}

      {search.data?.candidates.length === 0 && !search.isFetching && (
        <p className="submenu__empty">No matching subtitles found.</p>
      )}

      {search.data?.candidates.map((c) => (
        <button
          key={c.file_id}
          className="submenu__row"
          disabled={download.isPending}
          onClick={() => pick(c)}
        >
          <span className="submenu__tick">{c.hash_match ? "●" : ""}</span>
          <span className="submenu__label">
            {c.release || c.file_name}
            <span className="submenu__sub">
              {Math.round(c.score * 100)}% ·{" "}
              {c.hash_match ? "exact file match" : c.reason || "ranked"}
              {c.download_count ? ` · ${c.download_count.toLocaleString()} dl` : ""}
            </span>
          </span>
        </button>
      ))}
    </div>
  );
}
