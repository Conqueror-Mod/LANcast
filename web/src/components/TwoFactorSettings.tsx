import { useState } from "react";
import "./TwoFactorSettings.css";
import {
  useDisableTwoFactor,
  useEnableTwoFactor,
  useNewRecoveryCodes,
  useStartTwoFactor,
  useTwoFactor,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";

/*
 * Two-factor sign-in, in the Security pane (ADR 0100).
 *
 * Each person's own: off until they turn it on, and nobody can turn it on for
 * them. Turning it on needs a code from the app, which proves the app has the
 * secret. Turning it off, and making new recovery codes, need the password —
 * as adding a passkey does — because a session left open on somebody else's
 * screen should not be enough to weaken the account or read fresh codes.
 *
 * Recovery codes are shown once, in the page, until the person says they have
 * them. Nothing here uses a native confirm() (CLAUDE.md: focus in the desktop
 * window).
 */
export function TwoFactorSettings() {
  const { data } = useTwoFactor();
  const [codes, setCodes] = useState<string[] | null>(null);
  if (!data) return null;

  return (
    <>
      <span className="set-sublabel">Two-factor sign-in</span>
      {codes ? (
        <RecoveryCodesShown codes={codes} onDone={() => setCodes(null)} />
      ) : data.enabled ? (
        <TwoFactorOn
          since={data.enabled_at}
          left={data.recovery_codes_left}
          onNewCodes={setCodes}
        />
      ) : data.available ? (
        <TwoFactorOff onOn={setCodes} />
      ) : (
        <div className="set-row__sub">
          Two-factor sign-in is unavailable: this server could not prepare the
          key it keeps secrets under. Its log says why.
        </div>
      )}
    </>
  );
}

function TwoFactorOff({ onOn }: { onOn: (codes: string[]) => void }) {
  const start = useStartTwoFactor();
  const enable = useEnableTwoFactor();
  const [code, setCode] = useState("");
  const setup = start.data;

  if (!setup) {
    return (
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__sub">
            After your password, ask for a six-digit code from an authenticator
            app on your phone. Off until you turn it on; passkeys skip it.
          </div>
          {start.isError && (
            <div className="set-row__sub" role="alert">
              {errorMessage(start.error)}
            </div>
          )}
        </div>
        <div className="set-row__actions">
          <button className="set-btn" disabled={start.isPending} onClick={() => start.mutate()}>
            Turn on
          </button>
        </div>
      </div>
    );
  }

  return (
    <form
      className="set-tf"
      onSubmit={(e) => {
        e.preventDefault();
        enable.mutate(code, { onSuccess: (r) => onOn(r.recovery_codes) });
      }}
    >
      <div className="set-row__sub">
        In your authenticator app, add an account and type this key, or open
        the link on the phone the app is on.
      </div>
      <code className="set-tf__key" aria-label="Key for your authenticator app">
        {setup.key}
      </code>
      <a className="set-tf__link" href={setup.url}>
        Open in your authenticator app
      </a>
      <div className="set-add">
        <input
          className="set-input"
          aria-label="Code from the app"
          placeholder="Code from the app"
          autoComplete="one-time-code"
          inputMode="numeric"
          maxLength={7}
          value={code}
          onChange={(e) => setCode(e.target.value)}
        />
        <button
          className="set-btn"
          type="submit"
          disabled={enable.isPending || code.replace(/\s/g, "").length !== 6}
        >
          Turn on
        </button>
        <button
          className="set-btn"
          type="button"
          onClick={() => {
            start.reset();
            enable.reset();
            setCode("");
          }}
        >
          Cancel
        </button>
      </div>
      {enable.isError && (
        <span className="set-error" role="alert">
          {errorMessage(enable.error)}
        </span>
      )}
    </form>
  );
}

function TwoFactorOn({
  since,
  left,
  onNewCodes,
}: {
  since?: number;
  left: number;
  onNewCodes: (codes: string[]) => void;
}) {
  const off = useDisableTwoFactor();
  const fresh = useNewRecoveryCodes();
  const [asking, setAsking] = useState<"off" | "codes" | null>(null);
  const [password, setPassword] = useState("");
  const pending = off.isPending || fresh.isPending;
  const failed = off.error ?? fresh.error;

  const details = [
    since ? `On since ${new Date(since * 1000).toLocaleDateString()}` : "On",
    `${left} recovery ${left === 1 ? "code" : "codes"} left`,
  ].join(" · ");

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">On</div>
        <div className="set-row__sub">{details}</div>
        {left <= 2 && (
          <div className="set-row__sub">
            Running low: make new recovery codes and keep them somewhere safe.
          </div>
        )}
        {failed && (
          <div className="set-row__sub" role="alert">
            {errorMessage(failed)}
          </div>
        )}
      </div>
      <div className="set-row__actions">
        {asking ? (
          <form
            className="set-add"
            onSubmit={(e) => {
              e.preventDefault();
              const done = () => {
                setAsking(null);
                setPassword("");
              };
              if (asking === "off") off.mutate(password, { onSuccess: done });
              else
                fresh.mutate(password, {
                  onSuccess: (r) => {
                    done();
                    onNewCodes(r.recovery_codes);
                  },
                });
            }}
          >
            <input
              className="set-input"
              type="password"
              autoFocus
              placeholder="Current password"
              autoComplete="current-password"
              aria-label="Current password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button className="set-btn" type="submit" disabled={pending || !password}>
              {asking === "off" ? "Turn off" : "Make new codes"}
            </button>
            <button
              className="set-btn"
              type="button"
              onClick={() => {
                setAsking(null);
                setPassword("");
                off.reset();
                fresh.reset();
              }}
            >
              Cancel
            </button>
          </form>
        ) : (
          <>
            <button className="set-btn" onClick={() => setAsking("codes")}>
              New recovery codes
            </button>
            <button className="set-btn" onClick={() => setAsking("off")}>
              Turn off
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function RecoveryCodesShown({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="set-tf">
      <div className="set-row__sub">
        <strong>Save these recovery codes now.</strong> Each signs you in once if
        you lose your phone. They will not be shown again.
      </div>
      <ul className="set-tf__codes" aria-label="Recovery codes">
        {codes.map((c) => (
          <li key={c}>
            <code>{c}</code>
          </li>
        ))}
      </ul>
      <div className="set-add">
        <button
          className="set-btn"
          type="button"
          onClick={() => {
            navigator.clipboard?.writeText(codes.join("\n")).then(
              () => setCopied(true),
              () => setCopied(false),
            );
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
        <button className="set-btn" type="button" onClick={onDone}>
          I&rsquo;ve saved them
        </button>
      </div>
    </div>
  );
}
