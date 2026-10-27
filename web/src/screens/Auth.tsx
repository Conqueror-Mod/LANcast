import { useState, type FormEvent, type ReactNode } from "react";
import { ApiFailure } from "@/api/client";
import {
  useSetup,
  useLogin,
  useLoginTwoFactor,
  useAuthStatus,
  usePasskeyLogin,
  useLoginAccounts,
} from "@/api/hooks";
import { cancelledByPerson } from "@/lib/webauthn";
import "./Auth.css";

// The auth screens live outside the AppShell: no nav, just the nebula field and
// a single quiet card. Chrome recedes; the room lights are already dim.
function AuthCard({
  label,
  intro,
  children,
}: {
  label: string;
  intro?: string;
  children: ReactNode;
}) {
  return (
    <div className="auth">
      <div className="auth__card">
        <div className="auth__brand">LANCAST</div>
        <div className="auth__label">{label}</div>
        {intro && <p className="auth__intro">{intro}</p>}
        {children}
      </div>
    </div>
  );
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiFailure) return err.message;
  return "Something went wrong. Try again.";
}

// Setup: the first run. Creates the first account, which is always an admin.
//
// restartRequired, not "is the LAN reachable": the note below promises that a
// restart will make other devices work, and that is only true when a restart
// would actually bind wider. An operator who configured a loopback address
// deliberately gets no promise we cannot keep.
export function Setup({ restartRequired }: { restartRequired: boolean }) {
  const setup = useSetup();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  /*
   * Ticked to begin with, and that is the decision rather than a default
   * nobody thought about (ADR 0048).
   *
   * Most libraries cannot play without ffmpeg, and the failure it produces is
   * not "a feature is missing" — it is somebody concluding the software cannot
   * play their files, from a symptom two layers from its cause. The person this
   * protects is the one who reads nothing, so the box has to be ticked for
   * them.
   *
   * What keeps that honest is that the fetch follows a button *they* press,
   * having been told what it downloads, from where, how big and under which
   * licence. That is why this is a ticked box on a form and not a fetch the
   * server starts on its own: the traffic is asked for, so the no-phone-home
   * principle keeps its third job and README needed no exception.
   */
  const [installTools, setInstallTools] = useState(true);

  function submit(e: FormEvent) {
    e.preventDefault();
    setup.mutate({
      username: username.trim(),
      password,
      install_media_tools: installTools,
    });
  }

  return (
    <AuthCard
      label="Create your account"
      intro="The first account is the administrator. You can add more people once you're in."
    >
      <form className="auth__form" onSubmit={submit}>
        <label className="auth__field">
          <span>Username</span>
          <input
            autoFocus
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </label>
        <label className="auth__field">
          <span>Password</span>
          <input
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {/* The disclosure *is* the consent, so it names the thing, the size,
            the source and the licence rather than asking for a blank yes. A
            download somebody cannot identify is not consent (ADR 0043). */}
        <label className="auth__check">
          <input
            type="checkbox"
            checked={installTools}
            onChange={(e) => setInstallTools(e.target.checked)}
          />
          <span>
            <strong>Download the media tools (about 160 MB)</strong>
            <span className="auth__check-desc">
              Most video files need converting before a browser can play them,
              and LANcast uses ffmpeg to do it. It is fetched once from{" "}
              <code>github.com/BtbN/FFmpeg-Builds</code>, is licensed under the
              GPL, and never contacts anything again. Without it, most of a
              library will not play. You can untick this and install it later
              from Settings.
            </span>
          </span>
        </label>
        {setup.isError && (
          <p className="auth__error" role="alert">
            {errorMessage(setup.error)}
          </p>
        )}
        {restartRequired && (
          <p className="auth__note">
            After creating your account, restart LANcast to reach it from other
            devices on your network.
          </p>
        )}
        <button
          type="submit"
          className="auth__submit"
          disabled={setup.isPending || !username.trim() || !password}
        >
          {setup.isPending ? "Creating…" : "Create account"}
        </button>
      </form>
    </AuthCard>
  );
}

// Login: an account exists; prove who you are.
export function Login() {
  const login = useLogin();
  const passkey = usePasskeyLogin();
  // Only where the server says a passkey can work on this very connection:
  // at its public name, over its trusted certificate (ADR 0082). By IP or in
  // the desktop window there is no button, rather than one that cannot work.
  const { data: status } = useAuthStatus();
  const passkeysHere = status?.passkeys?.available === true;
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  /*
   * The account picker: names to pick from, offered by the server only to
   * the home network. picked is the name chosen from it; typing means
   * "someone else", which is also where a stranger always is, because the
   * list they are sent is empty.
   */
  const { data: listed } = useLoginAccounts();
  const accounts = Array.isArray(listed?.accounts) ? listed.accounts : [];
  const [picked, setPicked] = useState<string | null>(null);
  const [typing, setTyping] = useState(false);

  function submit(e: FormEvent) {
    e.preventDefault();
    login.mutate({ username: (picked ?? username).trim(), password });
  }

  // The password was right and the account wants a code (ADR 0100).
  const challenge = login.data?.two_factor ? login.data.challenge : undefined;
  if (challenge) {
    return (
      <CodeStep
        challenge={challenge}
        onRestart={() => {
          setPassword("");
          login.reset();
        }}
      />
    );
  }

  if (accounts.length > 0 && picked === null && !typing) {
    return (
      <AuthCard label="Who's signing in?">
        <div className="auth__accounts" role="list">
          {accounts.map((a) => (
            <button
              key={a.name}
              type="button"
              role="listitem"
              className="auth__account"
              onClick={() => {
                setPicked(a.name);
                setPassword("");
              }}
            >
              <span className="auth__avatar" aria-hidden="true">
                {a.name.slice(0, 1).toUpperCase()}
              </span>
              <span className="auth__account-name">{a.name}</span>
            </button>
          ))}
        </div>
        <button type="button" className="auth__link" onClick={() => setTyping(true)}>
          Someone else
        </button>
      </AuthCard>
    );
  }

  return (
    <AuthCard label="Sign in">
      <form className="auth__form" onSubmit={submit}>
        {picked !== null ? (
          <div className="auth__picked">
            <span className="auth__avatar" aria-hidden="true">
              {picked.slice(0, 1).toUpperCase()}
            </span>
            <span className="auth__account-name">{picked}</span>
            {/* Kept in the form so a password manager knows whose password it is. */}
            <input type="hidden" autoComplete="username" value={picked} readOnly />
            <button
              type="button"
              className="auth__link"
              onClick={() => {
                setPicked(null);
                setPassword("");
                login.reset();
              }}
            >
              Not you?
            </button>
          </div>
        ) : (
          <label className="auth__field">
            <span>Username</span>
            <input
              autoFocus
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </label>
        )}
        <label className="auth__field">
          <span>Password</span>
          <input
            type="password"
            autoFocus={picked !== null}
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {login.isError && (
          <p className="auth__error" role="alert">
            {errorMessage(login.error)}
          </p>
        )}
        <button
          type="submit"
          className="auth__submit"
          disabled={login.isPending || !(picked ?? username).trim() || !password}
        >
          {login.isPending ? "Signing in…" : "Sign in"}
        </button>
      </form>
      {passkeysHere && (
        <div className="auth__passkey">
          <div className="auth__or">or</div>
          <button
            type="button"
            className="auth__submit auth__submit--secondary"
            disabled={passkey.isPending}
            onClick={() => passkey.mutate()}
          >
            {passkey.isPending ? "Waiting for your passkey…" : "Sign in with a passkey"}
          </button>
          {passkey.isError && (
            <p className="auth__error" role="alert">
              {cancelledByPerson(passkey.error)
                ? "No passkey was used. Try again, or sign in with your password."
                : errorMessage(passkey.error)}
            </p>
          )}
        </div>
      )}
    </AuthCard>
  );
}

/*
 * The second half of a sign-in with two-factor on (ADR 0100): six digits from
 * the app, or a recovery code. A challenge lasts five minutes and five tries;
 * once the server says it has gone, the only way on is the password again.
 */
function CodeStep({ challenge, onRestart }: { challenge: string; onRestart: () => void }) {
  const verify = useLoginTwoFactor();
  const [recovery, setRecovery] = useState(false);
  const [code, setCode] = useState("");

  function submit(e: FormEvent) {
    e.preventDefault();
    verify.mutate(
      recovery ? { challenge, recovery_code: code } : { challenge, code },
      { onError: () => setCode("") },
    );
  }

  const ready = recovery ? code.replace(/[\s-]/g, "").length === 8 : code.replace(/\s/g, "").length === 6;

  return (
    <AuthCard
      label={recovery ? "Use a recovery code" : "Enter your code"}
      intro={
        recovery
          ? "One of the codes you saved when you turned on two-factor sign-in. Each works once."
          : "Open your authenticator app and type the six-digit code for LANcast."
      }
    >
      <form className="auth__form" onSubmit={submit}>
        <label className="auth__field">
          <span>{recovery ? "Recovery code" : "Code"}</span>
          <input
            key={recovery ? "recovery" : "code"}
            autoFocus
            autoComplete="one-time-code"
            inputMode={recovery ? "text" : "numeric"}
            spellCheck={false}
            maxLength={recovery ? 12 : 7}
            placeholder={recovery ? "xxxx-xxxx" : "123456"}
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        </label>
        {verify.isError && (
          <p className="auth__error" role="alert">
            {errorMessage(verify.error)}
          </p>
        )}
        <button type="submit" className="auth__submit" disabled={verify.isPending || !ready}>
          {verify.isPending ? "Checking…" : "Sign in"}
        </button>
      </form>
      <button
        type="button"
        className="auth__link"
        onClick={() => {
          setRecovery(!recovery);
          setCode("");
          verify.reset();
        }}
      >
        {recovery ? "Use a code from the app instead" : "Use a recovery code"}
      </button>
      <button type="button" className="auth__link" onClick={onRestart}>
        Start again
      </button>
    </AuthCard>
  );
}
