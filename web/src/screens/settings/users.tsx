import { AuthorizedDevices } from "@/components/AuthorizedDevices";
import { useState } from "react";
import { InviteFriend } from "@/components/InviteFriend";
import {
  useLibraries,
  useSettings,
  useUpdateSettings,
  useCurrentUser,
  useUsers,
  useUpdateUser,
  useUserLibraries,
  useSetUserLibraries,
  useCreateUser,
  useDeleteUser,
  useResetUserPassword,
  useRemoveUserTwoFactor,
  useClearUserPicture,
} from "@/api/hooks";
import { PersonPicture } from "@/components/Avatar";
import { errorMessage } from "@/lib/errors";
import type { AuthUser, ManagedUser } from "@/api/types";
import { RATING_CEILINGS } from "./shared";

function UserRow({ user, isSelf }: { user: ManagedUser; isSelf: boolean }) {
  const del = useDeleteUser();
  const removeTwoFactor = useRemoveUserTwoFactor();
  const clearPicture = useClearUserPicture();
  const [removingTwoFactor, setRemovingTwoFactor] = useState(false);
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
        <PersonPicture
          name={user.name}
          picture={user.picture}
          size={36}
          className="set-user__pic"
        />
        <div className="set-row__main">
          <div className="set-row__title">
            {user.name}
            {isSelf && <span className="set-tag">you</span>}
          </div>
          <div className="set-row__sub">
            {user.role}
            {/*
              Said on the row rather than hidden behind the control, because
              the question an administrator has when they open this pane is
              "which of these accounts is limited", and a limit you have to
              click each row to discover is one nobody audits.
            */}
            {user.max_content_rating
              ? ` · ${user.max_content_rating} and under`
              : ""}
            {user.two_factor ? " · two-factor sign-in" : ""}
          </div>
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
              {/*
                A ceiling set *for* this account, which is the opposite of the
                sharing switch in the Account pane: that one has no
                administrator route because a switch somebody else can flip is
                not consent, and this one has no self-service route because a
                limit you can lift is not a limit. The two look alike and must
                not be generalised into each other.

                Not offered for yourself. An administrator can lift any ceiling,
                so one set on your own account is a note rather than a limit,
                and offering it would suggest otherwise.
              */}
              {!isSelf && (
                <select
                  className="set-input"
                  aria-label={`Content rating limit for ${user.name}`}
                  value={user.max_content_rating ?? ""}
                  disabled={update.isPending}
                  onChange={(e) =>
                    update.mutate({
                      id: user.id,
                      max_content_rating: e.target.value,
                    })
                  }
                >
                  <option value="">No limit</option>
                  {RATING_CEILINGS.map((label) => (
                    <option key={label} value={label}>
                      {label} and under
                    </option>
                  ))}
                </select>
              )}
              <button className="set-btn" onClick={() => setResetting(true)}>
                Reset password
              </button>
              {/*
                Only ever to take it off: the way back in for somebody who lost
                their phone and their recovery codes (ADR 0100). There is no
                way to turn it on for somebody else. Two presses, in the page.
              */}
              {user.two_factor &&
                (removingTwoFactor ? (
                  <>
                    <button
                      className="set-btn set-btn--danger"
                      disabled={removeTwoFactor.isPending}
                      onClick={() =>
                        removeTwoFactor.mutate(user.id, {
                          onSuccess: () => setRemovingTwoFactor(false),
                        })
                      }
                    >
                      Remove two-factor
                    </button>
                    <button className="set-btn" onClick={() => setRemovingTwoFactor(false)}>
                      Keep it
                    </button>
                  </>
                ) : (
                  <button className="set-btn" onClick={() => setRemovingTwoFactor(true)}>
                    Remove two-factor…
                  </button>
                ))}
              {/* Taking down somebody's picture: the whole of the
                  moderation ADR 0121 §5 sizes for a household. One press,
                  because it is undone by them uploading again. */}
              {user.picture && (
                <button
                  className="set-btn"
                  disabled={clearPicture.isPending}
                  onClick={() => clearPicture.mutate(user.id)}
                >
                  Remove picture
                </button>
              )}
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
      {(del.isError || reset.isError || removeTwoFactor.isError || clearPicture.isError) && (
        <span className="set-error">
          {errorMessage(del.error ?? reset.error ?? removeTwoFactor.error ?? clearPicture.error)}
        </span>
      )}
      {/* Not for administrators, who are never restricted (ADR 0084), and
          so not for yourself either: you are one. */}
      {user.role !== "admin" && !isSelf && <UserLibraryAccess user={user} />}
    </div>
  );
}

/*
 * Which libraries a member can see (ADR 0084). Every library is on by
 * default; switching one off hides it from this account everywhere — lists,
 * search, Home, history — and the server refuses it by id as well.
 *
 * The summary is on the row, unopened, for the reason the rating limit is:
 * the question an administrator brings to this pane is "who is limited", and
 * a limit you have to open each row to find is one nobody audits.
 */
function UserLibraryAccess({ user }: { user: AuthUser }) {
  const access = useUserLibraries(user.id);
  const { data: libraries } = useLibraries();
  const save = useSetUserLibraries();
  const [open, setOpen] = useState(false);
  // Nothing until both answers are the shapes expected: one malformed reply
  // must cost this row, not the whole Users pane.
  if (!Array.isArray(access.data?.hidden) || !Array.isArray(libraries)) return null;
  const hidden = new Set(access.data.hidden);
  const visible = libraries.filter((l) => !hidden.has(l.id)).length;
  const toggle = (id: number, show: boolean) => {
    const next = new Set(hidden);
    if (show) next.delete(id);
    else next.add(id);
    save.mutate({ id: user.id, hidden: [...next] });
  };
  return (
    <div className="set-access">
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__sub">
            {hidden.size === 0
              ? "Sees every library"
              : `Sees ${visible} of ${libraries.length} libraries`}
          </div>
        </div>
        <div className="set-row__actions">
          <button
            className="set-btn"
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
          >
            {open ? "Done" : "Libraries"}
          </button>
        </div>
      </div>
      {open && (
        <div className="set-access__list" role="group" aria-label={`Libraries ${user.name} can see`}>
          {libraries.map((l) => (
            <label className="set-toggle" key={l.id}>
              <input
                type="checkbox"
                checked={!hidden.has(l.id)}
                disabled={save.isPending}
                onChange={(e) => toggle(l.id, e.target.checked)}
              />
              {l.name}
            </label>
          ))}
          <p className="set-row__sub">
            A library added later is visible to everyone until you switch it off
            here.
          </p>
          {save.isError && <span className="set-error">{errorMessage(save.error)}</span>}
        </div>
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

function UsersSection() {
  const { data: users } = useUsers();
  const me = useCurrentUser();
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();

  return (
    <section className="settings__section">
      <span className="section-label">Users</span>
      {users?.map((u) => (
        <UserRow key={u.id} user={u} isSelf={u.id === me?.id} />
      ))}
      <AddUser />
      <InviteFriend />
      {settings && (
        <>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.login_picker}
              onChange={(e) => update.mutate({ login_picker: e.target.checked })}
            />
            Show accounts on the sign-in page
          </label>
          <p className="set-row__sub">
            Devices on your home network see everyone&rsquo;s name to pick from instead of typing
            it. From the internet the sign-in page never lists them.
          </p>
          <label className="set-toggle">
            <input
              type="checkbox"
              checked={settings.member_pictures}
              onChange={(e) => update.mutate({ member_pictures: e.target.checked })}
            />
            Members can upload a picture of themselves
          </label>
          <p className="set-row__sub">
            Shown to everyone signed in to this server, never to other servers. Turning
            this off keeps pictures already uploaded; remove one with its account&rsquo;s
            Remove picture. Administrators can always set their own.
          </p>
        </>
      )}
    </section>
  );
}

export function UsersPane() {
  return (
    <>
      <UsersSection />
      <AuthorizedDevices everyone />
    </>
  );
}
