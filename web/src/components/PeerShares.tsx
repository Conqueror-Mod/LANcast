import { useState } from "react";
import type { SharedLibrary } from "../api/types";
import {
  usePeerShares,
  useSetPeerShare,
  useUnsetPeerShare,
  useIsAdmin,
} from "../api/hooks";
import { RATING_RUNGS } from "@/lib/ratings";

/*
 * Choosing what one paired server may see
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §1, §6, §7).
 *
 * §7 makes this part of the decision rather than a follow-up: phases 1 to 3 of
 * the federation plan were built and invisible, and repeating that would
 * produce a fourth invisible phase. Nothing here is reachable without it.
 *
 * Administrators only, because sharing is a decision about this server's
 * content rather than an exercise of anybody's personal consent — which is why
 * the presence switches on the same screen are *not* gated that way.
 */
export function PeerShares({ fingerprint }: { fingerprint: string }) {
  const isAdmin = useIsAdmin();
  const [open, setOpen] = useState(false);
  /*
   * Fetched whenever an administrator is looking, not only once the pane is
   * open, so the summary below is a live number rather than dead code.
   *
   * It was gated on `open` first, which meant the count could never render:
   * the only moment it would be shown was the only moment it was not fetched.
   * The query reads local rows, and "have I shared anything with this server"
   * is exactly the question somebody has while looking at this screen — worth
   * answering without a click.
   */
  const { data, isLoading } = usePeerShares(fingerprint, isAdmin);

  if (!isAdmin) return null;

  const libraries = data?.libraries ?? [];
  const sharedCount = libraries.filter((l) => l.shared).length;

  return (
    <div className="peer-shares">
      {/*
       * One label in both states, and it names the *pane* rather than an
       * action.
       *
       * It read "Hide what they can see" when open, which on a sharing screen
       * could plausibly be read as *revoke their access*. A control whose
       * label might describe the dangerous action is worth nobody's second
       * guess. The chevron carries open/closed, and aria-expanded carries it
       * for anything not looking at pixels.
       */}
      <button
        type="button"
        className="peer-shares__toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="peer-shares__chevron" aria-hidden="true">
          {open ? "▾" : "▸"}
        </span>
        Choose what they can see
        <span className="peer-shares__count">
          {!data
            ? ""
            : sharedCount === 0
              ? "nothing shared"
              : `${sharedCount} of ${libraries.length} shared`}
        </span>
      </button>

      {open && (
        <div className="peer-shares__body">
          {isLoading && <p className="peer-shares__note">Loading…</p>}
          {!isLoading && libraries.length === 0 && (
            <p className="peer-shares__note">This server has no libraries yet.</p>
          )}
          {libraries.map((library) => (
            <ShareRow
              key={library.id}
              fingerprint={fingerprint}
              library={library}
            />
          ))}
          {libraries.length > 0 && (
            <p className="peer-shares__note">
              Sharing a library lets everybody on that server browse and play
              what is in it. Nothing is shared until you say so, and unpairing
              takes it all back at once.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function ShareRow({
  fingerprint,
  library,
}: {
  fingerprint: string;
  library: SharedLibrary;
}) {
  const share = useSetPeerShare(fingerprint);
  const unshare = useUnsetPeerShare(fingerprint);
  const busy = share.isPending || unshare.isPending;

  const toggle = () => {
    if (library.shared) unshare.mutate(library.id);
    else share.mutate({ library: library.id, ceiling: "" });
  };

  /*
   * Two lines, with the limit indented under the library it belongs to.
   *
   * It was one row with the limit pushed right by `margin-left: auto`, which
   * on a maximised window put "Up to PG-13" more than a thousand pixels from
   * the checkbox it applied to, with three other libraries in between. Nothing
   * in jsdom could see that; it took looking at the thing.
   */
  return (
    <div className="peer-shares__row">
      <label className="peer-shares__lib">
        <input
          type="checkbox"
          checked={library.shared}
          disabled={busy}
          onChange={toggle}
        />
        <span className="peer-shares__lib-name">{library.name}</span>
      </label>

      {library.shared && (
        <div className="peer-shares__limit">
          {library.supports_ceiling ? (
            <>
              <label className="peer-shares__limit-label">
                Up to
                <select
          className="peer-shares__select"
                  value={library.ceiling}
                  disabled={busy}
                  onChange={(e) =>
                    share.mutate({
                      library: library.id,
                      ceiling: e.target.value,
                    })
                  }
                >
                  <option value="">no limit</option>
                  {RATING_RUNGS.map((rung) => (
                    <option key={rung} value={rung}>
                      {rung}
                    </option>
                  ))}
                </select>
              </label>
              {/*
               * What the limit costs, told to the host at the moment they
               * choose (ADR 0071 §6). An unrated item is blocked and then
               * vanishes from the friend's view with no explanation; the
               * mitigation is not to explain it to the friend, who should not
               * be told what they cannot see, but to tell the host. A number
               * the host sees beats a mystery the friend does not.
               */}
              {library.ceiling !== "" && library.unrated > 0 && (
                <p className="peer-shares__cost">
                  {library.unrated} of {library.total} are unrated and will not
                  be shown.
                </p>
              )}
            </>
          ) : (
            /*
             * Said rather than offered. ADR 0071 §6: an inert switch on a
             * sharing screen is worse than no switch, because it reads as a
             * limit that was applied.
             */
            <p className="peer-shares__cost">
              Nothing here carries an age rating, so a limit would do nothing.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
