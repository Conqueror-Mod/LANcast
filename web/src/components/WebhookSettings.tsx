import { useState } from "react";
import {
  useCreateWebhook,
  useDeleteWebhook,
  usePatchWebhook,
  useTestWebhook,
  useWebhooks,
} from "@/api/hooks";
import type { Webhook, WebhookCreate, WebhookStatus } from "@/api/types";
import { errorMessage } from "@/lib/errors";

/*
 * Server › Webhooks (ADR 0097).
 *
 * What this screen has to make plain: LANcast sends nothing anywhere until
 * somebody adds a webhook here; playback is only reported for people who
 * turned that on themselves; and the secret is shown once. The last outcome
 * of each webhook is on its row, because a webhook that has been failing
 * quietly for a week is the usual way these break.
 *
 * Delete asks twice by arming the row, the pattern the backup list uses,
 * rather than confirm(): a native dialog in the LANcast window loses
 * keyboard focus when it closes.
 */

const EVENT_LABELS: Record<string, string> = {
  "playback.started": "Someone starts watching",
  "playback.stopped": "Someone stops watching",
  "scan.finished": "A library scan finishes",
  "backup.taken": "A backup is taken",
};

function describeLast(last: WebhookStatus | null | undefined): string {
  if (!last) return "Nothing sent yet.";
  const when = new Date(last.at * 1000).toLocaleString();
  if (last.ok) return `Last sent ${when}: delivered.`;
  return `Last sent ${when}: failed after ${last.tries === 1 ? "one try" : `${last.tries} tries`}, ${last.error ?? "no reason given"}.`;
}

export function WebhookSettings() {
  const { data, error } = useWebhooks(true);
  const create = useCreateWebhook();
  const [url, setURL] = useState("");
  const [events, setEvents] = useState<string[]>([]);
  const [secret, setSecret] = useState<{ id: string; value: string } | null>(null);

  const all = data?.events ?? Object.keys(EVENT_LABELS);
  const hooks = data?.webhooks ?? [];
  const full = hooks.length >= 10;

  return (
    <section className="settings__section">
      <span className="section-label">Webhooks</span>
      <div className="set-row">
        <div className="set-row__main">
          <div className="set-row__title">Tell another program when something happens</div>
          <div className="set-row__sub">
            LANcast sends a message to each address here when the things you pick happen, for
            example to Home Assistant or a Discord channel. Nothing is sent anywhere until you add
            one. Viewing is only reported for people who turned that on themselves, in Your account
            › Privacy.
          </div>
        </div>
      </div>

      {error && <p className="set-error">{errorMessage(error)}</p>}

      {hooks.map((h) => (
        <WebhookRow key={h.id} hook={h} labels={EVENT_LABELS} />
      ))}

      {secret && (
        <div className="set-row" role="status">
          <div className="set-row__main">
            <div className="set-row__title">Copy the secret now. It isn&rsquo;t shown again.</div>
            <div className="set-row__sub">
              <code className="set-network__pin">{secret.value}</code>
              <br />
              Each message carries <code>X-LANcast-Signature: sha256=…</code>, the HMAC-SHA256 of the
              body with this secret, so the receiver can check it came from LANcast.
            </div>
          </div>
        </div>
      )}

      {!full && (
        <form
          className="set-row"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              { url: url.trim(), events: events as WebhookCreate["events"] },
              {
                onSuccess: (r) => {
                  setSecret({ id: r.webhook.id, value: r.secret });
                  setURL("");
                  setEvents([]);
                },
              },
            );
          }}
        >
          <div className="set-row__main">
            <div className="set-row__title">Add a webhook</div>
            <input
              className="set-input"
              type="url"
              placeholder="http://homeassistant.local:8123/api/webhook/lancast"
              aria-label="Webhook address"
              value={url}
              onChange={(e) => setURL(e.target.value)}
            />
            {all.map((ev) => (
              <label key={ev} className="set-toggle">
                <input
                  type="checkbox"
                  checked={events.includes(ev)}
                  onChange={(e) =>
                    setEvents((cur) => (e.target.checked ? [...cur, ev] : cur.filter((x) => x !== ev)))
                  }
                />
                {EVENT_LABELS[ev] ?? ev}
              </label>
            ))}
            {create.isError && <p className="set-error">{errorMessage(create.error)}</p>}
          </div>
          <button className="set-btn" disabled={!url.trim() || events.length === 0 || create.isPending}>
            Add
          </button>
        </form>
      )}
      {full && (
        <div className="set-row__sub set-row__sub--standalone">Ten is the most a server keeps.</div>
      )}
    </section>
  );
}

function WebhookRow({ hook, labels }: { hook: Webhook; labels: Record<string, string> }) {
  const patch = usePatchWebhook();
  const remove = useDeleteWebhook();
  const test = useTestWebhook();
  const [armed, setArmed] = useState(false);
  const failing = hook.last && !hook.last.ok;

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">
          <code>{hook.url}</code>
        </div>
        <div className="set-row__sub">
          {hook.events.map((e) => labels[e] ?? e).join(" · ")}
          <br />
          {failing ? <strong>{describeLast(hook.last)}</strong> : describeLast(hook.last)}
          {test.data && (
            <>
              <br />
              {test.data.ok
                ? `Test delivered (${test.data.status}, ${test.data.took_ms} ms).`
                : `Test failed: ${test.data.error ?? "no reason given"}.`}
            </>
          )}
        </div>
        <label className="set-toggle">
          <input
            type="checkbox"
            checked={hook.enabled}
            onChange={(e) => patch.mutate({ id: hook.id, patch: { enabled: e.target.checked } })}
          />
          On
        </label>
        {(patch.isError || remove.isError || test.isError) && (
          <p className="set-error">{errorMessage(patch.error ?? remove.error ?? test.error)}</p>
        )}
      </div>
      <div className="set-row__actions">
        <button className="set-btn" disabled={test.isPending} onClick={() => test.mutate(hook.id)}>
          {test.isPending ? "Sending…" : "Test"}
        </button>
        <button
          className="set-btn"
          disabled={remove.isPending}
          onClick={() => (armed ? remove.mutate(hook.id) : setArmed(true))}
        >
          {armed ? "Remove for good?" : "Remove"}
        </button>
      </div>
    </div>
  );
}

export function PlaybackWebhooksToggle({
  on,
  onChange,
}: {
  on: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <label className="set-toggle set-toggle--described">
      <input type="checkbox" checked={on} onChange={(e) => onChange(e.target.checked)} />
      <span>
        <strong>Let this server&rsquo;s webhooks report what I watch</strong>
        <span className="set-toggle__desc">
          If an administrator has set up webhooks, for example to dim the lights in Home
          Assistant, they&rsquo;re told your name and the title when you start and stop watching.
          A show&rsquo;s name, never which episode. Off unless you turn it on, and nobody else can
          turn it on for you.
        </span>
      </span>
    </label>
  );
}
