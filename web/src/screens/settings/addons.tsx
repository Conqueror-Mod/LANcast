import { useRef, useState } from "react";
import {
  usePlugins,
  useUploadPlugin,
  useGrantPlugin,
  useSetPluginEnabled,
  useSetPluginSecret,
  useRemovePlugin,
} from "@/api/hooks";
import type { Plugin } from "@/api/types";

const SIGNER_LABEL: Record<string, string> = {
  first_party: "First-party",
  pinned: "Pinned",
  unsigned: "Unsigned",
};

// A short, human list of a capability set for the approval dialog and rows.
function capSummary(caps: { http: string[]; secrets: string[] }): string[] {
  const lines: string[] = [];
  for (const h of caps.http) lines.push(`Reach ${h}`);
  for (const s of caps.secrets) lines.push(`Read your ${s.replace(/_/g, " ")}`);
  return lines;
}

// The capability-approval dialog: the whole point of the two-step install. It
// states plainly what a staged plugin wants before the operator grants it.
function GrantDialog({
  plugin,
  onClose,
}: {
  plugin: Plugin;
  onClose: () => void;
}) {
  const grant = useGrantPlugin();
  const wants = capSummary(plugin.requested);
  return (
    <div className="addon-dialog__scrim" role="dialog" aria-modal="true">
      <div className="addon-dialog">
        <div className="addon-dialog__title">
          Grant <strong>{plugin.name}</strong>?
        </div>
        <div className="addon-dialog__signer">
          {SIGNER_LABEL[plugin.signer] ?? plugin.signer}
          {plugin.signer === "unsigned" && " — you accept the risk"}
        </div>
        <p className="addon-dialog__lead">This add-on is asking to:</p>
        {wants.length > 0 ? (
          <ul className="addon-dialog__caps">
            {wants.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        ) : (
          <p className="addon-dialog__caps">
            Nothing beyond running — no network, no secrets.
          </p>
        )}
        <div className="addon-dialog__actions">
          <button
            className="set-btn"
            onClick={onClose}
            disabled={grant.isPending}
          >
            Cancel
          </button>
          <button
            className="set-btn set-btn--primary"
            disabled={grant.isPending}
            onClick={() =>
              grant.mutate(
                { name: plugin.name, caps: plugin.requested },
                { onSuccess: onClose },
              )
            }
          >
            Grant &amp; enable
          </button>
        </div>
        {grant.isError && (
          <span className="set-error">{(grant.error as Error).message}</span>
        )}
      </div>
    </div>
  );
}

/*
 * One granted secret, and whether the plugin can actually read it.
 *
 * This exists because "granted" was being read as "working". A plugin could be
 * granted a secret, be listed as granted, and read nothing — the host resolved
 * secrets from a fixed list of the server's own provider keys and answered
 * empty for every other name. So the approval dialog told an operator the
 * plugin would read a key that it never could.
 *
 * The state is therefore the point of this row, not the input: **needs a
 * value** is the thing worth saying out loud. Named in words rather than
 * coloured, because `design.md` reserves the one accent for where you are, and
 * a second meaning for gold would kill the first.
 *
 * No value is ever shown. The server does not return one — `secrets_configured`
 * carries names only — so there is nothing here to reveal, and the field is
 * always empty on arrival even for a secret that is set.
 */
function AddonSecret({ plugin, secret }: { plugin: Plugin; secret: string }) {
  const set = useSetPluginSecret();
  const [value, setValue] = useState("");
  const configured = plugin.secrets_configured.includes(secret);

  const save = (next: string) =>
    set.mutate(
      { name: plugin.name, secret, value: next },
      { onSuccess: () => setValue("") },
    );

  return (
    <div className="set-row set-row--stacked">
      <div className="set-row__main">
        <div className="set-row__title">{secret}</div>
        <div className="set-row__sub">
          {configured
            ? "Set. The add-on reads this; nobody can read it back out."
            : "Needs a value — until you give it one the add-on reads nothing."}
        </div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input"
          type="password"
          autoComplete="off"
          placeholder={configured ? "Replace…" : "Paste the key…"}
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <button
          className="set-btn"
          onClick={() => save(value)}
          disabled={value === "" || set.isPending}
        >
          Save
        </button>
        {configured && (
          <button
            className="set-btn set-btn--danger"
            onClick={() => save("")}
            disabled={set.isPending}
          >
            Clear
          </button>
        )}
      </div>
      {set.isError && (
        <span className="set-error">{(set.error as Error).message}</span>
      )}
    </div>
  );
}

function AddonRow({ plugin }: { plugin: Plugin }) {
  const setEnabled = useSetPluginEnabled();
  const remove = useRemovePlugin();
  const [confirming, setConfirming] = useState(false);
  const granted = capSummary(plugin.granted);
  // Granted but with nothing to read. Surfaced on the row itself so it is
  // visible without opening anything — it is the difference between an add-on
  // that works and one that quietly does nothing.
  const missingSecrets = plugin.granted.secrets.filter(
    (s) => !plugin.secrets_configured.includes(s),
  );
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          {plugin.name} <span className="addon-version">v{plugin.version}</span>{" "}
          <span className={"addon-signer addon-signer--" + plugin.signer}>
            {SIGNER_LABEL[plugin.signer] ?? plugin.signer}
          </span>
        </div>
        <div className="set-row__sub">
          {plugin.enabled ? "Enabled" : "Disabled"}
          {granted.length > 0
            ? " · " + granted.join(" · ")
            : " · no capabilities"}
          {missingSecrets.length > 0 &&
            ` · ${missingSecrets.length === 1 ? "1 key needs" : `${missingSecrets.length} keys need`} a value`}
        </div>
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          onClick={() =>
            setEnabled.mutate({ name: plugin.name, enabled: !plugin.enabled })
          }
          disabled={setEnabled.isPending}
        >
          {plugin.enabled ? "Disable" : "Enable"}
        </button>
        {confirming ? (
          <>
            <span className="set-confirm">Remove?</span>
            <button
              className="set-btn set-btn--danger"
              onClick={() => remove.mutate(plugin.name)}
              disabled={remove.isPending}
            >
              Yes
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              No
            </button>
          </>
        ) : (
          <button
            className="set-btn set-btn--danger"
            onClick={() => setConfirming(true)}
          >
            Remove
          </button>
        )}
      </div>
      {plugin.granted.secrets.length > 0 && (
        <div className="addon-secrets">
          {plugin.granted.secrets.map((secret) => (
            <AddonSecret key={secret} plugin={plugin} secret={secret} />
          ))}
        </div>
      )}
    </div>
  );
}

export function AddonsSection() {
  const { data: plugins } = usePlugins();
  const upload = useUploadPlugin();
  const fileRef = useRef<HTMLInputElement>(null);
  const [staged, setStaged] = useState<Plugin | null>(null);

  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-selecting the same file
    if (!file) return;
    const buf = await file.arrayBuffer();
    upload.mutate(buf, { onSuccess: (p) => setStaged(p) });
  };

  return (
    <section className="settings__section">
      <span className="section-label">Add-ons</span>

      {plugins && plugins.length > 0 ? (
        <div className="set-lib">
          {plugins.map((p) => (
            <AddonRow key={p.name} plugin={p} />
          ))}
        </div>
      ) : (
        <p className="set-row__sub">No add-ons installed.</p>
      )}

      <input
        ref={fileRef}
        type="file"
        accept=".lcplugin"
        onChange={onFile}
        style={{ display: "none" }}
      />
      <button
        className="set-btn"
        onClick={() => fileRef.current?.click()}
        disabled={upload.isPending}
      >
        {upload.isPending ? "Verifying…" : "Install add-on…"}
      </button>
      {upload.isError && (
        <span className="set-error">{(upload.error as Error).message}</span>
      )}

      {staged && (
        <GrantDialog plugin={staged} onClose={() => setStaged(null)} />
      )}
    </section>
  );
}
