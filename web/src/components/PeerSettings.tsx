import { useState } from "react";
import {
  useAddPeer,
  usePeerInvite,
  usePeers,
  useRemovePeer,
} from "@/api/hooks";
import { errorMessage } from "@/lib/errors";
import type { Peer } from "@/api/types";

/*
 * Introducing this server to another one (ADR 0044).
 *
 * Every route below has existed since August and nothing called any of them,
 * so two servers could not be introduced at all. That also made the People
 * screen's peers section — built, correct, and shipped with ADR 0045 —
 * permanently empty, because there was no peer for it to ask about. This pane
 * is the missing end of that chain.
 *
 * **Introduction is out-of-band and mutual**, which decides the shape of this
 * screen more than anything else. There is no directory to search and no
 * server to look somebody up in, by design: you hand somebody your invite
 * through a channel you already trust, and they hand you theirs. So the pane
 * has two halves that are deliberately symmetrical, and neither is a "connect"
 * button — nothing here reaches out to find anyone.
 *
 * Admin-gated because adding a peer opens a network relationship for the whole
 * server: the same class of operational power as adding a library, and not a
 * per-account preference (ADR 0015). Choosing to *appear* to a peer is the
 * opposite — that lives in Account, is self-service, and has no administrator
 * version, because a switch somebody else can flip is not consent.
 */
export function PeerSettings() {
  const { data, isLoading } = usePeers();
  const peers = data?.peers ?? [];

  return (
    <section className="settings__section">
      <span className="section-label">Other servers</span>
      <p className="set-row__sub set-row__sub--standalone">
        Servers this one has been introduced to. A pairing records that two
        servers know who each other are and{" "}
        <strong>grants nothing on its own</strong> — what anybody may see is
        decided separately, per person.
      </p>

      <MyInvite />
      <AddPeer />

      <span className="section-label">Paired</span>
      {isLoading ? (
        <p className="set-row__sub set-row__sub--standalone">…</p>
      ) : peers.length === 0 ? (
        <p className="set-row__sub set-row__sub--standalone">
          None yet. Send somebody your invite, and paste theirs above.
        </p>
      ) : (
        peers.map((p) => <PeerRow key={p.fingerprint} peer={p} />)
      )}
    </section>
  );
}

/*
 * This server's invite, fetched only when asked for.
 *
 * `GET /api/peers/invite` answers 409 on a server no other machine can reach,
 * and that is a real answer rather than a fault: a loopback-only server cannot
 * introduce itself, and saying so at the moment somebody asks for an invite is
 * more use than an error on a pane they opened for something else.
 */
function MyInvite() {
  const [asked, setAsked] = useState(false);
  const { data, isLoading, error } = usePeerInvite(asked);
  const [copied, setCopied] = useState(false);

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Your invite</div>
        {!asked ? (
          <div className="set-row__sub">
            Hand this to somebody you want to pair with, through a channel you
            already trust. It carries this server&rsquo;s identity and the
            addresses it can be reached on — nothing private, and no access.
          </div>
        ) : isLoading ? (
          <div className="set-row__sub">…</div>
        ) : error ? (
          <div className="set-error">{errorMessage(error)}</div>
        ) : data ? (
          <>
            <div className="set-row__sub set-row__sub--mono">{data.invite}</div>
            <div className="set-row__sub">
              {data.name} · {data.fingerprint_display}
            </div>
          </>
        ) : null}
      </div>
      <div className="set-row__actions">
        {!asked ? (
          <button className="set-btn" onClick={() => setAsked(true)}>
            Show invite
          </button>
        ) : (
          <button
            className="set-btn"
            disabled={!data}
            onClick={() => {
              if (!data) return;
              // Best effort: a clipboard that refuses is not worth an error
              // beside a value already on screen to be read or selected.
              void navigator.clipboard?.writeText(data.invite).then(
                () => setCopied(true),
                () => setCopied(false),
              );
            }}
          >
            {copied ? "Copied" : "Copy"}
          </button>
        )}
      </div>
    </div>
  );
}

/*
 * The other half: somebody else's invite, pasted in.
 *
 * The server refuses this server's own invite with `self` and a damaged one
 * with `bad_invite`, both carrying a message written for the person holding
 * the paste. So the error is shown as given rather than replaced with a
 * generic one — it is the only feedback somebody has about a long opaque
 * string they did not compose.
 */
function AddPeer() {
  const [invite, setInvite] = useState("");
  const add = useAddPeer();

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Add a server</div>
        <div className="set-row__sub">
          Paste the invite somebody sent you. Nothing is shared by pairing;
          you both decide separately what the other may see.
        </div>
        {/*
          Full width, because an invite is about three hundred characters and
          a default-width box shows twenty-five of them. Somebody pasting one
          cannot tell a complete paste from a truncated one, which is the only
          check available to them before pressing Add.
        */}
        <input
          className="set-input set-input--wide"
          placeholder="Paste an invite"
          aria-label="Paste an invite"
          value={invite}
          spellCheck={false}
          onChange={(e) => setInvite(e.target.value)}
        />
        {add.isError && (
          <div className="set-error">{errorMessage(add.error)}</div>
        )}
      </div>
      <div className="set-row__actions">
        <button
          className="set-btn"
          disabled={!invite.trim() || add.isPending}
          onClick={() =>
            add.mutate(invite.trim(), { onSuccess: () => setInvite("") })
          }
        >
          {add.isPending ? "Adding…" : "Add"}
        </button>
      </div>
    </div>
  );
}

function PeerRow({ peer }: { peer: Peer }) {
  const remove = useRemovePeer();
  const [confirming, setConfirming] = useState(false);

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{peer.name}</div>
        <div className="set-row__sub set-row__sub--mono">
          {peer.fingerprint_display}
        </div>
        <div className="set-row__sub">{describe(peer)}</div>
        {remove.isError && (
          <div className="set-error">{errorMessage(remove.error)}</div>
        )}
      </div>
      <div className="set-row__actions">
        {confirming ? (
          <>
            <button
              className="set-btn set-btn--danger"
              disabled={remove.isPending}
              onClick={() => remove.mutate(peer.fingerprint)}
            >
              {remove.isPending ? "Removing…" : "Remove for good"}
            </button>
            <button className="set-btn" onClick={() => setConfirming(false)}>
              Cancel
            </button>
          </>
        ) : (
          <button
            className="set-btn set-btn--danger"
            onClick={() => setConfirming(true)}
          >
            Unpair
          </button>
        )}
      </div>
    </div>
  );
}

/*
 * What a peer's state actually means, in words.
 *
 * `added` is not `paired`, and rendering it as "connected" would claim the
 * other person agreed to something they have not done yet: only the transport
 * can move a peer to `paired`, by reaching the far side and finding it holds
 * us too. Saying so is the difference between a screen that reports and one
 * that reassures.
 *
 * `last_seen` of 0 is likewise a different statement from a time in the past —
 * never answered, rather than answered and since gone quiet.
 */
function describe(peer: Peer): string {
  if (peer.state !== "paired") {
    return "Added, not yet mutual — they need to add your invite too.";
  }
  if (!peer.last_seen) {
    return "Paired. Has not answered yet.";
  }
  return `Paired. Last answered ${ago(peer.last_seen)}.`;
}

function ago(unixSeconds: number): string {
  const seconds = Math.max(0, Math.floor(Date.now() / 1000 - unixSeconds));
  if (seconds < 90) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 90) return `${minutes} minutes ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 36) return `${hours} hours ago`;
  return `${Math.floor(hours / 24)} days ago`;
}
