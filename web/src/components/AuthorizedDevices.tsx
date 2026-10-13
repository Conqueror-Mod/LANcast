import { useAllSessions, useMySessions, useSignOutDevice, type SessionInfo } from "@/api/hooks";
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
  const list = q.data ?? [];

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
                  {s.current ? "In use now" : `Last used ${when(iso(s.last_seen)).toLowerCase()}`}
                  {s.address && <> · {s.address}</>}
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
    </section>
  );
}

const iso = (sec: number) => new Date(sec * 1000).toISOString();
