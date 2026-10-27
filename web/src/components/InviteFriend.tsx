import { useState } from "react";
import { useCreateInvite, useDeleteInvite, useInvites, useLibraries } from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import { RATING_RUNGS } from "@/lib/ratings";
import "./InviteFriend.css";

/*
 * Invite a friend (ADR 0104): a link that makes one member account, once.
 *
 * Beside "Add user", not instead of it. That form is for somebody in the
 * room; this one is for somebody you would otherwise have to send a password
 * to. The link is shown once, because the server keeps only a hash of it.
 */

const LOOPBACK = new Set(["localhost", "127.0.0.1", "::1", "[::1]"]);

// The link for a secret: the public name when the server has one, otherwise
// the address this window is on. Exported for the tests.
export function inviteLink(token: string, publicName: string, origin = window.location.origin): string {
  const base = publicName ? `https://${publicName}` : origin;
  return `${base}/join#${token}`;
}

// What a link without a public name can reach, said before it is sent.
export function reachWarning(publicName: string, hostname = window.location.hostname): string | null {
  if (publicName) return null;
  if (LOOPBACK.has(hostname)) {
    return "This link points at this computer and works nowhere else. Turn on a public name under Remote access so a friend can reach you.";
  }
  return "This link uses your home network address, so it only works for somebody on your network. For a friend elsewhere, turn on a public name under Remote access.";
}

function when(unix: number): string {
  return new Date(unix * 1000).toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

export function InviteFriend() {
  const create = useCreateInvite();
  const { data: invites } = useInvites();
  const { data: libraries } = useLibraries();
  const withdraw = useDeleteInvite();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState("");
  const [ceiling, setCeiling] = useState("");
  const [hidden, setHidden] = useState<number[]>([]);
  const [made, setMade] = useState<{ link: string; warning: string | null } | null>(null);
  const [copied, setCopied] = useState(false);

  const reset = () => {
    setOpen(false);
    setNote("");
    setCeiling("");
    setHidden([]);
  };

  return (
    <div className="invite">
      {made && (
        <div className="invite__made" role="status">
          <p className="invite__made-head">
            Send this link to them. It works once, for 7 days, and you will not see it again.
          </p>
          <div className="invite__link-row">
            <input
              className="set-input invite__link"
              readOnly
              value={made.link}
              aria-label="Invite link"
              onFocus={(e) => e.currentTarget.select()}
            />
            <button
              className="set-btn"
              type="button"
              onClick={() => {
                void navigator.clipboard?.writeText(made.link).then(() => setCopied(true));
              }}
            >
              {copied ? "Copied" : "Copy"}
            </button>
            <button
              className="set-btn"
              type="button"
              onClick={() => {
                setMade(null);
                setCopied(false);
              }}
            >
              Done
            </button>
          </div>
          {made.warning && <p className="set-error">{made.warning}</p>}
        </div>
      )}

      {!open && !made && (
        <button className="set-btn" type="button" onClick={() => setOpen(true)}>
          + Invite a friend
        </button>
      )}

      {open && (
        <form
          className="invite__form"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              { note: note.trim(), max_content_rating: ceiling, hidden_libraries: hidden },
              {
                onSuccess: (r) => {
                  setMade({
                    link: inviteLink(r.token, r.public_name),
                    warning: reachWarning(r.public_name),
                  });
                  setCopied(false);
                  reset();
                },
              },
            );
          }}
        >
          <p className="set-row__sub">
            They choose their own name and password, and join as a member. Nobody sends anybody a
            password.
          </p>
          <input
            className="set-input"
            placeholder="Who it's for (only you see this)"
            value={note}
            maxLength={200}
            onChange={(e) => setNote(e.target.value)}
            aria-label="Who the invite is for"
          />
          <select
            className="set-input"
            value={ceiling}
            onChange={(e) => setCeiling(e.target.value)}
            aria-label="Content rating limit for the invited account"
          >
            <option value="">No rating limit</option>
            {RATING_RUNGS.map((label) => (
              <option key={label} value={label}>
                {label} and under
              </option>
            ))}
          </select>
          {libraries && libraries.length > 0 && (
            <fieldset className="invite__libraries">
              <legend>Keep from them</legend>
              {libraries.map((lib) => (
                <label key={lib.id} className="set-toggle">
                  <input
                    type="checkbox"
                    checked={hidden.includes(lib.id)}
                    onChange={(e) =>
                      setHidden(
                        e.currentTarget.checked
                          ? [...hidden, lib.id]
                          : hidden.filter((id) => id !== lib.id),
                      )
                    }
                  />
                  {lib.name}
                </label>
              ))}
            </fieldset>
          )}
          <div className="invite__actions">
            <button className="set-btn" type="submit" disabled={create.isPending}>
              Make link
            </button>
            <button className="set-btn" type="button" onClick={reset}>
              Cancel
            </button>
          </div>
          {create.isError && <span className="set-error">{errorMessage(create.error)}</span>}
        </form>
      )}

      {invites && invites.length > 0 && (
        <ul className="invite__list" aria-label="Open invites">
          {invites.map((inv) => (
            <li key={inv.id} className="invite__row">
              <div className="invite__who">
                <span className="invite__note">{inv.note || "Invite"}</span>
                <span className="invite__meta">
                  Made by {inv.created_by} · works until {when(inv.expires_at)}
                  {inv.max_content_rating ? ` · ${inv.max_content_rating} and under` : ""}
                  {inv.hidden_libraries.length > 0
                    ? ` · ${inv.hidden_libraries.length} ${inv.hidden_libraries.length === 1 ? "library" : "libraries"} kept from them`
                    : ""}
                </span>
              </div>
              <button
                className="set-btn"
                type="button"
                disabled={withdraw.isPending}
                onClick={() => withdraw.mutate(inv.id)}
              >
                Withdraw
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
