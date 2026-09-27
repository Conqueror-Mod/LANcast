import type { MouseEventHandler } from "react";
import { NavLink } from "react-router-dom";
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

  /*
   * Silent when there is nothing, including when that server is not
   * answering. An unreachable peer is the ordinary state of somebody else's
   * machine, and a rail that showed an error for it would put another
   * household's downtime in this one's furniture.
   */
  if (libraries.length === 0) return null;

  return (
    <>
      <span className="section-label app-shell__rail-label">{name}</span>
      {libraries.map((lib) => (
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
