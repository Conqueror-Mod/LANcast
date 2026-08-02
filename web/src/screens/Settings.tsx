import { useRef, useState } from "react";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useCreateLibrary,
  useDeleteLibrary,
  useStartScan,
  useRefreshLibrary,
  useScanStatus,
  useProbeStatus,
  useReprobe,
  useCurrentUser,
  useIsAdmin,
  useUsers,
  useCreateUser,
  useDeleteUser,
  useResetUserPassword,
  useChangePassword,
  usePlugins,
  useUploadPlugin,
  useGrantPlugin,
  useSetPluginEnabled,
  useRemovePlugin,
} from "@/api/hooks";
import { DirectoryPicker } from "@/components/DirectoryPicker";
import { ApiFailure } from "@/api/client";
import type {
  AuthUser,
  Library,
  Plugin,
  Settings as SettingsType,
  SettingsUpdate,
} from "@/api/types";
import "./Settings.css";

const KEYS: [string, string][] = [
  ["Arrows", "Move between tiles and shelves"],
  ["Enter", "Open the focused item"],
  ["Esc", "Back / close"],
  ["Space · K", "Play / pause (player)"],
  ["← · →", "Seek ∓10s (player)"],
  ["[ · ]", "Cycle subtitle track (player)"],
  ["↑ · ↓", "Volume up · down (player)"],
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
  const del = useDeleteLibrary();
  const running = status?.state === "running";
  const [showIssues, setShowIssues] = useState(false);
  const [confirming, setConfirming] = useState(false);

  const skipped = status?.skipped ?? 0;
  const issues = status?.issues ?? [];

  return (
    <div className="set-lib">
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{library.name}</div>
          <div className="set-row__sub">
            {library.path} · {library.item_count.toLocaleString()} items ·{" "}
            {running
              ? `scanning — ${status?.files_seen ?? 0} seen`
              : whenScanned(library.scanned_at)}
            {!running && skipped > 0 && (
              <>
                {" · "}
                <button
                  className="set-issues-toggle"
                  onClick={() => setShowIssues((v) => !v)}
                >
                  {skipped.toLocaleString()} skipped
                </button>
              </>
            )}
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
          {confirming ? (
            <>
              <span className="set-confirm">Remove?</span>
              <button
                className="set-btn set-btn--danger"
                disabled={del.isPending || running}
                onClick={() => del.mutate(library.id)}
              >
                Yes, remove
              </button>
              <button className="set-btn" onClick={() => setConfirming(false)}>
                Cancel
              </button>
            </>
          ) : (
            <button
              className="set-btn set-btn--danger"
              disabled={running}
              title={running ? "Wait for the scan to finish" : undefined}
              onClick={() => setConfirming(true)}
            >
              Remove
            </button>
          )}
        </div>
      </div>
      {showIssues && issues.length > 0 && (
        <ul className="set-issues">
          {issues.map((i, k) => (
            <li key={k}>
              <span className="set-issue-path">{i.path}</span>
              <span className="set-issue-reason">{i.reason}</span>
            </li>
          ))}
          {skipped > issues.length && (
            <li className="set-issue-more">
              …and {(skipped - issues.length).toLocaleString()} more
            </li>
          )}
        </ul>
      )}
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
  hint,
}: {
  label: string;
  configured: boolean;
  onSave: (value: string) => void;
  pending: boolean;
  hint?: string;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{label}</div>
        <div className="set-row__sub">
          {hint ? `${hint} · ` : ""}
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

function errorMessage(err: unknown): string {
  if (err instanceof ApiFailure) return err.message;
  if (err instanceof Error) return err.message;
  return "Something went wrong.";
}

function UserRow({ user, isSelf }: { user: AuthUser; isSelf: boolean }) {
  const del = useDeleteUser();
  const reset = useResetUserPassword();
  const [resetting, setResetting] = useState(false);
  const [password, setPassword] = useState("");
  const [done, setDone] = useState(false);

  return (
    <div className="set-lib">
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {user.name}
            {isSelf && <span className="set-tag">you</span>}
          </div>
          <div className="set-row__sub">{user.role}</div>
        </div>
        <div className="set-row__actions">
          {resetting ? (
            <>
              <input
                className="set-input"
                type="password"
                placeholder="New password"
                autoFocus
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              <button
                className="set-btn"
                disabled={!password || reset.isPending}
                onClick={() =>
                  reset.mutate(
                    { id: user.id, password },
                    {
                      onSuccess: () => {
                        setResetting(false);
                        setPassword("");
                        setDone(true);
                      },
                    },
                  )
                }
              >
                Set
              </button>
              <button className="set-btn" onClick={() => setResetting(false)}>
                Cancel
              </button>
            </>
          ) : (
            <>
              {done && <span className="set-row__sub">password reset</span>}
              <button className="set-btn" onClick={() => setResetting(true)}>
                Reset password
              </button>
              {!isSelf && (
                <button
                  className="set-btn set-btn--danger"
                  disabled={del.isPending}
                  onClick={() => del.mutate(user.id)}
                >
                  Remove
                </button>
              )}
            </>
          )}
        </div>
      </div>
      {(del.isError || reset.isError) && (
        <span className="set-error">
          {errorMessage(del.error ?? reset.error)}
        </span>
      )}
    </div>
  );
}

function AddUser() {
  const create = useCreateUser();
  const [open, setOpen] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("member");

  if (!open) {
    return (
      <button className="set-btn" onClick={() => setOpen(true)}>
        + Add user
      </button>
    );
  }
  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate(
          { username, password, role },
          {
            onSuccess: () => {
              setOpen(false);
              setUsername("");
              setPassword("");
              setRole("member");
            },
          },
        );
      }}
    >
      <input
        className="set-input"
        placeholder="Username"
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        required
      />
      <input
        className="set-input"
        type="password"
        placeholder="Password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        required
      />
      <select
        className="set-input"
        value={role}
        onChange={(e) => setRole(e.target.value)}
      >
        <option value="member">Member</option>
        <option value="admin">Admin</option>
      </select>
      <button className="set-btn" type="submit" disabled={create.isPending}>
        Create
      </button>
      <button className="set-btn" type="button" onClick={() => setOpen(false)}>
        Cancel
      </button>
      {create.isError && (
        <span className="set-error">{errorMessage(create.error)}</span>
      )}
    </form>
  );
}

// Admin-only. Members never mount this, so its admin-only queries never fire.
function UsersSection() {
  const { data: users } = useUsers();
  const me = useCurrentUser();

  return (
    <section className="settings__section">
      <span className="section-label">Users</span>
      {users?.map((u) => (
        <UserRow key={u.id} user={u} isSelf={u.id === me?.id} />
      ))}
      <AddUser />
    </section>
  );
}

// Everyone can change their own password. The server revokes the caller's
// sessions on success, so the auth gate returns them to the login screen.
function AccountSection() {
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");

  return (
    <section className="settings__section">
      <span className="section-label">Account</span>
      <form
        className="set-add"
        onSubmit={(e) => {
          e.preventDefault();
          change.mutate({ current_password: current, new_password: next });
        }}
      >
        <input
          className="set-input"
          type="password"
          placeholder="Current password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          required
        />
        <input
          className="set-input"
          type="password"
          placeholder="New password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          required
        />
        <button
          className="set-btn"
          type="submit"
          disabled={change.isPending || !current || !next}
        >
          Change password
        </button>
        {change.isError && (
          <span className="set-error">{errorMessage(change.error)}</span>
        )}
      </form>
    </section>
  );
}

// The metadata, playback, and library sections all read admin-only settings, so
// they live in a child that members never mount.
function AdminSections() {
  const { data: libraries } = useLibraries();
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();

  return (
    <>
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
            <ProviderKey
              label="OMDb"
              hint="Rotten Tomatoes, Metacritic & IMDb ratings"
              configured={settings.omdb.configured}
              pending={update.isPending}
              onSave={(v) => update.mutate({ omdb_key: v })}
            />
            <MediaToolsRow settings={settings} update={update} />
            <ReprobeRow available={!!settings.media_tools?.probe_available} />
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

    </>
  );
}

function KeyboardSection() {
  return (
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
  );
}

// ffmpeg/ffprobe status. This is a row rather than a hidden detail because its
// absence is otherwise invisible: nothing gets probed, every file is
// direct-played, and whatever the browser cannot decode fails with no
// explanation — which is exactly how a whole library went unplayable unnoticed.
function MediaToolsRow({
  settings,
  update,
}: {
  settings: SettingsType;
  update: { mutate: (u: SettingsUpdate) => void; isPending: boolean };
}) {
  const tools = settings.media_tools;
  const ok = tools?.probe_available;
  const [value, setValue] = useState("");
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          Media tools{" "}
          <span className={"addon-signer addon-signer--" + (ok ? "first_party" : "unsigned")}>
            {ok ? "found" : "missing"}
          </span>
        </div>
        <div className="set-row__sub">
          {ok
            ? `ffmpeg and ffprobe available${tools.directory ? " · " + tools.directory : " on PATH"}`
            : "Without ffmpeg, LANcast cannot inspect or convert media — files play only if your browser already supports them. Install ffmpeg, or set the folder containing it here."}
        </div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input set-input--wide"
          type="text"
          placeholder={tools?.directory || "Folder containing ffmpeg"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          className="set-btn"
          disabled={update.isPending || !value.trim()}
          onClick={() => {
            update.mutate({ ffmpeg_dir: value.trim() });
            setValue("");
          }}
        >
          Save
        </button>
      </div>
    </div>
  );
}

// Re-probing. Sits under the media-tools row because it is the same subject:
// what the server knows about your files, and what to do when that knowledge
// is out of date.
//
// Exists because a probe is only as good as the build that made it. Nothing
// revisits an already-probed file on its own, so when the prober learns to read
// something new — bit depth being the case that prompted this — every older
// file keeps a playback decision made without it, permanently.
//
// The full re-probe asks for confirmation inline rather than in a dialog. It is
// not destructive (nothing is deleted, and playback keeps working throughout),
// so a modal would overstate it — but it is hours of work on a large library,
// which is more than a single click should start.
function ReprobeRow({ available }: { available: boolean }) {
  const status = useProbeStatus(available);
  const reprobe = useReprobe();
  const [confirming, setConfirming] = useState(false);
  const [queued, setQueued] = useState<number | null>(null);

  const running = status.data?.running ?? false;
  const busy = reprobe.isPending || running;

  function run(scope: "incomplete" | "all") {
    setConfirming(false);
    reprobe.mutate(scope, {
      onSuccess: (r) => setQueued(r.queued),
    });
  }

  let sub: string;
  if (!available) {
    sub = "Install ffmpeg to inspect your files.";
  } else if (running) {
    const { probed = 0, total = 0 } = status.data ?? {};
    sub = total > 0 ? `Reading files — ${probed} of ${total}.` : "Reading files…";
  } else if (reprobe.isError) {
    sub = (reprobe.error as Error).message;
  } else if (queued === 0) {
    sub = "Nothing to re-read — every file is already up to date.";
  } else if (queued !== null) {
    sub = `Queued ${queued} file${queued === 1 ? "" : "s"}.`;
  } else {
    sub =
      "Re-read your files with the current version of LANcast. Worth doing after an upgrade: files inspected by an older version can be missing detail that playback decisions depend on.";
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Re-read media files</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <span className="set-confirm">
              Runs on every file — this can take hours.
            </span>
            <button className="set-btn" onClick={() => run("all")}>
              Re-read everything
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <button
              className="set-btn"
              disabled={!available || busy}
              onClick={() => run("incomplete")}
            >
              Only what's missing
            </button>
            <button
              className="set-btn"
              disabled={!available || busy}
              onClick={() => setConfirming(true)}
            >
              Everything
            </button>
          </>
        )}
      </div>
    </div>
  );
}

const SIGNER_LABEL: Record<string, string> = {
  first_party: "First-party",
  pinned: "Pinned",
  unsigned: "Unsigned",
};

// A short, human list of a capability set for the approval dialog and rows.
function capSummary(caps: { http: string[]; secrets: string[] }): string[] {
  const lines: string[] = [];
  for (const h of caps.http) lines.push(`Reach ${h}`);
  for (const s of caps.secrets) lines.push(`Read your ${s.replace(/_/g, " ")}`);
  return lines;
}

// The capability-approval dialog: the whole point of the two-step install. It
// states plainly what a staged plugin wants before the operator grants it.
function GrantDialog({
  plugin,
  onClose,
}: {
  plugin: Plugin;
  onClose: () => void;
}) {
  const grant = useGrantPlugin();
  const wants = capSummary(plugin.requested);
  return (
    <div className="addon-dialog__scrim" role="dialog" aria-modal="true">
      <div className="addon-dialog">
        <div className="addon-dialog__title">
          Grant <strong>{plugin.name}</strong>?
        </div>
        <div className="addon-dialog__signer">
          {SIGNER_LABEL[plugin.signer] ?? plugin.signer}
          {plugin.signer === "unsigned" && " — you accept the risk"}
        </div>
        <p className="addon-dialog__lead">This add-on is asking to:</p>
        {wants.length > 0 ? (
          <ul className="addon-dialog__caps">
            {wants.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        ) : (
          <p className="addon-dialog__caps">Nothing beyond running — no network, no secrets.</p>
        )}
        <div className="addon-dialog__actions">
          <button
            className="set-btn"
            onClick={onClose}
            disabled={grant.isPending}
          >
            Cancel
          </button>
          <button
            className="set-btn set-btn--primary"
            disabled={grant.isPending}
            onClick={() =>
              grant.mutate(
                { name: plugin.name, caps: plugin.requested },
                { onSuccess: onClose },
              )
            }
          >
            Grant &amp; enable
          </button>
        </div>
        {grant.isError && (
          <span className="set-error">{(grant.error as Error).message}</span>
        )}
      </div>
    </div>
  );
}

function AddonRow({ plugin }: { plugin: Plugin }) {
  const setEnabled = useSetPluginEnabled();
  const remove = useRemovePlugin();
  const [confirming, setConfirming] = useState(false);
  const granted = capSummary(plugin.granted);
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          {plugin.name}{" "}
          <span className="addon-version">v{plugin.version}</span>{" "}
          <span className={"addon-signer addon-signer--" + plugin.signer}>
            {SIGNER_LABEL[plugin.signer] ?? plugin.signer}
          </span>
        </div>
        <div className="set-row__sub">
          {plugin.enabled ? "Enabled" : "Disabled"}
          {granted.length > 0 ? " · " + granted.join(" · ") : " · no capabilities"}
        </div>
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          onClick={() =>
            setEnabled.mutate({ name: plugin.name, enabled: !plugin.enabled })
          }
          disabled={setEnabled.isPending}
        >
          {plugin.enabled ? "Disable" : "Enable"}
        </button>
        {confirming ? (
          <>
            <span className="set-confirm">Remove?</span>
            <button
              className="set-btn set-btn--danger"
              onClick={() => remove.mutate(plugin.name)}
              disabled={remove.isPending}
            >
              Yes
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              No
            </button>
          </>
        ) : (
          <button
            className="set-btn set-btn--danger"
            onClick={() => setConfirming(true)}
          >
            Remove
          </button>
        )}
      </div>
    </div>
  );
}

function AddonsSection() {
  const { data: plugins } = usePlugins();
  const upload = useUploadPlugin();
  const fileRef = useRef<HTMLInputElement>(null);
  const [staged, setStaged] = useState<Plugin | null>(null);

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-selecting the same file
    if (!file) return;
    const buf = await file.arrayBuffer();
    upload.mutate(buf, { onSuccess: (p) => setStaged(p) });
  };

  return (
    <section className="settings__section">
      <span className="section-label">Add-ons</span>

      {plugins && plugins.length > 0 ? (
        <div className="set-lib">
          {plugins.map((p) => (
            <AddonRow key={p.name} plugin={p} />
          ))}
        </div>
      ) : (
        <p className="set-row__sub">No add-ons installed.</p>
      )}

      <input
        ref={fileRef}
        type="file"
        accept=".lcplugin"
        onChange={onFile}
        style={{ display: "none" }}
      />
      <button
        className="set-btn"
        onClick={() => fileRef.current?.click()}
        disabled={upload.isPending}
      >
        {upload.isPending ? "Verifying…" : "Install add-on…"}
      </button>
      {upload.isError && (
        <span className="set-error">{(upload.error as Error).message}</span>
      )}

      {staged && <GrantDialog plugin={staged} onClose={() => setStaged(null)} />}
    </section>
  );
}

export function Settings() {
  const isAdmin = useIsAdmin();

  return (
    <div className="settings">
      <h1 className="settings__title">Settings</h1>
      {isAdmin && (
        <>
          <AdminSections />
          <AddonsSection />
          <UsersSection />
        </>
      )}
      <AccountSection />
      <KeyboardSection />
    </div>
  );
}
