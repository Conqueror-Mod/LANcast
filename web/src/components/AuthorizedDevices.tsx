import { useState } from "react";
import {
  useAllSessions,
  useMySessions,
  useSignOutDevice,
  useSignOutOtherDevices,
  type SessionInfo,
} from "@/api/hooks";
import { when } from "@/lib/when";
import "./AuthorizedDevices.css";

/*
 * Authorized devices (docs/settings-plan.md, step 4).
 *
 * Your account › Security lists every device signed in as you; Server › Users
 * & access lists everyone's, for an administrator. Each says what it is, when
 * it was last used and from where, and can be signed out — which ends that
 * session on the server at once, not when its cookie next expires.
 */
export function AuthorizedDevices({ everyone = false }: { everyone?: boolean }) {
  const mine = useMySessions();
  const all = useAllSessions(everyone);
  const q = everyone ? all : mine;
  const signOut = useSignOutDevice();
  const signOutOthers = useSignOutOtherDevices();
  // Signing out everything else is two presses, both in the page: a native
  // confirm() strands keyboard focus in the desktop window.
  const [confirming, setConfirming] = useState(false);
  const list = q.data ?? [];
  const others = everyone ? 0 : list.filter((s) => !s.current).length;

  const press = (s: SessionInfo) =>
    signOut.mutate(
      { id: s.id, anyAccount: everyone },
      {
        // Signing out this device is signing out: the server has cleared the
        // cookie, so the page goes back to the start, where sign-in is.
        onSuccess: () => {
          if (s.current) window.location.assign("/");
        },
      },
    );

  return (
    <section className="settings__section">
      <span className="section-label">{everyone ? "Signed-in devices" : "Authorized devices"}</span>
      <p className="set-row__sub set-row__sub--standalone">
        {everyone
          ? "Every device signed in to this server, by account. Signing one out ends it at once."
          : "Everywhere you are signed in. If you do not recognise one, sign it out and change your password."}
      </p>
      {q.isError && <p className="set-error">The server did not answer. Try again in a moment.</p>}
      {q.data && list.length === 0 && <p className="devices__empty">No devices are signed in.</p>}
      {list.length > 0 && (
        <ul className="devices">
          {list.map((s) => (
            <li className="devices__row" key={s.id}>
              <div className="devices__main">
                <span className="devices__name">
                  {s.device || "A device signed in before this was recorded"}
                  {s.current && <span className="devices__this">This device</span>}
                </span>
                <span className="devices__meta">
                  {everyone && <>{s.user_name} · </>}
                  {s.current ? "In use now" : `Last used ${lastUsed(s.last_seen)}`}
                  {s.address && <> · {place(s.address)}</>}
                </span>
              </div>
              <button
                className="set-btn"
                disabled={signOut.isPending && signOut.variables?.id === s.id}
                onClick={() => press(s)}
              >
                Sign out
              </button>
            </li>
          ))}
        </ul>
      )}
      {others > 1 && (
        <div className="devices__others">
          {confirming ? (
            <>
              <span className="set-row__sub">
                Sign out {others} other devices? Each will need to sign in again.
              </span>
              <button
                className="set-btn set-btn--danger"
                disabled={signOutOthers.isPending}
                onClick={() =>
                  signOutOthers.mutate(undefined, { onSettled: () => setConfirming(false) })
                }
              >
                Sign them out
              </button>
              <button className="set-btn" onClick={() => setConfirming(false)}>
                Cancel
              </button>
            </>
          ) : (
            <button className="set-btn" onClick={() => setConfirming(true)}>
              Sign out every other device
            </button>
          )}
        </div>
      )}
    </section>
  );
}

const iso = (sec: number) => new Date(sec * 1000).toISOString();

// "Last used today 03:00", but "Last used Oct 10 03:00": only the relative
// words read as the middle of a sentence; a month name keeps its capital.
export function lastUsed(sec: number, now = new Date()): string {
  return when(iso(sec), now).replace(/^(Just now|Today|Yesterday|Tomorrow)/, (w) => w.toLowerCase());
}

// The server records the address a request came from. Loopback is the
// machine the server runs on, which is a better answer than "::1".
export function place(address: string): string {
  return address === "::1" || address.startsWith("127.") ? "this computer" : address;
}
