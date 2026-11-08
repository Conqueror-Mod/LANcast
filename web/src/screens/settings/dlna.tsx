import { useState } from "react";
import {
  useSettings,
  useUpdateSettings,
  useUsers,
  useDLNAStatus,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";

/*
 * Server › DLNA (ADR 0086): TVs, consoles and VLC on the network browse and
 * play as one chosen account.
 *
 * The status line comes first and says why not, because the usual failure is
 * a TV that finds nothing, and that should have an answer here rather than
 * look like a LANcast bug. The firewall note is part of that answer: the
 * installer deliberately makes no firewall rules, and Windows drops what a
 * service is sent unless a rule allows it.
 */
export function DLNASection() {
  const { data: settings } = useSettings(true);
  const update = useUpdateSettings();
  const status = useDLNAStatus();
  const { data: users } = useUsers();
  const [name, setName] = useState<string | null>(null);
  if (!settings) return null;
  const s = status.data;
  const accounts = Array.isArray(users) ? users : [];
  return (
    <>
      <section className="settings__section">
        <span className="section-label">TVs and consoles</span>
        <div className="set-row">
          <div className="set-row__main">
            <div className="set-row__title">
              {s?.running
                ? `Running. TVs on your network can find "${settings.dlna_name || "LANcast"}".`
                : `Not running: ${s?.reason ?? "off"}.`}
            </div>
            <div className="set-row__sub">
              DLNA lets a smart TV, a games console or VLC browse and play your library with no
              LANcast app. It answers as the account you choose below, so a TV sees exactly what
              that account sees. Nothing played this way is recorded in anyone&rsquo;s history.
            </div>
          </div>
        </div>
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={!!settings.dlna_enabled}
            disabled={update.isPending}
            onChange={(e) => update.mutate({ dlna_enabled: e.target.checked })}
          />
          Let TVs and consoles on your network find this server
        </label>
        <label className="set-row">
          <span>Browse as</span>
          <select
            className="set-input"
            value={settings.dlna_account ?? ""}
            onChange={(e) => update.mutate({ dlna_account: e.target.value })}
          >
            <option value="">Choose an account…</option>
            {accounts.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name}
                {u.role === "admin" ? " (administrator: sees everything)" : ""}
              </option>
            ))}
          </select>
        </label>
        <label className="set-row">
          <span>Name on TVs</span>
          <input
            className="set-input"
            maxLength={64}
            placeholder="LANcast"
            value={name ?? settings.dlna_name ?? ""}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => {
              if (name !== null && name !== (settings.dlna_name ?? "")) {
                update.mutate({ dlna_name: name.trim() });
              }
              setName(null);
            }}
          />
        </label>
        {update.isError && <span className="set-error">{errorMessage(update.error)}</span>}
      </section>
      <section className="settings__section">
        <span className="section-label">If a TV finds nothing</span>
        <p className="set-row__sub set-row__sub--standalone">
          Windows blocks what a service is sent unless a rule allows it, and LANcast does not make
          firewall rules for you. DLNA needs TCP {s?.port ?? 8200} and UDP 1900 allowed on your
          private network. In PowerShell, as administrator:
        </p>
        <pre className="set-network__pin">
          {`New-NetFirewallRule -DisplayName "LANcast DLNA" -Direction Inbound -Protocol TCP -LocalPort ${s?.port ?? 8200} -Profile Private -Action Allow
New-NetFirewallRule -DisplayName "LANcast DLNA discovery" -Direction Inbound -Protocol UDP -LocalPort 1900 -Profile Private -Action Allow`}
        </pre>
      </section>
    </>
  );
}
