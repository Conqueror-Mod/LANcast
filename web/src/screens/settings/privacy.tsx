import { useEffect, useState } from "react";
import {
  useCurrentUser,
  useSetSharing,
  useSetPlaybackWebhooks,
  useHistoryCount,
  useResetHistory,
  type HistoryScope,
} from "@/api/hooks";
import { PlaybackWebhooksToggle } from "@/components/WebhookSettings";
import { useSetPeerVisibility } from "@/api/hooks";
import { errorMessage } from "@/lib/errors";

/*
 * Whether other people on this server can see what you have watched (ADR 0035).
 *
 * In Account rather than in Users because it is *yours*: an administrator may
 * run the server, and a switch somebody else can flip on your behalf is not
 * consent. There is no admin-facing version of this control, and the server has
 * no route that would allow one.
 *
 * The wording says what is shared and what is not, because "share activity" on
 * its own invites people to assume the worst — or, worse, the best.
 */
/*
 * The account's own say over playback webhooks (ADR 0097). Read back from auth
 * status, as SharingToggle is, for the reason its comment gives.
 */
function WebhookConsent() {
  const set = useSetPlaybackWebhooks();
  const me = useCurrentUser();
  const stored = me?.webhook_playback ?? false;
  const [pending, setPending] = useState<boolean | null>(null);
  useEffect(() => {
    setPending(null);
  }, [stored]);
  return (
    <section className="settings__section">
      <span className="section-label">Webhooks</span>
      <PlaybackWebhooksToggle
        on={pending ?? stored}
        onChange={(on) => {
          setPending(on);
          set.mutate(on);
        }}
      />
      {set.isError && <p className="set-error">{errorMessage(set.error)}</p>}
    </section>
  );
}

function SharingToggle() {
  const set = useSetSharing();
  const me = useCurrentUser();
  /*
   * The stored value comes from auth status, which is the only route that
   * reports the caller's own setting — `/api/people` excludes the caller by
   * design, so the previous version of this looked for itself in a list that
   * structurally could not contain it, found nothing, and fell back to `false`
   * on every mount. The toggle read as off for somebody who had opted in.
   *
   * The local override is still here, but only to keep the checkbox responsive
   * between the click and the refetch. It is cleared as soon as the server's
   * answer changes, so the server stays the thing being displayed rather than
   * a guess that happens to agree with it.
   */
  const stored = me?.sharing ?? false;
  const [pending, setPending] = useState<boolean | null>(null);
  useEffect(() => {
    setPending(null);
  }, [stored]);
  const on = pending ?? stored;

  return (
    <section className="settings__section">
      <span className="section-label">Sharing</span>
      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={on}
          onChange={(e) => {
            setPending(e.target.checked);
            set.mutate(e.target.checked);
          }}
        />
        <span>
          <strong>Let others here see what I have watched</strong>
          <span className="set-toggle__desc">
            Shares the titles you have <em>finished</em>, and when — with the
            other people on this server, on the People page. It does not share
            your ratings or notes, which stay private always, or where you have
            got to in something. Off unless you turn it on, and turning it off
            hides what you already shared.
          </span>
        </span>
      </label>
      {set.isError && (
        <span className="set-error">{(set.error as Error).message}</span>
      )}
      <PeerVisibility />
    </section>
  );
}

/*
 * Appearing to other servers, which is a **third** disclosure category and not
 * a wider setting of the one above (ADR 0045 §1).
 *
 * Its own switch, deliberately. Somebody who agreed to publish the films they
 * have finished did not thereby agree to be watched in real time, and silently
 * widening an existing opt-in is the failure ADR 0035 exists to prevent. The
 * two are next to each other because they are both about you; they are not
 * nested because they are not the same promise.
 *
 * What this switch does is narrower than it sounds, and the copy has to carry
 * that: it makes you **listable**, not visible. Nothing is disclosed until
 * somebody on the other server grants you presence by name, and that grant is
 * theirs to make. But it is also load-bearing in the other direction — an
 * account that has not opted in cannot be named by anybody's grant, in either
 * direction, which the schema enforces rather than a handler. So while this is
 * off, the People screen's peers section has nothing to draw however many
 * servers are paired.
 *
 * No administrator version exists and none should: a switch somebody else can
 * flip is not consent.
 */
function PeerVisibility() {
  const me = useCurrentUser();
  const set = useSetPeerVisibility();
  const stored = me?.visible_to_peers ?? false;
  const [pending, setPending] = useState<boolean | null>(null);
  useEffect(() => {
    setPending(null);
  }, [stored]);
  const on = pending ?? stored;

  return (
    <>
      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={on}
          onChange={(e) => {
            const next = e.target.checked;
            setPending(next);
            /*
             * Put it back if the write fails. Without this a refused write
             * leaves the switch claiming a state the server does not hold,
             * and for a privacy control that is worse than an error: somebody
             * reads it and believes they opted in, or out, when they did not.
             */
            set.mutate(next, { onError: () => setPending(null) });
          }}
        />
        <span>
          <strong>Let paired servers list me</strong>
          <span className="set-toggle__desc">
            Puts your name in the list this server hands to servers it is
            paired with, so somebody there can choose to see when you are
            watching something. <strong>It shares nothing by itself</strong>
            {" — "}they still have to ask for you by name, and you have to
            grant them the same, on the People page. Off unless you turn it on.
          </span>
        </span>
      </label>
      {set.isError && (
        <span className="set-error">{(set.error as Error).message}</span>
      )}
    </>
  );
}

/*
 * Forgetting what you have watched.
 *
 * On the Account pane rather than under an administrator's settings, because
 * this is one person's record and only its owner may clear it — the endpoint
 * takes no user id for exactly that reason (ADR 0006). An administrator runs
 * the server; that is not consent on somebody else's behalf.
 *
 * Three choices rather than one button, because "clear my history" means three
 * different things and `playback_state` is one table carrying two meanings.
 * Somebody forgetting a show they finished rarely means "and lose my place in
 * the one I am half way through".
 *
 * The count is fetched before anything is offered, and the confirmation says
 * it. A number is what makes an irreversible action reviewable: a person who
 * expected to clear one thing and is told four hundred has learned something
 * while it is still free. "Are you sure" teaches nobody anything.
 */
function HistoryReset() {
  const [scope, setScope] = useState<HistoryScope>("all");
  const [confirming, setConfirming] = useState(false);
  const [done, setDone] = useState<number | null>(null);
  const count = useHistoryCount(scope);
  const reset = useResetHistory();

  const n = count.data?.count ?? 0;
  const busy = reset.isPending || count.isFetching;

  let sub: string;
  if (reset.isError) {
    sub = errorMessage(reset.error);
  } else if (done !== null) {
    sub = done === 0 ? "There was nothing to forget." : `Forgot ${done}.`;
  } else if (count.isError) {
    sub = "Could not read how much history you have.";
  } else if (count.isLoading) {
    sub = "Counting…";
  } else {
    // Named rather than counted alone: "412 things" invites a shrug where
    // "412 things you have watched" is the thing about to be destroyed.
    sub = `${n} ${n === 1 ? "record" : "records"} — this cannot be undone.`;
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Clear watch history</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <span className="set-confirm">
              Forgets {n} {n === 1 ? "record" : "records"}, permanently
              {/* The log of finished viewings goes with what was finished
                  (ADR 0074); forgetting unfinished positions leaves it. */}
              {scope === "unfinished" ? "." : ", and their entries in your watch history."}
            </span>
            <button
              className="set-btn"
              disabled={busy}
              onClick={() =>
                reset.mutate(
                  { scope },
                  {
                    onSuccess: () => {
                      setDone(n);
                      setConfirming(false);
                    },
                  },
                )
              }
            >
              Forget them
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <select
              className="set-input"
              value={scope}
              aria-label="What to forget"
              onChange={(e) => {
                setScope(e.target.value as HistoryScope);
                setDone(null);
              }}
            >
              <option value="all">Everything</option>
              <option value="finished">Only what I finished</option>
              <option value="unfinished">Only what I did not finish</option>
            </select>
            {/*
              Disabled at zero rather than hidden. A control that vanishes when
              there is nothing to do leaves somebody wondering where it went,
              and "nothing to forget" is a useful answer in itself.
            */}
            <button
              className="set-btn"
              disabled={busy || n === 0}
              onClick={() => {
                setDone(null);
                setConfirming(true);
              }}
            >
              Clear…
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export function PrivacySection() {
  return (
    <section className="settings__section">
      <span className="section-label">Privacy</span>
      <SharingToggle />
      <WebhookConsent />
      <HistoryReset />
    </section>
  );
}
