import { useEffect, useState } from "react";
import {
  usePublicCertificate,
  useUpdatePublicCertificate,
} from "@/api/hooks";
import type { PublicCertificate } from "@/api/types";
import { errorMessage } from "@/lib/errors";

/*
 * The opt-in public certificate (ADR 0081).
 *
 * Everything this section can do is something the server will then do on the
 * internet, so it says so before the switch rather than after: who is
 * contacted, why, and what has to be open for it to work. Off is the state of
 * every install that never opens this.
 *
 * The CA's terms are a tick box the admin ticks, never a default. Saving again
 * is "try now", which is why the button says so once it is on.
 */
export function PublicCertificateSettings() {
  const { data } = usePublicCertificate();
  if (!data) return null;
  return (
    <section className="settings__section">
      <span className="section-label">Public certificate</span>
      <div className="set-row__sub">
        Lets browsers trust this server at a name you own, with no certificate
        warning, and is what makes passkeys possible. While it is on, LANcast
        contacts the certificate authority you choose, only to get and renew
        this one certificate. Nothing is contacted while it is off.
      </div>
      {data.available ? (
        <PublicCertificateForm data={data} />
      ) : (
        <div className="set-row__sub">
          Unavailable: this server only listens on this computer, so it serves
          no HTTPS to put a certificate on.
        </div>
      )}
    </section>
  );
}

function PublicCertificateForm({ data }: { data: PublicCertificate }) {
  const update = useUpdatePublicCertificate();
  const [hostname, setHostname] = useState(data.hostname);
  const [ca, setCA] = useState(data.ca || data.cas[0]?.id || "");
  const [email, setEmail] = useState(data.email);
  const [agree, setAgree] = useState(false);

  // A save answers with the stored, normalized name; show that, so what is on
  // screen is what the server will ask for.
  useEffect(() => {
    setHostname(data.hostname);
    setEmail(data.email);
    if (data.ca) setCA(data.ca);
  }, [data.hostname, data.email, data.ca]);

  const chosen = data.cas.find((c) => c.id === ca);
  // Already agreed for this CA: saving again does not ask again. A different
  // CA is a different agreement, which is the server's rule too.
  const agreed = data.enabled && !!data.agreed_at && data.ca === ca;

  const turnOn = () =>
    update.mutate(
      { enabled: true, hostname, ca, email, agree_terms: agree },
      { onSuccess: () => setAgree(false) },
    );

  return (
    <>
      <Status data={data} />
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Name</div>
          <div className="set-row__sub">
            The name this server is reached by, such as example.duckdns.org. It
            must point at this server, and inbound port 443 must reach it: the
            authority checks the name by connecting there.
          </div>
        </div>
        <div className="set-row__actions">
          <input
            className="set-input"
            aria-label="Public name"
            placeholder="example.duckdns.org"
            value={hostname}
            onChange={(e) => setHostname(e.target.value)}
          />
        </div>
      </div>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Certificate authority</div>
          <div className="set-row__sub">
            {chosen && !chosen.trusted
              ? "For testing: its certificates are real but no browser trusts them, so the warning stays."
              : "Who issues the certificate."}
          </div>
        </div>
        <div className="set-row__actions">
          <select
            className="set-input"
            aria-label="Certificate authority"
            value={ca}
            onChange={(e) => setCA(e.target.value)}
          >
            {data.cas.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Email (optional)</div>
          <div className="set-row__sub">
            Only for expiry notices from the authority. Leave it empty and none
            is sent.
          </div>
        </div>
        <div className="set-row__actions">
          <input
            className="set-input"
            type="email"
            aria-label="Email for the certificate authority"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
      </div>
      {!agreed && chosen ? (
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={agree}
            onChange={(e) => setAgree(e.target.checked)}
          />
          I agree to {chosen.name.replace(/ \(.*\)$/, "")}&rsquo;s{" "}
          <a href={chosen.terms_url} target="_blank" rel="noreferrer">
            subscriber agreement
          </a>
        </label>
      ) : null}
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={update.isPending || !hostname.trim() || (!agreed && !agree)}
          onClick={turnOn}
        >
          {data.enabled ? "Save and try now" : "Turn on"}
        </button>
        {data.enabled ? (
          <button
            className="set-btn"
            disabled={update.isPending}
            onClick={() => update.mutate({ enabled: false })}
          >
            Turn off
          </button>
        ) : null}
      </div>
      {update.isError ? (
        <div className="set-row__sub" role="alert">
          {errorMessage(update.error)}
        </div>
      ) : null}
    </>
  );
}

const when = (iso?: string) => (iso ? new Date(iso).toLocaleString() : "");

function Status({ data }: { data: PublicCertificate }) {
  const st = data.status;
  let line: string;
  switch (st.state) {
    case "active":
      line = `Active for ${st.hostname}, until ${when(st.not_after)}. Renews on its own.`;
      break;
    case "pending":
      line = `Asking the authority for a certificate for ${st.hostname}…`;
      break;
    case "failing":
      line = st.not_after
        ? `The last renewal failed. The current certificate is still served until ${when(st.not_after)}.`
        : "The certificate could not be issued.";
      break;
    default:
      line = "Off.";
  }
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Status</div>
        <div className="set-row__sub">{line}</div>
        {st.state === "failing" && st.last_error ? (
          <div className="set-row__sub" role="alert">
            {st.last_error}
            {st.next_attempt ? ` Trying again ${when(st.next_attempt)}.` : ""}
          </div>
        ) : null}
      </div>
    </div>
  );
}
