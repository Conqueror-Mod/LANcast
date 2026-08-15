import { useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useClearCache,
  useResetSettings,
  useProbeStatus,
  useReprobe,
  useHealth,
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
  useServerLog,
} from "@/api/hooks";
import { KeyBindings } from "@/components/KeyBindings";
import { CrashReports } from "@/components/CrashReports";
import { useBigscreen } from "@/lib/bigscreen";
import { AuditLog } from "@/components/AuditLog";
import { UpdateSettings } from "@/components/UpdateSettings";
import { DesktopSettings } from "@/components/DesktopSettings";
import { ApiFailure } from "@/api/client";
import type {
  AuthUser,
  Plugin,
  Settings as SettingsType,
  SettingsUpdate,
} from "@/api/types";
import { LibraryRow, AddLibrary } from "@/components/LibrarySettings";
import "./Settings.css";



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
// Each of these is its own pane now, so the component renders one at a time.
// Kept as one component rather than three because all three read the same two
// queries — splitting would triple the fetches to save a prop.
function AdminSections({ pane }: { pane: string }) {
  const { data: libraries } = useLibraries();
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();

  return (
    <>
      {pane === "libraries" && (
      <section className="settings__section">
        <span className="section-label">Libraries</span>
        {libraries?.map((lib) => (
          <LibraryRow key={lib.id} library={lib} />
        ))}
        <AddLibrary />

        {settings && (
          <>
            <RuleSelect
              title="Rescan libraries automatically"
              sub="LANcast scans when you ask it to and when a library is added. A timer is for a server whose media arrives by other means — a downloader, a sync job, another machine writing to the drive. A library already scanning is skipped, never queued."
              value={settings.scan_interval_hours}
              options={[
                { value: 0, label: "Never" },
                { value: 1, label: "Hourly" },
                { value: 6, label: "Every 6 hours" },
                { value: 12, label: "Every 12 hours" },
                { value: 24, label: "Daily" },
                { value: 168, label: "Weekly" },
              ]}
              onChange={(v) => update.mutate({ scan_interval_hours: v })}
            />
            {/* The switch that decides whether this server can destroy media at
                all. Off is a real answer, and it was not available before:
                every install could delete files from disk through the API. */}
            <label className="set-toggle">
              <input
                type="checkbox"
                checked={settings.allow_media_deletion}
                onChange={(e) =>
                  update.mutate({ allow_media_deletion: e.target.checked })
                }
              />
              Allow deleting media files from disk
            </label>
            <div className="set-row__sub set-row__sub--standalone">
              When off, removing a title takes it out of the library and leaves
              every file where it is. Nothing on this server can then delete
              your media.
            </div>
          </>
        )}
      </section>
      )}

      {pane === "metadata" && (
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
      )}

      {pane === "playback" && (
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

        {settings && (
          <>
            {/* Applied by the server on every progress write, so every client
                agrees about what is finished — the reason this is here and not
                in each player. */}
            <RuleSelect
              title="Counts as watched at"
              sub="Stop past this much of a film or episode and it is finished. Credits are not the film, and a shelf that keeps offering the last ninety seconds back is a shelf nobody clears."
              value={settings.watched_threshold}
              options={[
                { value: 70, label: "70%" },
                { value: 80, label: "80%" },
                { value: 85, label: "85%" },
                { value: 90, label: "90%" },
                { value: 95, label: "95%" },
                { value: 100, label: "100% — only at the end" },
              ]}
              onChange={(v) => update.mutate({ watched_threshold: v })}
            />
            <RuleNumber
              title="Weeks to keep in Continue Watching"
              sub="Anything untouched for longer drops off the shelf. 0 keeps everything for ever — the half hour of a documentary you abandoned in March is not something you are in the middle of, and it pushes out what you paused last night."
              value={settings.continue_weeks}
              min={0}
              max={520}
              onCommit={(v) => update.mutate({ continue_weeks: v })}
            />
            <RuleNumber
              title="Items in Continue Watching"
              sub="How many the shelf holds at most. A client may ask for fewer; it cannot ask for more."
              value={settings.continue_limit}
              min={1}
              max={100}
              onCommit={(v) => update.mutate({ continue_limit: v })}
            />
          </>
        )}
      </section>
      )}

    </>
  );
}

/*
 * A server rule with a fixed set of answers.
 *
 * A select rather than a free number field, for the settings where the useful
 * values are few and the useless ones are harmful: a watched threshold of 3%
 * marks a library watched, and nobody wanted 3%. Where a range genuinely is a
 * range (weeks, items) the field is a number and the server validates it —
 * these are the ones where the list *is* the vocabulary.
 */
function RuleSelect({
  title,
  sub,
  value,
  options,
  onChange,
  disabled,
}: {
  title: string;
  sub: string;
  value: number;
  options: { value: number; label: string }[];
  onChange: (v: number) => void;
  disabled?: boolean;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{title}</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        <select
          className="set-select"
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(Number(e.target.value))}
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/*
 * A server rule that is a number in a range.
 *
 * Committed on blur and on Enter rather than per keystroke: saving "1" on the
 * way to typing "16" is a write that means nothing, and with continue_weeks it
 * is a write that briefly empties somebody's shelf. Out-of-range input is put
 * back rather than sent — the server rejects it anyway, and a field that snaps
 * back says so faster than a toast.
 */
function RuleNumber({
  title,
  sub,
  value,
  min,
  max,
  onCommit,
}: {
  title: string;
  sub: string;
  value: number;
  min: number;
  max: number;
  onCommit: (v: number) => void;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{title}</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input"
          type="number"
          min={min}
          max={max}
          // Uncontrolled, keyed on the server's value: the field is the user's
          // while they are typing in it, and the server's again once they are
          // done. A controlled input here fights every keystroke.
          key={value}
          defaultValue={value}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          }}
          onBlur={(e) => {
            const v = Number(e.target.value);
            if (Number.isInteger(v) && v >= min && v <= max && v !== value) {
              onCommit(v);
            } else {
              e.target.value = String(value);
            }
          }}
        />
      </div>
    </div>
  );
}

/*
 * Display — the device's own view of the app.
 *
 * A device pane rather than a server one, and that is the substance of the
 * choice: "make everything larger because I am ten feet away" is a fact about
 * the room this device is in. Syncing it would shrink the phone in somebody's
 * hand because the television downstairs is a television.
 */
function DisplaySection() {
  const [bigscreen, setBigscreen] = useBigscreen();

  return (
    <section className="settings__section">
      <span className="section-label">Display</span>

      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={bigscreen}
          onChange={(e) => setBigscreen(e.target.checked)}
        />
        <span>
          <strong>Bigscreen mode</strong>
          <span className="set-toggle__desc">
            Scales the whole interface for a television across the room. The
            same screens, larger — not a separate client. Applies on this device
            only, survives a restart, and toggles with{" "}
            <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>B</kbd> from anywhere, so you
            can get back out without finding this page again.
          </span>
        </span>
      </label>
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
// not destructive — nothing is deleted, and playback keeps working throughout —
// so a modal would overstate it, but it is still real work across every file
// and more than a single stray click should start.
//
// Measured: 225 files on local storage took ~15s. The cost scales with file
// count and storage speed, not library size in bytes, so the warning says "a
// few minutes" rather than naming a number this component cannot know.
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
    // .set-row__sub is a single ellipsised line (nowrap), so this has to stay
    // short enough to read in full. The title carries the "what"; this carries
    // the only thing not obvious from it — when you would want to.
    sub = "Worth doing after an upgrade.";
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
              Runs on every file — a few minutes for most libraries.
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

// The server log, read from the UI. It has been written beside the database
// since v0.4.2 and until now could only be read by finding the data directory
// in a file manager — which is the wrong ask for the case it exists to serve,
// because the log matters most when the server runs as a service and something
// is wrong.
//
// Collapsed by default and never polled: this is what already happened, and the
// button that opens it is the refresh.
function ServerLogSection() {
  const [open, setOpen] = useState(false);
  const { data, isFetching, error, refetch } = useServerLog(open);
  const lines = data?.lines ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Server log</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{data?.path ?? "lancastd.log"}</div>
          <div className="set-row__sub">
            Written beside the database. A server running as a service has no
            console, so this is the only record of why it stopped.
          </div>
        </div>
        <div className="set-row__actions">
          {open && (
            <button
              className="set-btn"
              onClick={() => refetch()}
              disabled={isFetching}
            >
              {isFetching ? "Reading…" : "Refresh"}
            </button>
          )}
          <button className="set-btn" onClick={() => setOpen((v) => !v)}>
            {open ? "Hide" : "Show log"}
          </button>
        </div>
      </div>
      {open && (
        <div className="set-log">
          {error && <p className="set-log__note">Could not read the log.</p>}
          {!error && !isFetching && lines.length === 0 && (
            <p className="set-log__note">
              The log is empty. A server that has only ever run in a terminal
              writes to the terminal instead.
            </p>
          )}
          {lines.length > 0 && (
            <>
              {/* Saying the view is partial is the difference between "this is
                  the log" and "this is the end of the log". */}
              {data && !data.complete && (
                <p className="set-log__note">
                  Showing the last {lines.length.toLocaleString()} lines. Older
                  entries are in the file.
                </p>
              )}
              <pre className="set-log__body">{lines.join("\n")}</pre>
            </>
          )}
        </div>
      )}
    </section>
  );
}


/*
 * Server identity. /api/health has returned the version since the beginning and
 * nothing ever asked for it, so a settings page could not tell you which server
 * it was configuring — the first question anyone has when something behaves
 * unexpectedly after an update.
 */
function GeneralSection() {
  const { data: health } = useHealth();
  return (
    <section className="settings__section">
      <span className="section-label">Server</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Version</div>
          <div className="set-row__sub">
            {health ? `LANcast ${health.version}` : "…"}
          </div>
        </div>
      </div>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">API version</div>
          <div className="set-row__sub">
            {/* Shown because it is the number a third-party client is built
                against (ADR 0018), and the only place it was visible before was
                a response header nobody reads. */}
            {health ? health.api_version : "…"}
          </div>
        </div>
      </div>
    </section>
  );
}

/*
 * Two settings the server has always accepted and validated, and that no
 * control has ever reached: the provider rate limit and the update check. Both
 * were editable only by hand-editing config.json — which is the same
 * "capability with no path to it" that hid track deletion, arrived at from the
 * configuration side rather than the UI side.
 *
 * Separate sections rather than surgery inside AdminSections. They re-read the
 * same queries, which react-query serves from one cache entry, so the cost of
 * keeping the edit contained is nothing.
 */
function RateLimitSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  if (!settings) return null;
  return (
    <section className="settings__section">
      <span className="section-label">Provider requests</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Rate limit</div>
          <div className="set-row__sub">
            Requests per second to TMDB, OMDb and OpenSubtitles. Lower this if a
            provider starts refusing; the server rejects anything above 50.
          </div>
        </div>
        <div className="set-row__actions">
          <input
            className="set-input set-input--num"
            type="number"
            min={1}
            max={50}
            step={1}
            defaultValue={settings.rate_per_sec}
            aria-label="Provider requests per second"
            // On blur rather than on change: this is a number field, and
            // committing every keystroke would PATCH "1" on the way to "12"
            // and briefly throttle the server to a crawl.
            onBlur={(e) => {
              const v = Number(e.target.value);
              if (v >= 1 && v <= 50 && v !== settings.rate_per_sec) {
                update.mutate({ rate_per_sec: v });
              } else {
                e.target.value = String(settings.rate_per_sec);
              }
            }}
          />
        </div>
      </div>
    </section>
  );
}

function UpdateCheckSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  if (!settings) return null;
  return (
    <section className="settings__section">
      <span className="section-label">Update checks</span>
      <label className="set-toggle">
        <input
          type="checkbox"
          checked={settings.update_check}
          onChange={(e) => update.mutate({ update_check: e.target.checked })}
        />
        Check for new LANcast releases
      </label>
      <div className="set-row__sub">
        Turning this off stops the server contacting GitHub. Nothing else here
        reaches the internet on its own.
      </div>
    </section>
  );
}

/*
 * The settings shell.
 *
 * Everything used to be one column: eight sections stacked in a single scroll,
 * so finding the log meant passing every library, every provider key and every
 * user on the way down, and the page got longer with each release. Plex answers
 * this with a vertical list of categories on the left, and the answer is right —
 * settings are looked up, not read through.
 *
 * The pane lives in the URL, like every other view state in this client, so a
 * category is linkable and survives a reload.
 *
 * Two groups, split by who the setting belongs to rather than by subject: the
 * server, which is shared and admin-only, and this device, which is yours and
 * affects nobody else. That distinction is the one that actually matters when
 * two people use the same server, and it is invisible in a flat list.
 */
interface Pane {
  id: string;
  label: string;
  admin?: boolean;
}

const SERVER_PANES: Pane[] = [
  { id: "general", label: "General", admin: true },
  { id: "libraries", label: "Libraries", admin: true },
  { id: "metadata", label: "Metadata", admin: true },
  { id: "playback", label: "Playback", admin: true },
  { id: "users", label: "Users", admin: true },
  { id: "addons", label: "Add-ons", admin: true },
  { id: "updates", label: "Updates", admin: true },
  { id: "activity", label: "Activity", admin: true },
  { id: "logs", label: "Logs", admin: true },
];

const DEVICE_PANES: Pane[] = [
  { id: "account", label: "Account" },
  { id: "app", label: "This app" },
  { id: "display", label: "Display" },
  { id: "keyboard", label: "Keyboard" },
];

// DesktopSettings renders nothing in a browser tab — there is no tray to reduce
// to and no close button LANcast owns. Offering the category anyway would give a
// browser user a heading that leads to an empty column, which is the same fault
// the player's controls avoid by appearing only when they can act. Asked the
// same way the component asks itself, so the two cannot disagree.
function desktopAvailable(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof (window as { lancastDesktopState?: unknown }).lancastDesktopState ===
      "function"
  );
}

export function Settings() {
  const isAdmin = useIsAdmin();
  const [params, setParams] = useSearchParams();

  const server = isAdmin ? SERVER_PANES : [];
  const device = DEVICE_PANES.filter(
    (x) => x.id !== "app" || desktopAvailable(),
  );
  const all = [...server, ...device];
  // An unknown or absent pane falls back to the first one the user may see,
  // rather than rendering an empty column — a link to an admin pane followed by
  // a demotion should land somewhere, not nowhere.
  const requested = params.get("pane") ?? "";
  const pane = all.some((x) => x.id === requested)
    ? requested
    : (all[0]?.id ?? "account");

  const go = (id: string) =>
    setParams(
      (prev) => {
        prev.set("pane", id);
        return prev;
      },
      { replace: true },
    );

  const navGroup = (label: string, panes: Pane[]) =>
    panes.length > 0 && (
      <div className="settings__navgroup">
        <span className="section-label">{label}</span>
        {panes.map((x) => (
          <button
            key={x.id}
            className={
              "settings__navitem" + (pane === x.id ? " is-active" : "")
            }
            aria-current={pane === x.id ? "page" : undefined}
            onClick={() => go(x.id)}
          >
            {x.label}
          </button>
        ))}
      </div>
    );

  return (
    <div className="settings">
      <h1 className="settings__title">Settings</h1>

      <div className="settings__layout">
        <nav className="settings__nav" aria-label="Settings categories">
          {navGroup("Server", server)}
          {navGroup("This device", device)}
        </nav>

        <div className="settings__pane">
          {isAdmin && (
            <>
              {pane === "general" && <GeneralSection />}
              <AdminSections pane={pane} />
              {pane === "metadata" && <RateLimitSection />}
              {pane === "users" && <UsersSection />}
              {pane === "addons" && <AddonsSection />}
              {pane === "updates" && (
                <>
                  <UpdateSettings />
                  <UpdateCheckSection />
                </>
              )}
              {pane === "activity" && <AuditLog />}
              {pane === "logs" && (
        <>
          <ServerLogSection />
          <CrashReports />
          <MaintenanceSection />
        </>
      )}
            </>
          )}
          {pane === "account" && <AccountSection />}
          {pane === "app" && <DesktopSettings />}
          {pane === "keyboard" && <KeyBindings />}
          {pane === "display" && <DisplaySection />}
        </div>
      </div>
    </div>
  );
}

/*
 * Debug logging, and the two things it is reasonable to throw away.
 *
 * On the Logs pane because that is where somebody already is when they want
 * any of it: they came to read the log because something is wrong, and the next
 * two questions are "can I get more detail" and "can I make it stop by clearing
 * something".
 *
 * Every action here is recoverable by the server itself — cached artwork
 * re-downloads, transcode scratch is rebuilt on the next play, settings return
 * to documented defaults. Nothing touches media, the database, accounts, or
 * anything a person typed. That boundary is the feature, and it is stated on
 * the controls rather than left to be discovered.
 */
function MaintenanceSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const clear = useClearCache();
  const reset = useResetSettings();
  const [confirmReset, setConfirmReset] = useState(false);
  const [freed, setFreed] = useState<string | null>(null);

  return (
    <section className="settings__section">
      <span className="section-label">Diagnostics</span>

      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.debug_logging}
              onChange={(e) => update.mutate({ debug_logging: e.target.checked })}
            />
            Write debug detail to the log
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            Takes effect on the next line logged — no restart — and survives one,
            because the faults worth turning this on for are the intermittent
            ones. Leave it off for ordinary running: it is verbose.
          </div>
        </>
      )}

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Clear cached artwork</div>
          <div className="set-row__sub">
            Posters and backgrounds are downloaded again as they are needed.
            Nothing about your library changes; artwork is blank until it
            arrives.
            {freed && <> · {freed} freed</>}
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={clear.isPending}
            onClick={() =>
              clear.mutate("artwork", {
                onSuccess: (r) =>
                  setFreed(`${Math.round((r.freed_bytes / 1048576) * 10) / 10} MB`),
              })
            }
          >
            {clear.isPending ? "Clearing…" : "Clear"}
          </button>
        </div>
      </div>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Clear transcode scratch</div>
          <div className="set-row__sub">
            Stops anything being transcoded right now and deletes its working
            files. A few seconds of buffered video, rebuilt the moment somebody
            presses play.
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={clear.isPending}
            onClick={() => clear.mutate("transcode")}
          >
            Clear
          </button>
        </div>
      </div>

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Reset settings</div>
          <div className="set-row__sub">
            Puts every setting back to its default. Your password, provider API
            keys, certificate paths and ffmpeg location are kept — a reset
            cannot restore those, and losing the first would lock you out of
            your own server.
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            disabled={reset.isPending}
            onClick={() => {
              if (!confirmReset) {
                setConfirmReset(true);
                return;
              }
              reset.mutate(undefined, { onSuccess: () => setConfirmReset(false) });
            }}
          >
            {reset.isPending
              ? "Resetting…"
              : confirmReset
                ? "Reset everything?"
                : "Reset"}
          </button>
        </div>
      </div>
    </section>
  );
}
