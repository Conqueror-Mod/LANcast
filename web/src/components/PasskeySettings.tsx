import { useState } from "react";
import {
  useAddPasskey,
  usePasskeys,
  useRemovePasskey,
  useRenamePasskey,
} from "@/api/hooks";
import type { Passkey } from "@/api/types";
import { errorMessage } from "@/lib/errors";
import { cancelledByPerson } from "@/lib/webauthn";

/*
 * Passkeys, in the Account pane (ADR 0082).
 *
 * The list works everywhere, so a passkey can always be seen and removed. Adding
 * one only works at the server's public name, and the panel says so instead of
 * offering a button that would fail in the browser.
 *
 * Adding asks for the current password, as changing it does: a session left
 * open is not enough to add a new way in. Removing asks for nothing, because it
 * only ever makes the account harder to enter — but it is two presses, in the
 * page, never a native confirm() (see CLAUDE.md on what those do to focus in a
 * desktop window).
 */
export function PasskeySettings() {
  const { data } = usePasskeys();
  const list = data?.passkeys;
  if (!data || !Array.isArray(list)) return null;
  return (
    <>
      <span className="set-sublabel">Passkeys</span>
      <div className="set-row__sub">
        Sign in with your phone, computer or security key instead of typing a
        password. Your password keeps working too.
      </div>
      {list.map((p) => (
        <PasskeyRow key={p.id} passkey={p} />
      ))}
      {data.available_here ? (
        <AddPasskey />
      ) : (
        <div className="set-row__sub">
          Passkeys can only be added when LANcast is opened at its public name
          over HTTPS, not by IP address or in the desktop app.
        </div>
      )}
    </>
  );
}

const day = (iso?: string) => (iso ? new Date(iso).toLocaleDateString() : "");

function PasskeyRow({ passkey: p }: { passkey: Passkey }) {
  const rename = useRenamePasskey();
  const remove = useRemovePasskey();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(p.name);
  const [confirming, setConfirming] = useState(false);

  const details = [
    `Added ${day(p.created_at)}`,
    p.last_used_at ? `last used ${day(p.last_used_at)}` : "never used",
    p.synced ? "synced across your devices" : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="set-row">
      <div className="set-row__main">
        {editing ? (
          <input
            className="set-input"
            aria-label="Passkey name"
            value={name}
            maxLength={64}
            onChange={(e) => setName(e.target.value)}
          />
        ) : (
          <div className="set-row__title">{p.name}</div>
        )}
        <div className="set-row__sub">{details}</div>
        {!p.usable && (
          <div className="set-row__sub">
            Made for {p.rp_id}, which is not this server&rsquo;s public name
            any more, so it cannot sign in here. Remove it and add a new one.
          </div>
        )}
        {(rename.isError || remove.isError) && (
          <div className="set-row__sub" role="alert">
            {errorMessage(rename.error ?? remove.error)}
          </div>
        )}
      </div>
      <div className="set-row__actions">
        {editing ? (
          <>
            <button
              className="set-btn"
              disabled={rename.isPending}
              onClick={() =>
                rename.mutate({ id: p.id, name }, { onSuccess: () => setEditing(false) })
              }
            >
              Save
            </button>
            <button className="set-btn" onClick={() => { setName(p.name); setEditing(false); }}>
              Cancel
            </button>
          </>
        ) : confirming ? (
          <>
            <button
              className="set-btn"
              disabled={remove.isPending}
              onClick={() => remove.mutate(p.id)}
            >
              Remove passkey
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Keep it
            </button>
          </>
        ) : (
          <>
            <button className="set-btn" onClick={() => setEditing(true)}>
              Rename
            </button>
            <button className="set-btn" onClick={() => setConfirming(true)}>
              Remove
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function AddPasskey() {
  const add = useAddPasskey();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");

  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        add.mutate(
          { currentPassword: password, name },
          { onSuccess: () => { setName(""); setPassword(""); } },
        );
      }}
    >
      <input
        className="set-input"
        placeholder="Name it, e.g. Phone"
        aria-label="Name for the new passkey"
        maxLength={64}
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      <input
        className="set-input"
        type="password"
        placeholder="Current password"
        autoComplete="current-password"
        aria-label="Current password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        required
      />
      <button className="set-btn" type="submit" disabled={add.isPending || !password}>
        {add.isPending ? "Waiting for your device…" : "Add a passkey"}
      </button>
      {add.isError && (
        <span className="set-error" role="alert">
          {cancelledByPerson(add.error)
            ? "No passkey was made. Try again when you are ready."
            : errorMessage(add.error)}
        </span>
      )}
    </form>
  );
}
