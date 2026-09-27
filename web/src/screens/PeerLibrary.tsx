import { useState } from "react";
import { useParams, Link } from "react-router-dom";
import { usePeerLibraries, usePeerItems } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import "./PeerLibrary.css";

/*
 * Somebody else's library
 * ([ADR 0071](../../../docs/adr/0071-a-shared-library-is-a-standing-grant.md)
 * §5).
 *
 * Its own screen rather than the ordinary Library one with a flag, because the
 * two differ in what they can truthfully offer. There is no infinite scroll,
 * no facets and no sort yet — the far server browses one library at a time —
 * and a screen that shares its shell with the local one would have to hide
 * half its own controls and explain why.
 *
 * More importantly, §5 is a rule about not merging, and the surest way to keep
 * it is for the two never to share a code path that could be taught to
 * concatenate.
 */
export function PeerLibrary() {
  const { fingerprint = "", library = "" } = useParams();
  const libraryID = Number(library);
  const [query, setQuery] = useState("");

  const libs = usePeerLibraries(fingerprint);
  const items = usePeerItems(fingerprint, libraryID, query);

  const here = libs.data?.libraries.find((l) => l.id === libraryID);

  /*
   * A server that is not answering is said plainly, and said as a fact about
   * *them*. It is the ordinary state of another household's machine, not an
   * error in this one — so it reads as information rather than as a fault
   * somebody here should fix.
   */
  if (libs.isError || items.isError) {
    return (
      <div className="peer-lib">
        <PeerLibraryHeader name={here?.name} />
        <p className="peer-lib__away">
          That server is not answering. Their library is only here while their
          machine is on.
        </p>
      </div>
    );
  }

  const list = items.data?.items ?? [];

  return (
    <div className="peer-lib">
      <PeerLibraryHeader name={here?.name} />

      <div className="peer-lib__tools">
        <input
          className="peer-lib__search"
          type="search"
          placeholder="Search this library"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Search this library"
        />
        {items.data && (
          <span className="peer-lib__count">
            {items.data.total.toLocaleString()}
          </span>
        )}
      </div>

      {items.isLoading && <p className="peer-lib__note">Loading…</p>}

      {!items.isLoading && list.length === 0 && (
        <p className="peer-lib__note">
          {query === ""
            ? "Nothing here."
            : "Nothing here matches that."}
        </p>
      )}

      <div className="peer-lib__grid">
        {list.map((item) => (
          /*
           * onOpen is passed and does nothing, which is deliberate and is the
           * most important line on this screen.
           *
           * Left off, PosterTile navigates to `/item/{id}` — and an id from
           * another server names a **different item here**. Pressing a tile
           * would quietly open somebody else's film from our own library,
           * which is the merging §5 forbids arriving through the one door
           * nobody was watching.
           *
           * Playing from a peer needs the playback path to know it is
           * streaming through the proxy, which is the next piece of work. Until
           * then a tile that went nowhere is honest and a tile that went
           * somewhere wrong is not.
           */
          <PosterTile key={item.id} item={item} onOpen={() => {}} />
        ))}
      </div>
    </div>
  );
}

/*
 * The header names the server before the library, which is the whole of §5 in
 * one line: *"A person needs to know at a glance whose disk a film is on"* —
 * because it is gone when that machine is off, it counts against that host's
 * streaming cap, and deleting it is not theirs to do.
 */
function PeerLibraryHeader({ name }: { name?: string }) {
  const { fingerprint = "" } = useParams();
  return (
    <header className="peer-lib__head">
      <Link to="/people" className="peer-lib__server">
        Another server
      </Link>
      <h1 className="peer-lib__title">{name ?? "Shared library"}</h1>
      <p className="peer-lib__sub">
        On their machine, not yours. It is here while they are online, and what
        you can see is what they chose to share.
      </p>
      <span className="peer-lib__fp" title={fingerprint} aria-hidden="true" />
    </header>
  );
}
