import { useState } from "react";
import { useCreateMetricsToken, useRevokeMetricsToken, useSettings } from "@/api/hooks";
import { errorMessage } from "@/lib/errors";

/*
 * Metrics for Prometheus, in Troubleshooting (ADR 0096).
 *
 * The token is shown once, in the answer that made it, and this screen keeps
 * it only until it is left. After that the server can say a token exists and
 * when it was made, and nothing more, so the copy-it-now sentence is the
 * important one.
 *
 * Replacing and revoking ask once more in the page, rather than with
 * confirm(): a native dialog in the LANcast window loses keyboard focus when
 * it closes. Both stop a scraper that is working, which is worth a second
 * press.
 */
export function MetricsSettings() {
  const { data: settings } = useSettings(true);
  const create = useCreateMetricsToken();
  const revoke = useRevokeMetricsToken();
  const [shown, setShown] = useState<string | null>(null);
  const [armed, setArmed] = useState<"replace" | "revoke" | null>(null);
  if (!settings) return null;
  const token = settings.metrics_token;
  const origin = typeof window === "undefined" ? "" : window.location.origin;

  const make = () =>
    create.mutate(undefined, {
      onSuccess: (r) => {
        setShown(r.token);
        setArmed(null);
      },
    });

  return (
    <section className="settings__section">
      <span className="section-label">Metrics</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Metrics for Prometheus</div>
          <div className="set-row__sub">
            If you run Prometheus or Grafana, it can read this server&rsquo;s numbers from{" "}
            <code>{origin}/api/metrics</code>: conversions, library sizes, scans, backups and
            errors. Nothing about who watches what. A scraper can&rsquo;t sign in, so it uses a
            token that reads metrics and nothing else.
            <br />
            {token.configured
              ? `A token was made ${new Date(token.created_at * 1000).toLocaleString()}.`
              : "No token yet, so only an administrator signed in here can read them."}
          </div>
        </div>
        <div className="set-row__actions">
          {!token.configured && (
            <button className="set-btn" disabled={create.isPending} onClick={make}>
              Make a token
            </button>
          )}
          {token.configured && armed === null && (
            <>
              <button className="set-btn" onClick={() => setArmed("replace")}>
                Replace
              </button>
              <button className="set-btn" onClick={() => setArmed("revoke")}>
                Revoke
              </button>
            </>
          )}
        </div>
      </div>

      {armed && (
        <div className="set-row" role="alertdialog" aria-label={armed === "replace" ? "Replace the token" : "Revoke the token"}>
          <div className="set-row__main">
            <div className="set-row__title">
              {armed === "replace" ? "Make a new token?" : "Revoke the token?"}
            </div>
            <div className="set-row__sub">
              {armed === "replace"
                ? "The current token stops working at once. Anything scraping with it needs the new one."
                : "Anything scraping with it stops getting numbers until you make a new one."}
            </div>
            <div className="set-row">
              {armed === "replace" ? (
                <button className="set-btn" type="button" disabled={create.isPending} onClick={make}>
                  Make a new token
                </button>
              ) : (
                <button
                  className="set-btn"
                  type="button"
                  disabled={revoke.isPending}
                  onClick={() =>
                    revoke.mutate(undefined, {
                      onSuccess: () => {
                        setArmed(null);
                        setShown(null);
                      },
                    })
                  }
                >
                  Revoke it
                </button>
              )}
              <button className="set-btn" type="button" onClick={() => setArmed(null)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}

      {shown && (
        <div className="set-row" role="status">
          <div className="set-row__main">
            <div className="set-row__title">Copy this now. It isn&rsquo;t shown again.</div>
            <div className="set-row__sub">
              <code className="set-network__pin">{shown}</code>
              <br />
              In Prometheus, give the scrape job <code>authorization: {"{"} credentials: &lt;this token&gt; {"}"}</code>{" "}
              and <code>metrics_path: /api/metrics</code>.
            </div>
          </div>
        </div>
      )}
      {(create.isError || revoke.isError) && (
        <p className="set-error">{errorMessage(create.error ?? revoke.error)}</p>
      )}
    </section>
  );
}
