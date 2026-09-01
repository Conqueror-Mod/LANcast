import { useEffect, useRef, useState } from "react";
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
  useSetSharing,
  useChannelSources,
  useAddChannelSource,
  useRefreshChannelSource,
  useDeleteChannelSource,
  useSetGuideURL,
  useRenameSelf,
  useUpdateUser,
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
  useMediaTools,
  useInstallMediaTools,
  useCancelMediaToolsInstall,
  useScanAllLibraries,
  useHistoryCount,
  useResetHistory,
  type HistoryScope,
  useFaceCapabilities,
  useFaceModels,
  useInstallFaceModels,
  useCancelFaceModels,
} from "@/api/hooks";
import {
  capabilities,
  claimable,
  clearDenials,
  deniedCapabilities,
  restore,
  withhold,
  withheldCapabilities,
} from "@/playback/capabilities";
import { KeyBindings } from "@/components/KeyBindings";
import { CrashReports } from "@/components/CrashReports";
import { useBigscreen } from "@/lib/bigscreen";
import { useSpoilerMode, type SpoilerMode } from "@/lib/spoilers";
import { useLiveTransport } from "@/lib/liveTransport";
import { AuditLog } from "@/components/AuditLog";
import { Review } from "./Review";
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
  const update = useUpdateUser();
  const [resetting, setResetting] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [newName, setNewName] = useState(user.name);
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
          {/* The server refuses to demote the last administrator — inside a
              transaction with the count, because two admins demoting each other
              at once is a race a client-side check cannot win. This surfaces
              that refusal rather than pretending it cannot happen. */}
          {update.isError && (
            <div className="set-row__note set-row__note--warn">
              <strong>{(update.error as Error).message}</strong>
            </div>
          )}
        </div>
        <div className="set-row__actions">
          {renaming ? (
            <>
              <input
                className="set-input"
                autoFocus
                value={newName}
                maxLength={60}
                onChange={(e) => setNewName(e.target.value)}
                aria-label={`New name for ${user.name}`}
              />
              <button
                className="set-btn"
                disabled={!newName.trim() || update.isPending}
                onClick={() =>
                  update.mutate(
                    { id: user.id, name: newName.trim() },
                    { onSuccess: () => setRenaming(false) },
                  )
                }
              >
                Save
              </button>
              <button className="set-btn" onClick={() => setRenaming(false)}>
                Cancel
              </button>
            </>
          ) : resetting ? (
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
              <button
                className="set-btn"
                onClick={() => {
                  setNewName(user.name);
                  setRenaming(true);
                }}
              >
                Rename
              </button>
              {/*
                Promotion and demotion in one button, because they are one
                decision with two directions and a pair of buttons would leave
                one of them permanently inert.

                Offered for yourself too: an admin demoting themselves is a
                legitimate act on a household server, and the server refuses it
                only when they are the last one — which is a different rule from
                "not yourself" and the one that actually protects the install.
              */}
              <button
                className="set-btn"
                disabled={update.isPending}
                onClick={() =>
                  update.mutate({
                    id: user.id,
                    role: user.role === "admin" ? "member" : "admin",
                  })
                }
              >
                {user.role === "admin" ? "Make member" : "Make admin"}
              </button>
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
/*
 * Your own display name.
 *
 * Separate from the Users pane on purpose: renaming yourself affects nobody and
 * needs no role, while renaming somebody else is administration. Two surfaces
 * because they answer to different authority, not because the form is different.
 *
 * The account id does not change, so watch history, ratings and playlist
 * membership follow silently — which is the whole reason this is a rename
 * rather than "delete and make a new one".
 */
function DisplayNameForm() {
  const me = useCurrentUser();
  const rename = useRenameSelf();
  const [name, setName] = useState("");
  const [saved, setSaved] = useState(false);

  // Seeded from the current name once it arrives, rather than left empty: an
  // empty box beside "Display name" reads as though the name has been lost.
  useEffect(() => {
    if (me?.name) setName(me.name);
  }, [me?.name]);

  const unchanged = !name.trim() || name.trim() === me?.name;

  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        rename.mutate(name.trim(), { onSuccess: () => setSaved(true) });
      }}
    >
      <span className="set-sublabel">Display name</span>
      <input
        className="set-input"
        value={name}
        maxLength={60}
        onChange={(e) => {
          setName(e.target.value);
          setSaved(false);
        }}
        aria-label="Display name"
      />
      <button className="set-btn" disabled={unchanged || rename.isPending}>
        {rename.isPending ? "Saving…" : "Save name"}
      </button>
      {saved && <span className="set-note">Saved.</span>}
      {rename.isError && (
        <span className="set-error">{(rename.error as Error).message}</span>
      )}
    </form>
  );
}

/*
 * Whether other people on this server can see what you have watched (ADR 0035).
 *
 * In Account rather than in Users because it is *yours*: an administrator may
 * run the server, and a switch somebody else can flip on your behalf is not
 * consent. There is no admin-facing version of this control, and the server has
 * no route that would allow one.
 *
 * The wording says what is shared and what is not, because "share activity" on
 * its own invites people to assume the worst — or, worse, the best.
 */
function SharingToggle() {
  const set = useSetSharing();
  const me = useCurrentUser();
  /*
   * The stored value comes from auth status, which is the only route that
   * reports the caller's own setting — `/api/people` excludes the caller by
   * design, so the previous version of this looked for itself in a list that
   * structurally could not contain it, found nothing, and fell back to `false`
   * on every mount. The toggle read as off for somebody who had opted in.
   *
   * The local override is still here, but only to keep the checkbox responsive
   * between the click and the refetch. It is cleared as soon as the server's
   * answer changes, so the server stays the thing being displayed rather than
   * a guess that happens to agree with it.
   */
  const stored = me?.sharing ?? false;
  const [pending, setPending] = useState<boolean | null>(null);
  useEffect(() => {
    setPending(null);
  }, [stored]);
  const on = pending ?? stored;

  return (
    <section className="settings__section">
      <span className="section-label">Sharing</span>
      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={on}
          onChange={(e) => {
            setPending(e.target.checked);
            set.mutate(e.target.checked);
          }}
        />
        <span>
          <strong>Let others here see what I have watched</strong>
          <span className="set-toggle__desc">
            Shares the titles you have <em>finished</em>, and when — with the
            other people on this server, on the People page. It does not share
            your ratings or notes, which stay private always, or where you have
            got to in something. Off unless you turn it on, and turning it off
            hides what you already shared.
          </span>
        </span>
      </label>
      {set.isError && (
        <span className="set-error">{(set.error as Error).message}</span>
      )}
    </section>
  );
}

/*
 * Live TV channel sources.
 *
 * Admin-gated for a stronger reason than most panes here: adding a source makes
 * *the server* fetch a URL somebody typed, which is server-side request forgery
 * in miniature. The server refuses its own address; everything else on the
 * network is allowed, because a tuner on the same machine is the ordinary case.
 */
function LiveTVSection() {
  const { data } = useChannelSources(true);
  const add = useAddChannelSource();
  const refresh = useRefreshChannelSource();
  const remove = useDeleteChannelSource();
  const setGuide = useSetGuideURL();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  // Two guide-URL fields, two states. Sharing one was the first shape and it
  // leaked: opening a source's guide editor prefilled the *add* form with that
  // source's URL, so the next list added inherited somebody else's guide.
  const [epgUrl, setEpgUrl] = useState("");
  const [newEpgUrl, setNewEpgUrl] = useState("");
  const [note, setNote] = useState<string | null>(null);
  // Which source's guide field is open. One at a time: this is a rarely-typed
  // URL, and a row of permanently-visible inputs makes the common case (look at
  // the list, refresh one) noisier for the sake of the rare one.
  const [editing, setEditing] = useState<number | null>(null);

  const sources = data?.sources ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Live TV</span>
      <p className="set-row__note">
        A channel list is an M3U — from an IPTV provider, or from a tuner on
        this network. Channels are played through this server, so the list URL
        and anything in it stays here.
      </p>
      <p className="set-row__note">
        A guide is a separate XMLTV file, plain or gzipped. Listings attach to
        channels by <code>tvg-id</code>, so a channel list that does not carry
        one gets no guide — there is no reliable way to match by name. Guides
        refresh by themselves every twelve hours.
      </p>

      {sources.map((src) => (
        <div className="set-lib" key={src.id}>
          <div className="set-row">
            <div className="set-row__main">
              <div className="set-row__title">{src.name}</div>
              <div className="set-row__sub">
                {src.channel_count.toLocaleString()} channels
                {src.refreshed_at
                  ? ` · refreshed ${new Date(src.refreshed_at * 1000).toLocaleDateString()}`
                  : " · never refreshed"}
                {/* A guide is either configured and working, configured and
                    empty, or absent — and those want three different next
                    actions, so they are three different sentences. */}
                {src.epg_url
                  ? ` · ${src.program_count.toLocaleString()} listings`
                  : " · no guide"}
              </div>
            </div>
            <div className="set-row__actions">
              <button
                className="set-btn"
                onClick={() => {
                  setEditing(editing === src.id ? null : src.id);
                  setEpgUrl(src.epg_url ?? "");
                }}
              >
                Guide
              </button>
              <button
                className="set-btn"
                disabled={refresh.isPending}
                onClick={() => refresh.mutate(src.id)}
              >
                {refresh.isPending ? "Refreshing…" : "Refresh"}
              </button>
              <button
                className="set-btn set-btn--danger"
                disabled={remove.isPending}
                onClick={() => remove.mutate(src.id)}
              >
                Remove
              </button>
            </div>
          </div>

          {editing === src.id && (
            <form
              className="set-add"
              onSubmit={(e) => {
                e.preventDefault();
                setNote(null);
                setGuide.mutate(
                  { id: src.id, epg_url: epgUrl.trim() },
                  {
                    onSuccess: (res) => {
                      setEditing(null);
                      setNote(
                        res.epg_error
                          ? `The guide could not be read: ${res.epg_error}`
                          : epgUrl.trim() === ""
                            ? "Guide removed."
                            : `Imported ${res.programs ?? 0} listings.`,
                      );
                    },
                  },
                );
              }}
            >
              <input
                className="set-input"
                placeholder="https://…/guide.xml or .xml.gz"
                value={epgUrl}
                onChange={(e) => setEpgUrl(e.target.value)}
                aria-label="XMLTV guide URL"
              />
              <button className="set-btn" disabled={setGuide.isPending}>
                {setGuide.isPending ? "Importing…" : "Save guide"}
              </button>
            </form>
          )}
        </div>
      ))}

      <form
        className="set-add"
        onSubmit={(e) => {
          e.preventDefault();
          setNote(null);
          add.mutate(
            { name: name.trim(), url: url.trim(), epg_url: newEpgUrl.trim() },
            {
              onSuccess: (res) => {
                setName("");
                setUrl("");
                setNewEpgUrl("");
                // A source whose import failed is kept, because the URL may be
                // right and the provider down. Saying which it was is the whole
                // value of the message.
                setNote(
                  res.import_error
                    ? `Added, but the list could not be read: ${res.import_error}`
                    : res.epg_error
                      ? `Added ${res.channels ?? 0} channels, but the guide could not be read: ${res.epg_error}`
                      : `Added ${res.channels ?? 0} channels` +
                        (res.programs ? ` and ${res.programs} listings.` : "."),
                );
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
          className="set-input"
          placeholder="https://…/playlist.m3u"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          required
        />
        <button className="set-btn" disabled={add.isPending}>
          {add.isPending ? "Importing…" : "Add channel list"}
        </button>
      </form>
      {note && <span className="set-note">{note}</span>}
      {add.isError && (
        <span className="set-error">{(add.error as Error).message}</span>
      )}
    </section>
  );
}

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

      <DisplayNameForm />

      <SharingToggle />

      <HistoryReset />

      <span className="set-sublabel">Password</span>
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
          <ScanEverything />

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
                When off, removing a title takes it out of the library and
                leaves every file where it is. Nothing on this server can then
                delete your media.
              </div>

              {/*
                Emptying the trash, which is about *rows* and not files.
                Deliberately below the deletion switch and worded to keep the
                two apart: one destroys media, this destroys the record of media
                that is already gone.
              */}
              <label className="set-toggle">
                <input
                  type="checkbox"
                  checked={settings.empty_trash_on_scan ?? false}
                  onChange={(e) =>
                    update.mutate({ empty_trash_on_scan: e.target.checked })
                  }
                />
                Forget missing files after a scan
              </label>
              <div className="set-row__sub set-row__sub--standalone">
                A file that has gone leaves its entry behind, marked missing, so
                a drive that failed to mount does not cost you a library. Turn
                this on and a scan removes those entries — along with their
                watch history, positions and ratings, which is what makes it
                worth asking about rather than doing quietly.
                <br />
                A scan that could not read a location, or that saw no files at
                all, leaves them alone whatever this says: an empty walk is a
                statement about the walk and not about the library.
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
                  onChange={(e) =>
                    update.mutate({ write_nfo: e.target.checked })
                  }
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
/*
 * Scanning every library, in one press.
 *
 * The capability was never missing — the rescan timer has always looped every
 * library — it was only ever unreachable by hand. On a five-library server
 * "check for new media" meant pressing Scan five times, which is the kind of
 * chore people quietly stop doing, and then wonder why nothing new appears.
 *
 * It sits with the timer rather than above the library list because it belongs
 * to the same idea: this is the whole-server half of scanning, and the timer is
 * the automatic version of exactly this button.
 *
 * The result says both halves. A sweep that started nothing because every
 * library was already scanning is indistinguishable from a sweep that did
 * nothing at all, and "Nothing to do" is the wrong answer to both.
 */
function ScanEverything() {
  const scanAll = useScanAllLibraries();
  /*
   * Both lists defaulted rather than trusted.
   *
   * A settings pane that throws takes the whole screen with it, and the thing
   * it would be throwing over is a 202 whose body did not arrive in the shape
   * this component assumed. Reading "no libraries to scan" from an odd answer
   * is wrong in a way somebody can recover from; a white screen is not.
   */
  const started = scanAll.data?.started ?? [];
  const busy = scanAll.data?.busy ?? [];
  const answered = scanAll.isSuccess;

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Scan every library</div>
        <div className="set-row__sub">
          Walks every location for new, changed and missing files — the same
          thing the timer below does, when you would rather not wait for it.
        </div>
        {scanAll.isError && (
          <p className="set-error">
            Could not start: {(scanAll.error as Error).message}
          </p>
        )}
        {answered && (
          <p className="set-note">
            {started.length > 0 && (
              <>
                Scanning {started.length}{" "}
                {started.length === 1 ? "library" : "libraries"}.
              </>
            )}
            {started.length > 0 && busy.length > 0 && " "}
            {busy.length > 0 && (
              <>
                {busy.length} {busy.length === 1 ? "was" : "were"} already
                scanning and {busy.length === 1 ? "was" : "were"} left to
                finish.
              </>
            )}
            {started.length === 0 && busy.length === 0 && (
              <>There are no libraries to scan yet.</>
            )}
          </p>
        )}
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={scanAll.isPending}
          onClick={() => scanAll.mutate()}
        >
          {scanAll.isPending ? "Starting…" : "Scan all"}
        </button>
      </div>
    </div>
  );
}

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
  const [spoilers, setSpoilers] = useSpoilerMode();
  const [liveTransport, setLiveTransport] = useLiveTransport();

  return (
    <section className="settings__section">
      <span className="section-label">Display</span>

      {/*
       * Live TV transport, and it is deliberately a device setting rather than
       * a server one: what a browser can do with MediaSource is a fact about
       * this machine, not about the library.
       *
       * Off by default, and staying that way until the new path has been lived
       * with — step 4 of the ADR 0013 amendment. The old path is one toggle
       * away for exactly as long as that takes.
       */}
      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={liveTransport === "mse"}
          onChange={(e) =>
            setLiveTransport(e.target.checked ? "mse" : "progressive")
          }
        />
        <span>
          <strong>Improved live TV playback</strong>
          <span className="set-toggle__desc">
            Plays channels through a segmented stream instead of one long
            response, which lets the player see how much it is holding rather
            than guessing. It should stutter less and stop drifting behind live.
            New, and off by default — if a channel behaves worse with this on,
            turn it off and it will play the way it always has. Applies on this
            device only.
          </span>
        </span>
      </label>

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
            only, survives a restart, and toggles with <kbd>Ctrl</kbd>{" "}
            <kbd>Shift</kbd> <kbd>B</kbd> from anywhere, so you can get back out
            without finding this page again.
          </span>
        </span>
      </label>

      {/*
       * Spoilers, in the device pane for the same reason bigscreen is: there is
       * no per-user preference store on the server, and inventing one for a
       * checkbox would be a schema decision made by a checkbox. Somebody who
       * watches on two machines sets it twice, which is honest and small.
       */}
      <label className="set-row set-row--stacked">
        <div className="set-row__main">
          <div className="set-row__title">Spoilers on a season page</div>
          <div className="set-row__sub">
            An episode's synopsis is written as a summary, not a tease, so the
            next one down the list can give away what you were about to watch.
            This applies only to episodes you have not started — two minutes in,
            you have already met whatever the first scene gives away.
          </div>
        </div>
        <div className="set-row__actions">
          <select
            className="set-input"
            value={spoilers}
            onChange={(e) => setSpoilers(e.target.value as SpoilerMode)}
            aria-label="Spoilers on a season page"
          >
            <option value="synopsis">Hide the synopsis</option>
            <option value="all">Hide the synopsis and the still</option>
            <option value="show">Show everything</option>
          </select>
        </div>
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
  /*
   * The install job, polled only while it matters.
   *
   * Enabled when the tools are missing or a job is running: once ffmpeg is
   * present there is nothing to watch, and a settings page that polls forever
   * for a finished download is a page that costs a request a second for nothing.
   */
  const job = useMediaTools(!ok);
  const install = useInstallMediaTools();
  const cancel = useCancelMediaToolsInstall();
  const state = job.data;
  const src = state?.available_source;
  const running = !!state?.running;
  const pct =
    state && state.bytes_total > 0
      ? Math.min(100, Math.round((state.bytes_done / state.bytes_total) * 100))
      : 0;
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          Media tools{" "}
          <span
            className={
              "addon-signer addon-signer--" + (ok ? "first_party" : "unsigned")
            }
          >
            {ok ? "found" : "missing"}
          </span>
        </div>
        <div className="set-row__sub">
          {ok
            ? `ffmpeg and ffprobe available${tools.directory ? " · " + tools.directory : " on PATH"}`
            : "Without ffmpeg, LANcast cannot inspect or convert media — files play only if your browser already supports them. Download it below, or set the folder containing it here."}
        </div>

        {/* What is about to be downloaded, before it is: version, size and
            licence. A download the user cannot identify is not consent, and the
            licence is GPL, which is worth saying out loud rather than burying in
            an ADR. */}
        {!ok && src && !running && (
          <div className="set-row__sub">
            {`${(src.size_bytes / 1_000_000).toFixed(0)} MB · ffmpeg ${src.version} · `}
            <a href={src.licence_url} target="_blank" rel="noreferrer">
              {src.licence}
            </a>
            {" · fetched from GitHub, checksum verified before it is unpacked"}
          </div>
        )}

        {running && (
          <div className="set-row__sub" role="status">
            {state?.stage === "downloading"
              ? `Downloading… ${pct}% of ${(state.bytes_total / 1_000_000).toFixed(0)} MB`
              : state?.stage === "verifying"
                ? "Verifying the checksum…"
                : "Installing…"}
          </div>
        )}

        {/* A failed install says why and stays failed until something is done
            about it, rather than reverting to a bare "missing" that looks like
            nothing was ever attempted. */}
        {!running && state?.error && (
          <div className="set-row__sub" role="alert">
            {`That install did not finish: ${state.error}`}
          </div>
        )}
      </div>
      <div className="set-row__actions">
        {/* Downloading it is offered first, because typing a path presupposes
            having already installed ffmpeg somewhere — the step this exists to
            remove. The folder box stays for anyone who has. */}
        {!ok && src && !running && (
          <button
            className="set-btn"
            disabled={install.isPending}
            onClick={() => install.mutate()}
          >
            {install.isPending ? "Starting…" : "Download ffmpeg"}
          </button>
        )}
        {running && (
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        )}
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
    sub =
      total > 0 ? `Reading files — ${probed} of ${total}.` : "Reading files…";
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

/*
 * What this browser is claiming it can decode, and what it has stopped claiming.
 *
 * Sits under the re-probe row because it is the mirror image of it: that row is
 * what the *server* knows about your files, this is what the *client* says
 * about itself. Both go stale, and neither used to say so.
 *
 * It exists because this state was invisible and wrong at the same time. A real
 * install was found withholding every claim it is capable of making — hevc,
 * hevc10, ac3, eac3 — and had been serving a full 4K re-encode of every HEVC
 * film as a result. Clearing it made the same file direct-play with no ffmpeg.
 * Nothing had failed: falling back is correct behaviour, so the symptom was
 * only ever "this seems to work hard".
 *
 * Denials now expire on their own, so the button is the impatient path rather
 * than the only one — someone who has just installed a codec extension should
 * not wait a fortnight to learn whether it took.
 */
function CodecDenialsRow() {
  const [denials, setDenials] = useState(() => deniedCapabilities());
  const [claims, setClaims] = useState(() => capabilities());
  const [off, setOff] = useState(() => withheldCapabilities());
  const offered = claimable();

  function refresh() {
    setDenials(deniedCapabilities());
    setClaims(capabilities());
    setOff(withheldCapabilities());
  }

  function reset() {
    clearDenials();
    refresh();
  }

  function toggle(name: string, on: boolean) {
    if (on) restore(name);
    else withhold(name);
    refresh();
  }

  /*
   * Two mechanisms turn a codec off, and the summary used one word for both.
   *
   * A *denial* is automatic and temporary: recorded when a direct play fails,
   * expiring after a fortnight, cleared by the button. A *withholding* is
   * manual and permanent: the checkboxes below. The line said "Withheld after a
   * failure: hevc, ac3" — which is a denial, described with the word the
   * checkboxes own — directly above those same checkboxes, ticked.
   *
   * So the panel contradicted itself on screen: it named codecs as withheld
   * while showing them as not withheld. Both statements were true about
   * different things and there was no way to tell from reading it.
   *
   * Named rather than counted, still: "2 off" invites a shrug, and "hevc, ac3"
   * is the thing a person can connect to a film that plays badly.
   */
  const parts: string[] = [];
  if (denials.length > 0) {
    parts.push(`off after a failure: ${denials.map((d) => d.name).join(", ")}`);
  }
  if (off.length > 0) {
    parts.push(`turned off by you: ${off.join(", ")}`);
  }

  let sub: string;
  if (offered.length > 0 && !claims) {
    /*
     * The state this row was built to make visible, said outright.
     *
     * A browser that claims nothing has every affected file converted by the
     * server instead — a full re-encode of every HEVC film, on a real install,
     * for as long as it went unnoticed. Nothing fails, so the only symptom is
     * that the server seems to work hard, and the previous wording buried the
     * cause in a word that appeared to disagree with the ticks underneath it.
     */
    sub = `Claiming nothing, so the server converts anything that needs these — ${parts.join("; ")}.`;
  } else if (parts.length > 0) {
    sub = `Claiming ${claims.split(",").join(", ")}; ${parts.join("; ")}.`;
  } else if (claims) {
    sub = `Claiming ${claims.split(",").join(", ")}.`;
  } else {
    // No claims and nothing turned off is the ordinary state of a browser that
    // simply decodes none of the optional codecs, and is not a fault.
    sub = "This browser decodes none of the optional codecs.";
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Playback codecs</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {/* Only the automatic half. Clearing denials is not "turn everything
            back on": a codec somebody switched off by hand stays off, because
            that was a judgement about a picture and this button is about
            failures. The title says so, since the label cannot. */}
        <button
          className="set-btn"
          disabled={denials.length === 0}
          onClick={reset}
          title="Clears only the codecs turned off automatically after a failure. Ones you turned off stay off."
        >
          Try them again
        </button>
      </div>

      {/*
       * Turning a codec off by hand, which the automatic net cannot do.
       *
       * A denial is recorded when a direct play *fails*. A file that decodes
       * in hardware and stutters has not failed — measured on a real film:
       * HEVC Main 10 direct-played with the decode engine at 8-9%, the server
       * idle, and a juddering picture in two different browsers, while the
       * same file re-encoded played perfectly. Nothing in here can see the
       * difference between a smooth picture and a rough one. A person can.
       *
       * Only what this browser actually claims is offered: a switch for a
       * codec the engine never claimed would be a control that does nothing.
       */}
      {offered.length > 0 && (
        <div className="set-codecs">
          <div className="set-codecs__note">
            If a file plays but stutters, the browser is claiming a codec it
            cannot really keep up with. Turn it off here and the server will
            convert those files instead.
          </div>
          {offered.map((name) => {
            const on = !off.includes(name);
            return (
              <label className="set-codecs__item" key={name}>
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) => toggle(name, e.target.checked)}
                />
                <span>{name}</span>
              </label>
            );
          })}
        </div>
      )}
    </div>
  );
}

/*
 * Forgetting what you have watched.
 *
 * On the Account pane rather than under an administrator's settings, because
 * this is one person's record and only its owner may clear it — the endpoint
 * takes no user id for exactly that reason (ADR 0006). An administrator runs
 * the server; that is not consent on somebody else's behalf.
 *
 * Three choices rather than one button, because "clear my history" means three
 * different things and `playback_state` is one table carrying two meanings.
 * Somebody forgetting a show they finished rarely means "and lose my place in
 * the one I am half way through".
 *
 * The count is fetched before anything is offered, and the confirmation says
 * it. A number is what makes an irreversible action reviewable: a person who
 * expected to clear one thing and is told four hundred has learned something
 * while it is still free. "Are you sure" teaches nobody anything.
 */
function HistoryReset() {
  const [scope, setScope] = useState<HistoryScope>("all");
  const [confirming, setConfirming] = useState(false);
  const [done, setDone] = useState<number | null>(null);
  const count = useHistoryCount(scope);
  const reset = useResetHistory();

  const n = count.data?.count ?? 0;
  const busy = reset.isPending || count.isFetching;

  let sub: string;
  if (reset.isError) {
    sub = errorMessage(reset.error);
  } else if (done !== null) {
    sub = done === 0 ? "There was nothing to forget." : `Forgot ${done}.`;
  } else if (count.isError) {
    sub = "Could not read how much history you have.";
  } else if (count.isLoading) {
    sub = "Counting…";
  } else {
    // Named rather than counted alone: "412 things" invites a shrug where
    // "412 things you have watched" is the thing about to be destroyed.
    sub = `${n} ${n === 1 ? "record" : "records"} — this cannot be undone.`;
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Clear watch history</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <span className="set-confirm">
              Forgets {n} {n === 1 ? "record" : "records"}, permanently.
            </span>
            <button
              className="set-btn"
              disabled={busy}
              onClick={() =>
                reset.mutate(
                  { scope },
                  {
                    onSuccess: () => {
                      setDone(n);
                      setConfirming(false);
                    },
                  },
                )
              }
            >
              Forget them
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <select
              className="set-input"
              value={scope}
              aria-label="What to forget"
              onChange={(e) => {
                setScope(e.target.value as HistoryScope);
                setDone(null);
              }}
            >
              <option value="all">Everything</option>
              <option value="finished">Only what I finished</option>
              <option value="unfinished">Only what I did not finish</option>
            </select>
            {/*
              Disabled at zero rather than hidden. A control that vanishes when
              there is nothing to do leaves somebody wondering where it went,
              and "nothing to forget" is a useful answer in itself.
            */}
            <button
              className="set-btn"
              disabled={busy || n === 0}
              onClick={() => {
                setDone(null);
                setConfirming(true);
              }}
            >
              Clear…
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
          <p className="addon-dialog__caps">
            Nothing beyond running — no network, no secrets.
          </p>
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
          {plugin.name} <span className="addon-version">v{plugin.version}</span>{" "}
          <span className={"addon-signer addon-signer--" + plugin.signer}>
            {SIGNER_LABEL[plugin.signer] ?? plugin.signer}
          </span>
        </div>
        <div className="set-row__sub">
          {plugin.enabled ? "Enabled" : "Disabled"}
          {granted.length > 0
            ? " · " + granted.join(" · ")
            : " · no capabilities"}
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

      {staged && (
        <GrantDialog plugin={staged} onClose={() => setStaged(null)} />
      )}
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
/*
 * Everything a picture library can be told to do (ADR 0051, ADR 0052).
 *
 * Its own pane because there are now two of these and there will be more —
 * sensitivity, faces, and eventually whatever comes of dates, duplicates and
 * places. A general pane holding two picture settings is one somebody has to
 * read all of to find either.
 */
function PicturesSection() {
  const { data: settings } = useSettings();
  const update = useUpdateSettings();
  const { data: caps } = useFaceCapabilities();
  const { data: models } = useFaceModels();
  const install = useInstallFaceModels();
  const cancel = useCancelFaceModels();

  const job = models?.job;
  const running = job?.running ?? false;
  const pct =
    job && job.bytes_total > 0
      ? Math.min(100, Math.round((job.bytes_done / job.bytes_total) * 100))
      : 0;

  return (
    <section className="settings__section">
      <span className="section-label">Pictures</span>

      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.sensitive_marking}
              onChange={(e) =>
                update.mutate({ sensitive_marking: e.target.checked })
              }
            />
            Allow folders to be marked sensitive
          </label>
          <p className="set-row__sub">
            Right-click a folder in a picture library to mark it. Anything
            marked is covered — its name still shows — and can only be uncovered
            inside the library or the folder itself, never from the home page.
            It covers again when you leave. Turning this off stops the covering
            and keeps the marks.
          </p>
        </>
      )}

      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Group faces</div>
          <div className="set-row__sub">
            Find the people in a picture library and let you name them. It runs
            entirely on this machine — nothing is uploaded and no account is
            needed.
          </div>
        </div>
      </div>

      {/*
        Three states, said apart. "Not installed", "no models" and "ready" have
        different fixes, and a single "unavailable" sends somebody looking in
        the wrong place.
      */}
      {models && !models.supported && (
        <p className="set-row__sub">
          {models.reason ??
            "There is no face-model download for this platform yet."}
        </p>
      )}

      {models?.supported && !models.installed && !running && (
        <>
          <p className="set-row__sub">
            This needs a one-off download of{" "}
            <strong>{formatMB(models.bytes_total ?? 0)}</strong> — two models
            and the runtime that executes them. Nothing is fetched until you
            press the button.
          </p>
          <ul className="set-assets">
            {(models.assets ?? []).map((a) => (
              <li key={a.name}>
                <code>{a.name}</code> · {formatMB(a.size_bytes)} ·{" "}
                <a href={a.licence_url} target="_blank" rel="noreferrer">
                  {a.licence}
                </a>
              </li>
            ))}
          </ul>
          <button
            className="set-btn"
            onClick={() => install.mutate()}
            disabled={install.isPending}
          >
            {install.isPending ? "Starting…" : "Download the face models"}
          </button>
        </>
      )}

      {running && job && (
        <>
          <p className="set-row__sub">
            {job.stage === "verifying"
              ? "Checking what arrived…"
              : job.stage === "installing"
                ? "Putting it in place…"
                : `Downloading ${job.asset ?? ""}`}{" "}
            — {pct}%
          </p>
          <button className="set-btn" onClick={() => cancel.mutate()}>
            Cancel
          </button>
        </>
      )}

      {job?.error && !running && (
        <p className="set-row__sub set-row__sub--warn">
          The download did not finish: {job.error}
        </p>
      )}

      {models?.installed && (
        <p className="set-row__sub">
          The models are installed in <code>{models.directory}</code>.
          {caps?.ready ? (
            " Face grouping is ready — open a picture library and press People."
          ) : (
            <>
              {" "}
              The worker itself is still missing:{" "}
              <strong>{caps?.reason ?? "not installed"}</strong>. It ships with
              the LANcast installer rather than the in-app update, so running
              the installer for this version will supply it.
            </>
          )}
        </p>
      )}

      {models?.supported && !models.installed && !running && caps && !caps.ready && (
        <p className="set-row__sub">
          {/*
            Said here because it is the single most likely reason this feature
            appears to do nothing: the in-app updater replaces the server only,
            and the worker arrives with the installer.
          */}
          Note: the face worker ships with the LANcast installer. If you updated
          from inside the app, run the installer for this version as well.
        </p>
      )}
    </section>
  );
}

// Megabytes, rounded, because nobody reading a download size wants three
// decimal places of a hundred-megabyte number.
function formatMB(bytes: number): string {
  return `${Math.round(bytes / 1048576)} MB`;
}

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
      {/*
        The source link, and it is a licence obligation rather than a courtesy.

        LANcast is AGPL-3.0 (ADR 0053). Section 13 requires a modified version
        offered to people over a network to give those people its source, and
        this server is exactly that shape — a thing other people in the house
        use through a browser. Shipping the affordance means a fork inherits
        compliance instead of having to remember it, which is the only version
        of this that actually works.

        A plain link rather than a bundled tarball: the licence asks for access
        through customary means, and for a public repository this is it.
        Somebody who forks and modifies has to point it at their own.
      */}
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Licence</div>
          <div className="set-row__sub">
            AGPL-3.0 — you are entitled to the source of the version you are
            using.{" "}
            <a
              href="https://github.com/Conqueror-Mod/LANcast"
              target="_blank"
              rel="noreferrer"
            >
              Source code
            </a>
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
  /*
   * Pictures has its own pane rather than a corner of Libraries.
   *
   * Sensitive marking sat under Libraries because it was the only setting a
   * picture library had. It is not any more — face grouping arrived with a
   * download, a state and a switch of its own — and two of them in a general
   * pane is the point at which somebody looking for one has to read all of it.
   * Made a home now rather than after the third.
   */
  { id: "pictures", label: "Pictures", admin: true },
  { id: "metadata", label: "Metadata", admin: true },
  { id: "playback", label: "Playback", admin: true },
  { id: "users", label: "Users", admin: true },
  { id: "livetv", label: "Live TV", admin: true },
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
              {pane === "livetv" && <LiveTVSection />}
              {pane === "addons" && <AddonsSection />}
              {pane === "updates" && (
                <>
                  <UpdateSettings />
                  <UpdateCheckSection />
                </>
              )}
              {pane === "pictures" && <PicturesSection />}
              {pane === "activity" && (
                <>
                  {/*
                    Review has a permanent home here, and only a conditional one
                    in the nav.

                    The nav entry appears when something needs a look and
                    vanishes when nothing does, which is right for a prompt and
                    wrong for a page: the two-files-one-work report (ADR 0042)
                    is reachable *only* from Review, and dismissing the last
                    fixable match takes the door away with it — the collisions
                    are still there, still listed, and no longer openable.
                    Reported as exactly that.

                    Activity is where it belongs. Both panes answer "what has
                    this server been doing and what does it want from me", and
                    a settings pane is allowed to be empty in a way a nav badge
                    is not.
                  */}
                  <Review />
                  <AuditLog />
                </>
              )}
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
          {pane === "display" && (
            <>
              <DisplaySection />
              {/* On the device pane rather than the admin Playback one: a
                  denial is stored per browser, so the person it slows down is
                  the one sitting here, who may not be an administrator. */}
              <section className="settings__section">
                <span className="section-label">Playback</span>
                <CodecDenialsRow />
              </section>
            </>
          )}
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
              onChange={(e) =>
                update.mutate({ debug_logging: e.target.checked })
              }
            />
            Write debug detail to the log
          </label>
          <div className="set-row__sub set-row__sub--standalone">
            Takes effect on the next line logged — no restart — and survives
            one, because the faults worth turning this on for are the
            intermittent ones. Leave it off for ordinary running: it is verbose.
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
                  setFreed(
                    `${Math.round((r.freed_bytes / 1048576) * 10) / 10} MB`,
                  ),
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
              reset.mutate(undefined, {
                onSuccess: () => setConfirmReset(false),
              });
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
