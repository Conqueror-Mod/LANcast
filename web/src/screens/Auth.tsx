import { useState, type FormEvent, type ReactNode } from "react";
import { ApiFailure } from "@/api/client";
import { useSetup, useLogin } from "@/api/hooks";
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
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  function submit(e: FormEvent) {
    e.preventDefault();
    login.mutate({ username: username.trim(), password });
  }

  return (
    <AuthCard label="Sign in">
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
          disabled={login.isPending || !username.trim() || !password}
        >
          {login.isPending ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </AuthCard>
  );
}
