import { useState } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { ApiFailure } from "@/api/client";
import { usePeerLibraries, usePeerItems } from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { peerArtworkURL } from "@/playback/peerSource";
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
  const navigate = useNavigate();
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
  /*
   * Two different failures, and they were one sentence.
   *
   * "Not answering" is a fact about their *machine*. A withdrawn share is a
   * *decision* — their server answered perfectly well and said no. Telling
   * somebody a computer is switched off when its owner has just stopped sharing
   * sends them to ask whether it is on, and the answer is yes.
   *
   * This is the rule the People card already had to learn twice: never render a
   * choice as an absence, and never render an absence as a choice.
   */
  if (libs.isError || items.isError) {
    const err = (items.error ?? libs.error) as ApiFailure | undefined;
    const refused = err?.code === "peer_refused";
    return (
      <div className="peer-lib">
        <PeerLibraryHeader name={here?.name} />
        <p className="peer-lib__away">
          {refused
            ? "This is not shared with you any more. Their server answered; it is what they chose."
            : "That server is not answering. Their library is only here while their machine is on."}
        </p>
      </div>
    );
  }

  const pages = items.data?.pages ?? [];
  const list = pages.flatMap((p) => p.items);
  const total = pages[0]?.total ?? 0;

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
          /*
           * How many are *here*, beside how many there are.
           *
           * This printed the total alone while the screen rendered the first
           * page, so a library of 1,395 films showed sixty tiles ending in the
           * A's under the number 1,395. The count did not merely fail to
           * explain the truncation — it contradicted it, which is worse than
           * saying nothing.
           */
          <span className="peer-lib__count">
            {list.length < total
              ? `${list.length.toLocaleString()} of ${total.toLocaleString()}`
              : total.toLocaleString()}
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
           * onOpen is passed, and it is still the most important line on this
           * screen — it is now what *stops* the default rather than what does
           * nothing.
           *
           * Left off, PosterTile navigates to `/item/{id}`, and an id from
           * another server names a **different item here**. Pressing a tile
           * would quietly open somebody else's film from our own library, with
           * nothing to fail: the id it lands on is real.
           *
           * So the route carries the server as well as the item, and the title
           * rides along because the far server browses a library at a time and
           * has no route for one item — the alternative is fetching a whole
           * page to find a heading.
           */
          <PosterTile
            key={item.id}
            item={item}
            /*
             * Their image, from their server. The hash names bytes on their
             * disk, and `/api/artwork` is ours — which is why this library
             * rendered as placeholders until now.
             */
            posterURL={(it) =>
              peerArtworkURL(fingerprint, it.id, it.artwork?.poster)
            }
            onOpen={() =>
              navigate(
                `/peers/${encodeURIComponent(fingerprint)}/item/${item.id}`,
                { state: { title: item.title } },
              )
            }
          />
        ))}
      </div>

      {/*
        Asked for rather than scrolled into.
        
        An observer that fetched on scroll would spend somebody else's
        connection on a guess about what this person is about to do. A button
        is a person saying they want more of another household's library, which
        is the honest unit for a request that costs them.
      */}
      {items.hasNextPage && (
        <div className="peer-lib__more">
          <button
            type="button"
            className="peer-lib__more-button"
            disabled={items.isFetchingNextPage}
            onClick={() => void items.fetchNextPage()}
          >
            {items.isFetchingNextPage ? "Loading…" : "Show more"}
          </button>
        </div>
      )}
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
