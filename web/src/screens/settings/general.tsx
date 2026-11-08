import {
  useSettings,
  useUpdateSettings,
  useHealth,
  useServerIdentity,
} from "@/api/hooks";
import { UpdateSettings } from "@/components/UpdateSettings";
import { groupFingerprint } from "./shared";

// Megabytes, rounded, because nobody reading a download size wants three
// decimal places of a hundred-megabyte number.
function GeneralSection() {
  const { data: health } = useHealth();
  const { data: settings } = useSettings(true);
  const { data: identity } = useServerIdentity();
  return (
    <section className="settings__section">
      <span className="section-label">Server</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Version</div>
          <div className="set-row__sub">
            {health ? `LANcast ${health.version}` : "…"}
          </div>
        </div>
      </div>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">API version</div>
          <div className="set-row__sub">
            {/* Shown because it is the number a third-party client is built
                against (ADR 0018), and the only place it was visible before was
                a response header nobody reads. */}
            {health ? health.api_version : "…"}
          </div>
        </div>
      </div>
      {/*
        Two fingerprints, and the order is the point.

        They answer different questions and have different lifetimes, which is
        the correction the amendment to ADR 0070 makes. The **identity**
        (ADR 0044) is generated once and never regenerated, survives being
        restored from a backup, and is what says this is the same server. The
        **TLS certificate fingerprint** secures the connection, and it is
        reissued for ordinary reasons -- a reinstall, a deleted certificate
        picking up a new network address, an operator rotating one they
        supplied.

        Neither is called a "key" on this screen, deliberately. A key is
        something you are given and that lets you in; the thing that pairs two
        servers is an *invite* (ADR 0044), and somebody handed one of those
        reasonably expects it to grant access. Both values here grant nothing
        whatsoever. They are compared, never presented.

        Identity first, because it is the durable one and the one somebody
        checking a changed certificate is sent here to read. The certificate
        fingerprint is still shown, because it is the only value both ends can
        compare before anyone has signed in, which is exactly the first-contact
        case.

        Neither is a secret. The identity's public half is what an invite
        carries; the certificate fingerprint is handed to anyone who opens a TLS
        connection. Both get their protection from being compared out of band.
      */}
      {identity?.fingerprint_display ? (
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">Identity fingerprint</div>
            <div className="set-row__sub set-row__sub--mono">
              {identity.fingerprint_display}
            </div>
            <div className="set-row__sub">
              Who this server is. It does not change when the certificate is
              reissued, and it survives being restored from a backup onto
              another machine &mdash; so this is the one to read out when somebody
              is checking that this is still the same server.
            </div>
          </div>
        </div>
      ) : null}
      {settings?.certificate_fingerprint ? (
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">TLS certificate fingerprint</div>
            <div className="set-row__sub set-row__sub--mono">
              {groupFingerprint(settings.certificate_fingerprint)}
            </div>
            <div className="set-row__sub">
              Read this out to somebody adding this
              server on another computer for the first time, before they have
              signed in. It is reissued for ordinary reasons, so a change in it
              is not by itself a problem. Not a secret.
            </div>
          </div>
        </div>
      ) : null}
      {/*
        The source link, and it is a licence obligation rather than a courtesy.

        LANcast is AGPL-3.0 (ADR 0053). Section 13 requires a modified version
        offered to people over a network to give those people its source, and
        this server is exactly that shape — a thing other people in the house
        use through a browser. Shipping the affordance means a fork inherits
        compliance instead of having to remember it, which is the only version
        of this that actually works.

        A plain link rather than a bundled tarball: the licence asks for access
        through customary means, and for a public repository this is it.
        Somebody who forks and modifies has to point it at their own.
      */}
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Licence</div>
          <div className="set-row__sub">
            AGPL-3.0 — you are entitled to the source of the version you are
            using.{" "}
            <a
              href="https://github.com/Conqueror-Mod/LANcast"
              target="_blank"
              rel="noreferrer"
            >
              Source code
            </a>
          </div>
        </div>
      </div>
      {/* What LANcast sends, to whom and when, and the emulator cores it plays
          with (ADR 0112 step 6: RetroAchievements asks that both be findable). */}
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Privacy</div>
          <div className="set-row__sub">
            Everything LANcast sends beyond your network, and when.{" "}
            <a href="https://github.com/Conqueror-Mod/LANcast/blob/main/docs/privacy.md" target="_blank" rel="noreferrer">
              Privacy
            </a>
            {" · "}
            <a href="https://github.com/Conqueror-Mod/LANcast/blob/main/docs/cores.md" target="_blank" rel="noreferrer">
              Emulator cores and licences
            </a>
          </div>
        </div>
      </div>
    </section>
  );
}

function UpdateCheckSection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  if (!settings) return null;
  return (
    <section className="settings__section">
      <span className="section-label">Update checks</span>
      <label className="set-toggle">
        <input
          type="checkbox"
          checked={settings.update_check}
          onChange={(e) => update.mutate({ update_check: e.target.checked })}
        />
        Check for new LANcast releases
      </label>
      <div className="set-row__sub">
        Turning this off stops the server contacting GitHub. Nothing else here
        reaches the internet on its own.
      </div>
    </section>
  );
}

export function GeneralPane() {
  return (
    <>
      <GeneralSection />
      <UpdateSettings />
      <UpdateCheckSection />
    </>
  );
}
