import { useState } from "react";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useCreateLibrary,
  useDeleteLibrary,
  useStartScan,
  useRefreshLibrary,
  useScanStatus,
  useCurrentUser,
  useIsAdmin,
  useUsers,
  useCreateUser,
  useDeleteUser,
  useResetUserPassword,
  useChangePassword,
} from "@/api/hooks";
import { DirectoryPicker } from "@/components/DirectoryPicker";
import { ApiFailure } from "@/api/client";
import type { AuthUser, Library } from "@/api/types";
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

export function Settings() {
  const isAdmin = useIsAdmin();

  return (
    <div className="settings">
      <h1 className="settings__title">Settings</h1>
      {isAdmin && (
        <>
          <AdminSections />
          <UsersSection />
        </>
      )}
      <AccountSection />
      <KeyboardSection />
    </div>
  );
}
