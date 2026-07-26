import { useState } from "react";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useCreateLibrary,
  useStartScan,
  useRefreshLibrary,
  useScanStatus,
} from "@/api/hooks";
import { DirectoryPicker } from "@/components/DirectoryPicker";
import type { Library } from "@/api/types";
import "./Settings.css";

const KEYS: [string, string][] = [
  ["Arrows", "Move between tiles and shelves"],
  ["Enter", "Open the focused item"],
  ["Esc", "Back / close"],
  ["Space · K", "Play / pause (player)"],
  ["← · →", "Seek ∓10s (player)"],
  ["[ · ]", "Cycle subtitle track (player)"],
  ["F · M", "Fullscreen · mute (player)"],
];

function whenScanned(ts: number | null): string {
  if (!ts) return "never scanned";
  return "scanned " + new Date(ts * 1000).toLocaleDateString();
}

function LibraryRow({ library }: { library: Library }) {
  const { data: status } = useScanStatus(library.id);
  const scan = useStartScan();
  const refresh = useRefreshLibrary();
  const running = status?.state === "running";

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{library.name}</div>
        <div className="set-row__sub">
          {library.path} · {library.item_count.toLocaleString()} items ·{" "}
          {running
            ? `scanning — ${status?.files_seen ?? 0} seen`
            : whenScanned(library.scanned_at)}
        </div>
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={running || scan.isPending}
          onClick={() => scan.mutate(library.id)}
        >
          {running ? "Scanning…" : "Scan"}
        </button>
        <button
          className="set-btn"
          disabled={refresh.isPending}
          onClick={() => refresh.mutate(library.id)}
        >
          Refresh metadata
        </button>
      </div>
    </div>
  );
}

function AddLibrary() {
  const create = useCreateLibrary();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [kind, setKind] = useState("movie");
  const [pickerOpen, setPickerOpen] = useState(false);

  if (!open) {
    return (
      <button className="set-btn" onClick={() => setOpen(true)}>
        + Add library
      </button>
    );
  }
  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate(
          { name, kind, path },
          {
            onSuccess: () => {
              setOpen(false);
              setName("");
              setPath("");
            },
          },
        );
      }}
    >
      <input
        className="set-input"
        placeholder="Name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        required
      />
      <input
        className="set-input set-input--wide"
        placeholder="Folder path on the server"
        value={path}
        onChange={(e) => setPath(e.target.value)}
        required
      />
      <button className="set-btn" type="button" onClick={() => setPickerOpen(true)}>
        Browse…
      </button>
      <select
        className="set-input"
        value={kind}
        onChange={(e) => setKind(e.target.value)}
      >
        <option value="movie">Movies</option>
        <option value="show">Shows</option>
        <option value="music">Music</option>
        <option value="other">Other</option>
      </select>
      <button className="set-btn" type="submit" disabled={create.isPending}>
        Create
      </button>
      <button className="set-btn" type="button" onClick={() => setOpen(false)}>
        Cancel
      </button>
      {create.isError && (
        <span className="set-error">{(create.error as Error).message}</span>
      )}
      {pickerOpen && (
        <DirectoryPicker
          onSelect={(p) => {
            setPath(p);
            setPickerOpen(false);
          }}
          onClose={() => setPickerOpen(false)}
        />
      )}
    </form>
  );
}

function ProviderKey({
  label,
  configured,
  onSave,
  pending,
}: {
  label: string;
  configured: boolean;
  onSave: (value: string) => void;
  pending: boolean;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{label}</div>
        <div className="set-row__sub">
          {configured ? "Key configured" : "Not set"} · stored write-only
        </div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input"
          type="password"
          placeholder={configured ? "Replace key" : "Enter key"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          className="set-btn"
          disabled={!value || pending}
          onClick={() => {
            onSave(value);
            setValue("");
          }}
        >
          Save
        </button>
      </div>
    </div>
  );
}

export function Settings() {
  const { data: libraries } = useLibraries();
  const { data: settings } = useSettings();
  const update = useUpdateSettings();

  return (
    <div className="settings">
      <h1 className="settings__title">Settings</h1>

      <section className="settings__section">
        <span className="section-label">Libraries</span>
        {libraries?.map((lib) => (
          <LibraryRow key={lib.id} library={lib} />
        ))}
        <AddLibrary />
      </section>

      <section className="settings__section">
        <span className="section-label">Metadata</span>
        {settings && (
          <>
            <ProviderKey
              label="TMDB"
              configured={settings.tmdb.configured}
              pending={update.isPending}
              onSave={(v) => update.mutate({ tmdb_key: v })}
            />
            <ProviderKey
              label="OpenSubtitles"
              configured={settings.opensubtitles.configured}
              pending={update.isPending}
              onSave={(v) => update.mutate({ opensubtitles_key: v })}
            />
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.auto_enrich}
                onChange={(e) =>
                  update.mutate({ auto_enrich: e.target.checked })
                }
              />
              Automatically fetch metadata after a scan
            </label>
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.write_nfo}
                onChange={(e) => update.mutate({ write_nfo: e.target.checked })}
              />
              Write NFO sidecar files next to media
            </label>
          </>
        )}
      </section>

      <section className="settings__section">
        <span className="section-label">Playback</span>
        {settings && (
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">Video encoder</div>
              <div className="set-row__sub">
                Active: {settings.encoder.active.label}
                {settings.encoder.active.hardware ? " (hardware)" : ""}
              </div>
            </div>
            <div className="set-row__actions">
              <select
                className="set-input"
                value={settings.encoder.preference}
                onChange={(e) =>
                  update.mutate({ hardware_encoder: e.target.value })
                }
              >
                <option value="auto">Auto</option>
                {settings.encoder.available.map((enc) => (
                  <option key={enc.name} value={enc.name}>
                    {enc.label}
                  </option>
                ))}
              </select>
            </div>
          </div>
        )}
      </section>

      <section className="settings__section">
        <span className="section-label">Keyboard</span>
        <div className="set-keys">
          {KEYS.map(([k, d]) => (
            <div className="set-key" key={k}>
              <kbd>{k}</kbd>
              <span>{d}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
