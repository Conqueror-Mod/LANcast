import { useState, type MouseEventHandler } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { usePeers, usePeerLibraries } from "@/api/hooks";
import { LibraryIcon } from "@/components/LibraryIcon";

/*
 * Other servers' libraries in the rail
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §5).
 *
 * Under their own heading, below ours, and **never mixed in**. §5 is explicit
 * that this is not only presentation: a person needs to know at a glance whose
 * disk a film is on, because everything about it differs — it is gone when
 * that server is off, it counts against that host's streaming cap, and
 * deleting it is not theirs to do. A merged list makes all of that invisible
 * at exactly the moment it matters.
 *
 * Nothing renders until a peer actually answers with something shared. A
 * heading over an empty space would be a promise about a machine that may
 * simply be switched off.
 */
export function PeerLibraryLinks({
  onNavigate,
}: {
  onNavigate: MouseEventHandler<HTMLElement>;
}) {
  const { data } = usePeers();
  const peers = data?.peers ?? [];
  if (peers.length === 0) return null;

  return (
    <>
      {peers.map((peer) => (
        <PeerSection
          key={peer.fingerprint}
          fingerprint={peer.fingerprint}
          name={peer.name}
          onNavigate={onNavigate}
        />
      ))}
    </>
  );
}

function PeerSection({
  fingerprint,
  name,
  onNavigate,
}: {
  fingerprint: string;
  name: string;
  onNavigate: MouseEventHandler<HTMLElement>;
}) {
  const { data } = usePeerLibraries(fingerprint);
  const libraries = data?.libraries ?? [];
  const location = useLocation();
  const [open, setOpen] = useState(() => readOpen(fingerprint));
  /*
   * Where you are is never folded away. Browsing one of this server's
   * libraries keeps its list open whatever was last chosen, so the gold edge
   * that says "you are here" always has a row to sit on.
   */
  const here = location.pathname.startsWith(
    `/peers/${encodeURIComponent(fingerprint)}/`,
  );
  const shown = open || here;

  /*
   * Silent when there is nothing, including when that server is not
   * answering. An unreachable peer is the ordinary state of somebody else's
   * machine, and a rail that showed an error for it would put another
   * household's downtime in this one's furniture.
   */
  if (libraries.length === 0) return null;

  /*
   * A server is a heading that folds, not a list that is always spelled out.
   *
   * Every paired server used to add its whole library list to the rail. With
   * one friend that is four more rows; with four it is the rail. Reported as
   * "it would not take much to overpopulate the navbar". The heading is a
   * button, the choice is remembered on this device, and a new server starts
   * folded: the rail is for your own places first.
   */
  return (
    <>
      <button
        type="button"
        className="section-label app-shell__rail-label app-shell__server"
        aria-expanded={shown}
        title={shown ? `Hide ${name}'s libraries` : `Show ${name}'s libraries`}
        onClick={() => {
          const next = !shown;
          setOpen(next);
          writeOpen(fingerprint, next);
        }}
      >
        <span className="app-shell__server-name">{name}</span>
        <svg
          className={"app-shell__chevron" + (shown ? " is-open" : "")}
          viewBox="0 0 16 16"
          aria-hidden="true"
        >
          <path
            d="M4 6l4 4 4-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>
      {shown && libraries.map((lib) => (
        <NavLink
          key={lib.id}
          to={`/peers/${encodeURIComponent(fingerprint)}/library/${lib.id}`}
          title={`${lib.name} — on ${name}`}
          onClick={onNavigate}
          className={({ isActive }) =>
            "app-shell__lib" + (isActive ? " is-active" : "")
          }
        >
          <LibraryIcon kind={lib.kind} />
          <span className="app-shell__lib-name app-shell__label">
            {lib.name}
          </span>
          {/*
            No count. Ours carry one; theirs deliberately do not — a number
            here would sit in the same column as our own totals and read as
            part of the same library, which is the merging §5 forbids. It is
            also a number we would have to keep fetching from a machine that
            may be off.
          */}
        </NavLink>
      ))}
    </>
  );
}

/*
 * Whether a server's list is open, per device. Browser storage is right for
 * this and nothing more: it is how one person likes their rail on one screen,
 * and it can come back empty -- a private window, cleared site data -- in which
 * case the server is simply folded.
 */
const openKey = (fingerprint: string) => `lancast.rail.server.${fingerprint}`;

function readOpen(fingerprint: string): boolean {
  try {
    return localStorage.getItem(openKey(fingerprint)) === "open";
  } catch {
    return false;
  }
}

function writeOpen(fingerprint: string, open: boolean) {
  try {
    if (open) localStorage.setItem(openKey(fingerprint), "open");
    else localStorage.removeItem(openKey(fingerprint));
  } catch {
    // Not remembered; the rail still works.
  }
}
