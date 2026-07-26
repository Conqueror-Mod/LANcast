import { useState } from "react";
import { useBrowse } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import "./DirectoryPicker.css";

// A server-side folder picker for choosing a library's path. It walks the
// server's filesystem (the browse endpoint lists directories only), so a person
// adding a library doesn't have to type an absolute path from memory.
export function DirectoryPicker({
  initialPath = "",
  onSelect,
  onClose,
}: {
  initialPath?: string;
  onSelect: (path: string) => void;
  onClose: () => void;
}) {
  const [current, setCurrent] = useState(initialPath);
  const { data, isLoading, isError, error } = useBrowse(current);

  useBackHandler(onClose);

  const atRoots = current === "";

  return (
    <div className="dirpick__overlay" onClick={onClose}>
      <div className="dirpick" onClick={(e) => e.stopPropagation()} role="dialog">
        <div className="dirpick__head">
          <span className="section-label">Choose a folder</span>
          <button className="dirpick__x" onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>

        <div className="dirpick__bar">
          <button
            className="dirpick__up"
            disabled={atRoots}
            onClick={() => setCurrent(data?.parent ?? "")}
            title="Up one level"
          >
            ↑ Up
          </button>
          <span className="dirpick__crumb">{current || "This server"}</span>
        </div>

        <div className="dirpick__list">
          {isLoading && <p className="dirpick__msg">Loading…</p>}
          {isError && (
            <p className="dirpick__msg">
              {(error as Error)?.message ?? "This folder can't be read."}
            </p>
          )}
          {data?.entries.length === 0 && !isLoading && (
            <p className="dirpick__msg">No subfolders here.</p>
          )}
          {data?.entries.map((e) => (
            <button
              key={e.path}
              className="dirpick__entry"
              onClick={() => setCurrent(e.path)}
            >
              <span className="dirpick__folder" aria-hidden="true">
                🗀
              </span>
              {e.name}
            </button>
          ))}
        </div>

        <div className="dirpick__foot">
          <span className="dirpick__foot-path">{atRoots ? "" : current}</span>
          <button className="dirpick__cancel" onClick={onClose}>
            Cancel
          </button>
          <button
            className="dirpick__use"
            disabled={atRoots}
            onClick={() => onSelect(current)}
          >
            Use this folder
          </button>
        </div>
      </div>
    </div>
  );
}
