import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import {
  useDecidePairing,
  useLookupPairing,
  type PendingPairing,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import "./Settings.css";

/*
 * Pair a device (ADR 0125): the screen a person types a TV's code into.
 *
 * Three steps, each a different question, so each is its own state rather
 * than one form that grows: which code, is this the device you mean, and the
 * answer.
 *
 * The middle step is the phishing defence the ADR settles for. A device flow's
 * known weakness is somebody showing a person a code and asking them to type
 * it in, so before anything is approved the screen names the device, where it
 * asked from, and says plainly that it is about to be given this account. The
 * password is asked for on the same screen as that sentence, not before it.
 */
export function Pair() {
  const [params] = useSearchParams();
  const [code, setCode] = useState(params.get("code") ?? "");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState<PendingPairing | null>(null);
  const [done, setDone] = useState<{ name: string; approved: boolean } | null>(
    null,
  );
  const lookup = useLookupPairing();
  const decide = useDecidePairing();

  const restart = () => {
    setPending(null);
    setDone(null);
    setCode("");
    setPassword("");
    lookup.reset();
    decide.reset();
  };

  return (
    <div className="browse pair">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Pair a device</h1>
      </div>

      {done ? (
        <section className="settings__section pair__done" aria-live="polite">
          {/*
            The answer is the point of the screen, so it is said large. Not in
            gold: gold means where you are, and a result is not a place.
          */}
          <div
            className={
              "pair__result" + (done.approved ? "" : " pair__result--refused")
            }
          >
            <span className="pair__mark" aria-hidden="true">
              {done.approved ? "✓" : "✕"}
            </span>
            <div>
              <h2 className="pair__heading">
                {done.approved ? "Paired" : "Refused"}
              </h2>
              <p className="pair__device">{done.name}</p>
            </div>
          </div>
          {done.approved ? (
            <p className="set-row__sub">
              The TV signs in as you within a few seconds. Its key is listed
              under <Link to="/settings">Settings</Link> › Security › API keys,
              where revoking it signs the TV out.
            </p>
          ) : (
            <p className="set-row__sub">
              The TV will say so, and can show a new code to ask again.
            </p>
          )}
          <div className="pair__actions">
            <button className="set-btn" onClick={restart}>
              Pair another
            </button>
            <Link className="set-btn" to="/settings">
              Back to Settings
            </Link>
          </div>
        </section>
      ) : pending ? (
        <section className="settings__section">
          <p className="set-row__sub">
            <strong>
              This gives “{pending.name}” ({pending.device}) your account.
            </strong>{" "}
            It will see everything you can see, until you revoke it. It asked
            from {pending.address} at{" "}
            {new Date(pending.created_at * 1000).toLocaleTimeString()}. Only
            approve a code you are reading off your own screen.
          </p>
          <form
            className="set-add"
            onSubmit={(e) => {
              e.preventDefault();
              if (!password) return;
              decide.mutate(
                {
                  code: pending.code,
                  approve: true,
                  current_password: password,
                },
                {
                  onSuccess: (r) => {
                    setPassword("");
                    setDone({ name: r.name, approved: true });
                  },
                },
              );
            }}
          >
            {/*
              The password, because approving ends in a key and a key outlives
              every session (ADR 0061, amended 2026-11-16).
            */}
            <input
              className="set-input"
              type="password"
              placeholder="Your password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
            <button
              className="set-btn"
              type="submit"
              disabled={decide.isPending || !password}
            >
              {decide.isPending ? "Approving…" : "Approve"}
            </button>
            <button
              className="set-btn set-btn--danger"
              type="button"
              disabled={decide.isPending}
              onClick={() =>
                decide.mutate(
                  { code: pending.code, approve: false },
                  {
                    onSuccess: (r) => setDone({ name: r.name, approved: false }),
                  },
                )
              }
            >
              Refuse
            </button>
            {decide.isError && (
              <span className="set-error">{errorMessage(decide.error)}</span>
            )}
          </form>
        </section>
      ) : (
        <section className="settings__section">
          <p className="set-row__sub">
            Type the code your TV is showing. Letters or digits, with or
            without the dash.
          </p>
          <form
            className="set-add"
            onSubmit={(e) => {
              e.preventDefault();
              if (!code.trim()) return;
              lookup.mutate(code, { onSuccess: (p) => setPending(p) });
            }}
          >
            <input
              className="set-input pair__code"
              type="text"
              placeholder="ABCD-1234"
              autoComplete="off"
              autoCapitalize="characters"
              spellCheck={false}
              maxLength={12}
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
            <button
              className="set-btn"
              type="submit"
              disabled={lookup.isPending || !code.trim()}
            >
              {lookup.isPending ? "Looking…" : "Continue"}
            </button>
            {lookup.isError && (
              <span className="set-error">{errorMessage(lookup.error)}</span>
            )}
          </form>
        </section>
      )}
    </div>
  );
}
