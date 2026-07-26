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
export function Setup({ lanEnabled }: { lanEnabled: boolean }) {
  const setup = useSetup();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  function submit(e: FormEvent) {
    e.preventDefault();
    setup.mutate({ username: username.trim(), password });
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
        {setup.isError && (
          <p className="auth__error" role="alert">
            {errorMessage(setup.error)}
          </p>
        )}
        {!lanEnabled && (
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
