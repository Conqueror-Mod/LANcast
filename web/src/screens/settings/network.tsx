import { useState } from "react";
import {
  useSettings,
  useUpdateSettings,
  useNetworkStatus,
  useCustomCertificate,
  useSetPort,
  useSetCustomCertificate,
} from "@/api/hooks";
import { asSentence, errorMessage } from "@/lib/errors";
import type { NetworkStatus } from "@/api/types";
import { answersWithin, goTo, urlAtPort } from "@/lib/portMove";
import { usePlaybackIfAny } from "@/playback/PlaybackProvider";
import { useQueryClient } from "@tanstack/react-query";

/*
 * Server › Network: this machine and the LAN, as a status.
 *
 * Read-only, and the sentence at the top is the point of the pane. Whether
 * the server answers the network is the loopback rule — no password, no LAN —
 * which CLAUDE.md says is not a setting, so there is no switch here to look
 * for. What there is: which side of that rule this server is on, why, and the
 * addresses somebody on the network would type.
 */
function NetworkSection() {
  const { data: net, isError } = useNetworkStatus();
  if (isError) {
    return (
      <section className="settings__section">
        <p className="set-error">The server did not answer. Try again in a moment.</p>
      </section>
    );
  }
  if (!net) return null;
  const addresses = Array.isArray(net.addresses) ? net.addresses : [];
  let status: string;
  if (net.lan_enabled) {
    status = addresses.length
      ? `Reachable on your network at ${addresses[0]}`
      : "Open to your network, but this machine reports no address other devices could use.";
  } else if (net.restart_required) {
    status = "A password is set. The network opens the next time the server restarts.";
  } else if (!net.secured) {
    status = "Only this computer can reach the server, because no password is set. Setting one opens it to your network.";
  } else {
    status = "Only this computer can reach the server: it was started bound to this machine alone.";
  }
  return (
    <section className="settings__section">
      <span className="section-label">This machine and your network</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">{status}</div>
          <div className="set-row__sub">
            Opening the server to your network follows the password:
            with none set it answers this computer only, and that is not a setting.
          </div>
        </div>
      </div>
      <PortRow net={net} />
      {addresses.length > 1 && (
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Every address</div>
            <div className="set-row__sub">
              {addresses.map((a) => (
                <div key={a}>
                  <code>{a}</code>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}
      {net.certificate_fingerprint && (
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Certificate</div>
            <div className="set-row__sub">
              {net.custom_certificate ? "One you supplied." : "Made by LANcast for this server."}{" "}
              A device that asks you to trust it should show this fingerprint:
              <br />
              <code className="set-network__pin">{net.certificate_fingerprint}</code>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

/*
 * The port, and moving it (ADR 0089).
 *
 * Moving it restarts the server, and three things outside LANcast still
 * point at the old port: the firewall, other devices' bookmarks, and
 * friends' servers. So the button asks once more, in the page rather than
 * with confirm() (a native dialog in this window loses keyboard focus), and
 * says those three things before anything happens.
 *
 * Afterwards the page follows the server: it waits for the new port to
 * answer and goes there. If the old port answers again instead, the server
 * rolled back, and the page says so. If neither has answered after a minute
 * it goes to the new port anyway, because a browser that has not yet trusted
 * the certificate there cannot see it answer, and the page it lands on is
 * where that question gets asked.
 */
function PortRow({ net }: { net: NetworkStatus }) {
  const setPort = useSetPort();
  const playback = usePlaybackIfAny();
  const qc = useQueryClient();
  const [draft, setDraft] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [moving, setMoving] = useState<string | null>(null);
  const [cameBack, setCameBack] = useState<string | null>(null);
  const last = net.last_port_change;
  const port = Number(draft);
  const valid = Number.isInteger(port) && port >= 1024 && port <= 65535 && draft !== net.port;

  const follow = async (to: string) => {
    setMoving(to);
    const target = urlAtPort(to);
    const started = Date.now();
    while (Date.now() - started < 60_000) {
      if (await answersWithin(new URL(target).origin, 1000)) {
        goTo(target);
        return;
      }
      if (Date.now() - started > 35_000 && (await answersWithin(window.location.origin, 1000))) {
        setMoving(null);
        setCameBack(to);
        qc.invalidateQueries({ queryKey: ["network"] });
        return;
      }
    }
    goTo(target);
  };

  if (!net.port_changeable) {
    return (
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Port {net.port}</div>
          {net.port_fixed_because && <div className="set-row__sub">It can&rsquo;t be changed here: {net.port_fixed_because}.</div>}
        </div>
      </div>
    );
  }
  if (moving) {
    return (
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title" role="status">Restarting on port {moving}&hellip;</div>
          <div className="set-row__sub">This page moves there when the server answers.</div>
        </div>
      </div>
    );
  }
  return (
    <>
      <form
        className="set-row"
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) setConfirming(true);
        }}
      >
        <label className="set-row__main">
          <span className="set-row__title">Port</span>
          <input
            className="set-input"
            inputMode="numeric"
            placeholder={net.port}
            value={draft}
            onChange={(e) => {
              setDraft(e.target.value.trim());
              setConfirming(false);
            }}
          />
        </label>
        <button className="set-btn" disabled={!valid || setPort.isPending}>
          Change port
        </button>
      </form>
      {confirming && (
        <div className="set-row" role="alertdialog" aria-label="Change the port">
          <div className="set-row__main">
            <div className="set-row__title">Restart the server on port {draft}?</div>
            <div className="set-row__sub">
              Anyone watching is interrupted. Other devices need the new address, and Windows
              Firewall has to allow port {draft}: LANcast doesn&rsquo;t make firewall rules. Friends&rsquo;
              servers keep the old port until you send them a new invite. If the server doesn&rsquo;t
              answer on the new port, it goes back to port {net.port} by itself.
            </div>
            <div className="set-row">
              <button
                className="set-btn"
                type="button"
                disabled={setPort.isPending}
                onClick={() => {
                  // The restart ends whatever is playing anyway; stopping it
                  // first means it ends cleanly rather than in a mini-player
                  // that nothing can control any more.
                  playback?.stop();
                  setPort.mutate(port, {
                    onSuccess: (r) => {
                      setConfirming(false);
                      void follow(r.port);
                    },
                  });
                }}
              >
                Restart on port {draft}
              </button>
              <button className="set-btn" type="button" onClick={() => setConfirming(false)}>
                Cancel
              </button>
            </div>
          </div>
        </div>
      )}
      {setPort.isError && <p className="set-error">{errorMessage(setPort.error)}</p>}
      {cameBack && (
        <p className="set-error" role="alert">
          The server didn&rsquo;t answer on port {cameBack}, so it went back to port {net.port}.
        </p>
      )}
      {!cameBack && last && !last.ok && <p className="set-error">{asSentence(last.error ?? "")}</p>}
    </>
  );
}

/*
 * Server › Network: trusted proxies.
 *
 * One address or network per line, saved as a whole. The server normalises
 * and validates the list, so what is shown after saving is what it enforces,
 * and a refusal is its sentence rather than one the client made up.
 */
export function TrustedProxiesSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const saved = settings?.trusted_proxies ?? [];
  const [draft, setDraft] = useState<string | null>(null);
  if (!settings) return null;
  const text = draft ?? saved.join("\n");
  const entries = text
    .split(/[\n,]/)
    .map((e) => e.trim())
    .filter((e) => e !== "");
  const changed = entries.join(",") !== saved.join(",");
  return (
    <section className="settings__section">
      <span className="section-label">Trusted proxies</span>
      <form
        className="set-row"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate({ trusted_proxies: entries }, { onSuccess: () => setDraft(null) });
        }}
      >
        <div className="set-row__main">
          <div className="set-row__title">Reverse proxies in front of LANcast</div>
          <div className="set-row__sub">
            Only if you run one, such as Caddy or nginx. LANcast believes the visitor&rsquo;s
            address these send, so sign-in limits and the account picker apply to the visitor,
            not to the proxy. One address or network per line, for example 127.0.0.1. Leave it
            empty if nothing sits in front of LANcast.
          </div>
          <textarea
            className="set-input"
            rows={3}
            spellCheck={false}
            aria-label="Trusted proxies"
            value={text}
            onChange={(e) => setDraft(e.target.value)}
          />
        </div>
        <button className="set-btn" disabled={!changed || update.isPending}>
          Save
        </button>
      </form>
      {update.isError && <p className="set-error">{errorMessage(update.error)}</p>}
    </section>
  );
}

/*
 * Server › Network: a certificate of your own (ADR 0088).
 *
 * Two paths, because that is what a renewal tool writes to: the server
 * re-reads the files when they change. The status line is about the
 * certificate other devices are actually served, which is LANcast's own
 * while the configured one will not load, and the error says why in the
 * server's words. Saving checks the pair first, so a mistake comes back here
 * as a sentence rather than as a browser warning on somebody's phone.
 */
function CustomCertificateSection() {
  const { data: cert } = useCustomCertificate();
  const save = useSetCustomCertificate();
  const [certFile, setCertFile] = useState<string | null>(null);
  const [keyFile, setKeyFile] = useState<string | null>(null);
  if (!cert) return null;
  if (!cert.available) {
    return (
      <section className="settings__section">
        <span className="section-label">Your own certificate</span>
        <p className="set-row__sub">
          This server answers this computer only, so it serves no HTTPS to put a certificate on.
        </p>
      </section>
    );
  }
  const configured = cert.cert_file !== "" || cert.key_file !== "";
  const certValue = certFile ?? cert.cert_file;
  const keyValue = keyFile ?? cert.key_file;
  const expires = cert.not_after ? new Date(cert.not_after).toLocaleDateString() : "";
  const names = Array.isArray(cert.names) ? cert.names : [];
  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    save.mutate(
      { cert_file: certValue.trim(), key_file: keyValue.trim() },
      { onSuccess: () => { setCertFile(null); setKeyFile(null); } },
    );
  };
  return (
    <section className="settings__section">
      <span className="section-label">Your own certificate</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">
            {cert.serving === "custom"
              ? `Other devices are served your certificate for ${cert.subject || names[0] || "this server"}.`
              : "Other devices are served the certificate LANcast made."}
          </div>
          <div className="set-row__sub">
            {cert.serving === "custom" ? (
              <>
                {names.length > 0 && <>Covers {names.join(", ")}. </>}
                {cert.issuer && <>From {cert.issuer}. </>}
                {expires && <>Expires {expires}.</>}
              </>
            ) : (
              <>
                Browsers warn about it until they are told to trust it. If you have a certificate
                for this server, from your own authority or a public one, give its files here.
              </>
            )}
          </div>
        </div>
      </div>
      {cert.error && (
        <p className="set-error" role="alert">
          {asSentence(cert.error)}{" "}
          {cert.serving === "custom"
            ? "Still serving the copy loaded before."
            : "Other devices get LANcast’s certificate until this is fixed."}
        </p>
      )}
      <form onSubmit={submit}>
        <label className="set-row">
          <span>Certificate file</span>
          <input
            className="set-input"
            placeholder="C:\Certs\media.pem"
            value={certValue}
            onChange={(e) => setCertFile(e.target.value)}
          />
        </label>
        <label className="set-row">
          <span>Key file</span>
          <input
            className="set-input"
            placeholder="C:\Certs\media-key.pem"
            value={keyValue}
            onChange={(e) => setKeyFile(e.target.value)}
          />
        </label>
        <p className="set-row__sub">
          Full paths to PEM files the server can read. The server re-reads them when they change,
          so a renewal tool can update them in place. This computer&rsquo;s own LANcast window keeps
          LANcast&rsquo;s certificate, and the public certificate&rsquo;s name keeps that one.
        </p>
        <div className="set-row">
          <button
            className="set-btn"
            disabled={save.isPending || !certValue.trim() || !keyValue.trim()}
          >
            Use this certificate
          </button>
          {configured && (
            <button
              className="set-btn"
              type="button"
              disabled={save.isPending}
              onClick={() =>
                save.mutate(null, { onSuccess: () => { setCertFile(null); setKeyFile(null); } })
              }
            >
              Use LANcast&rsquo;s certificate
            </button>
          )}
        </div>
      </form>
      {save.isError && <span className="set-error">{errorMessage(save.error)}</span>}
    </section>
  );
}

export function NetworkPane() {
  return (
    <>
      <NetworkSection />
      <TrustedProxiesSection />
      <CustomCertificateSection />
    </>
  );
}
