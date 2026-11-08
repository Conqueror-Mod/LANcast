import { AuthorizedDevices } from "@/components/AuthorizedDevices";
import { useState } from "react";
import {
  useChangePassword,
  useAPIKeys,
  useCreateAPIKey,
  useRevokeAPIKey,
  type APIKey,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import { PasskeySettings } from "@/components/PasskeySettings";
import { TwoFactorSettings } from "@/components/TwoFactorSettings";

export function SecuritySection() {
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");

  return (
    <section className="settings__section">
      <span className="section-label">Security</span>

      <AuthorizedDevices />

      <PasskeySettings />

      <TwoFactorSettings />

      <APIKeysPanel />

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

/*
 * API keys (ADR 0061).
 *
 * In the Account pane rather than an admin one, because a key belongs to the
 * person who made it rather than to the server — a member has as much use for
 * one as an administrator, and it grants neither of them administration.
 *
 * The secret is shown once, and this screen has to be blunt about that. Held in
 * component state and never in the query cache: the list refetches after
 * creation and would drop it anyway, and a cache holding a live credential is
 * one more place it can be read from.
 */
function APIKeysPanel() {
  const { data } = useAPIKeys();
  const create = useCreateAPIKey();
  const revoke = useRevokeAPIKey();
  const [name, setName] = useState("");
  const [fresh, setFresh] = useState<{ name: string; secret: string } | null>(
    null,
  );

  const keys = data?.keys ?? [];

  return (
    <>
      <span className="set-sublabel">API keys</span>
      <p className="set-row__sub">
        For a script, a command line, or another program that talks to this
        server — so it never needs your password. A key acts as you, except that
        it can never change server settings and can never manage keys.
      </p>

      <form
        className="set-add"
        onSubmit={(e) => {
          e.preventDefault();
          const wanted = name.trim();
          if (!wanted) return;
          create.mutate(wanted, {
            onSuccess: (r) => {
              setFresh({ name: r.key.name, secret: r.secret });
              setName("");
            },
          });
        }}
      >
        <input
          className="set-input"
          type="text"
          placeholder="What is it for? e.g. backup script"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={64}
        />
        <button
          className="set-btn"
          type="submit"
          disabled={create.isPending || name.trim().length === 0}
        >
          {create.isPending ? "Creating…" : "Create a key"}
        </button>
        {create.isError && (
          <span className="set-error">{errorMessage(create.error)}</span>
        )}
      </form>

      {/*
        Shown once, and said so twice — in the heading and beside the value.
        Somebody who closes this needs a new key, which is a far better outcome
        than a server that can hand the old one back.
      */}
      {fresh && (
        <div className="set-keyreveal">
          <div className="set-keyreveal__title">
            Copy “{fresh.name}” now — it is not shown again
          </div>
          <code className="set-keyreveal__value">{fresh.secret}</code>
          <p className="set-row__sub">
            Send it as <code>Authorization: Bearer {"<key>"}</code>. LANcast
            stores only a fingerprint of it, so nobody — including this screen —
            can show it to you a second time.
          </p>
          <button className="set-btn" onClick={() => setFresh(null)}>
            I have copied it
          </button>
        </div>
      )}

      {keys.length === 0 && !fresh && (
        <p className="set-row__sub">No keys yet.</p>
      )}

      {keys.map((k: APIKey) => (
        <div className="set-row" key={k.id}>
          <div className="set-row__main">
            <div className="set-row__title">{k.name}</div>
            <div className="set-row__sub">
              {/*
                "Never used" rather than a date in 1970, and it is the useful
                half of this row: the key nothing has ever presented is the safe
                one to revoke.
              */}
              {k.last_used === 0
                ? "Never used"
                : `Last used ${new Date(k.last_used * 1000).toLocaleDateString()}`}
              {" · created "}
              {new Date(k.created_at * 1000).toLocaleDateString()}
            </div>
          </div>
          <button
            className="set-btn set-btn--danger"
            onClick={() => revoke.mutate(k.id)}
            disabled={revoke.isPending}
          >
            Revoke
          </button>
        </div>
      ))}
    </>
  );
}
