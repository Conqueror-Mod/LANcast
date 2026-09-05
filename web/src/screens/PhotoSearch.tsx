import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useLibraries,
  useSemanticCapabilities,
  usePhotoSearch,
  useSemanticIndexStatus,
  useStartSemanticPass,
  useIsAdmin,
} from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { PhotoViewer } from "@/components/PhotoViewer";
import type { Item } from "@/api/types";
import "./PhotoSearch.css";

/*
 * Finding a photograph by describing it (ADR 0060).
 *
 * THREE EMPTY STATES, NOT ONE
 *
 * An empty grid here can mean three completely different things: the feature
 * is not installed, it is installed but this library has never been indexed,
 * or it looked and nothing matched. They have three different fixes — download
 * something, press a button, type something else — and a single "no results"
 * sends somebody to the wrong one every time.
 *
 * That is why the API returns `indexed` alongside the hits and `semantic_ready`
 * from its own route, and it is the whole reason this screen is worth writing
 * carefully. The people page has the same three states for the same reason, and
 * got them wrong first.
 *
 * SEARCH ON SUBMIT, NOT AS YOU TYPE
 *
 * Every query is a process start and a model load on the server. Searching per
 * keystroke would spend six of those to answer one question, and the answer to
 * a half-typed query is misleading rather than merely wasteful — "a dog on a b"
 * ranks photographs confidently and wrongly, which reads as the feature being
 * bad rather than the query being unfinished.
 */
export function PhotoSearch() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const isAdmin = useIsAdmin();

  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data: caps, isError: capsFailed } = useSemanticCapabilities();
  const startPass = useStartSemanticPass(libraryID);
  const { data: status } = useSemanticIndexStatus(libraryID);

  // The typed text and the submitted query are separate: the first changes on
  // every keystroke and the second is what was actually asked, which is what
  // the results on screen are an answer to.
  const [typed, setTyped] = useState("");
  const [query, setQuery] = useState("");
  const { data, isLoading, isError } = usePhotoSearch(libraryID, query);
  const [shown, setShown] = useState<{ photos: Item[]; at: number } | null>(
    null,
  );

  const hits = data?.hits ?? [];
  const photos = hits.map((h) => h.item);
  const ready = caps?.semantic_ready ?? false;

  return (
    <div className="photosearch">
      <div className="photosearch__bar">
        <button className="photosearch__back" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1 className="photosearch__title">
          {library?.name ?? "Photos"} <span>by description</span>
        </h1>
        {/*
          Offered only to an administrator and only when the models are here.
          A button that always fails is worse than no button: it teaches people
          the feature is broken rather than that it is not set up.
        */}
        {isAdmin && ready && (
          <button
            className="photosearch__index"
            onClick={() => startPass.mutate()}
            disabled={startPass.isPending}
          >
            {startPass.isPending ? "Starting…" : "Index photographs"}
          </button>
        )}
      </div>

      <form
        className="photosearch__form"
        onSubmit={(e) => {
          e.preventDefault();
          setQuery(typed.trim());
        }}
      >
        <input
          className="photosearch__input"
          type="search"
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder="a dog on a beach"
          aria-label="Describe the photograph"
          disabled={!ready}
        />
        <button
          className="photosearch__go"
          type="submit"
          disabled={!ready || typed.trim().length === 0}
        >
          Search
        </button>
      </form>

      {/*
        A server that could not be asked at all.
        
        Written after watching the page against a server too old to have the
        route: the request 404s, `caps` stays undefined, and every branch below
        is guarded on having an answer — so the page rendered a disabled field
        and no explanation whatsoever, which is the one thing on this screen
        that genuinely does read as broken. Not-ready and not-asked are
        different sentences, and this is the second of them.
      */}
      {capsFailed && (
        <p className="photosearch__note">
          This server could not be asked whether it can search photographs by
          description. It may be older than the feature — check that the server
          and this client are the same version.
        </p>
      )}

      {/* State one: nothing installed. Carries the server's own reason, because
          "not installed" and "no model" are different problems. */}
      {caps && !ready && (
        <p className="photosearch__note">
          Searching photographs by description is not set up on this server.
          {caps.semantic_reason ? ` ${caps.semantic_reason}.` : ""}{" "}
          {isAdmin
            ? "It is an optional download — see Pictures in Settings."
            : "An administrator can enable it in Settings."}
        </p>
      )}

      {/*
        A pass in progress, said on the page rather than only in the activity
        bar. The count climbs while it runs, which is the difference between
        "this is working" and "this is stuck".
      */}
      {status?.running && (
        <p className="photosearch__note">
          Indexing — {status.indexed.toLocaleString()} done
          {status.pending > 0
            ? `, ${status.pending.toLocaleString()} to go`
            : ""}
          . You can search what is already indexed, and you can leave this page.
        </p>
      )}

      {/*
        State two: installed, but this library has no vectors.

        Read from the status route, not from a search result — which is the
        change. Derived from the search it only appeared *after* somebody typed
        something, so the screen opened on an unindexed library showing a field
        and no explanation at all: the one page written to keep empty states
        apart, opening in a state it never described. Watching it do that is the
        only way it was ever going to be found.
      */}
      {ready && status && !status.running && status.indexed === 0 && (
        <p className="photosearch__note">
          Nothing in this library has been indexed yet.{" "}
          {isAdmin ? (
            <>
              Press <strong>Index photographs</strong> to look through it — it
              takes a while, and you can leave this page while it runs.
            </>
          ) : (
            "An administrator can index it."
          )}
        </p>
      )}

      {isLoading && query.length > 0 && (
        <p className="photosearch__note">Looking…</p>
      )}

      {isError && (
        <p className="photosearch__note photosearch__note--warn">
          The search could not be run. The models may be part-installed — see
          Pictures in Settings.
        </p>
      )}

      {/* State three, and the only one that is really "no results". */}
      {ready && data && data.indexed > 0 && hits.length === 0 && (
        <p className="photosearch__note">
          Nothing matched “{query}”. {data.indexed.toLocaleString()} photograph
          {data.indexed === 1 ? " was" : "s were"} searched — try describing
          what is in the picture rather than naming it.
        </p>
      )}

      {hits.length > 0 && (
        <>
          <p className="photosearch__count">
            {hits.length} of {data?.indexed.toLocaleString()} photographs, best
            first
          </p>
          <div className="photosearch__grid">
            {hits.map((hit, i) => (
              // The whole result set goes to the viewer, not one picture:
              // arrowing through the answers is the thing a result list is
              // for, and passing a single item makes every result a dead end.
              <PosterTile
                key={hit.item.id}
                item={hit.item}
                onOpen={() => setShown({ photos, at: i })}
              />
            ))}
          </div>
        </>
      )}

      {shown && (
        <PhotoViewer
          photos={shown.photos}
          startAt={shown.at}
          onClose={() => setShown(null)}
        />
      )}
    </div>
  );
}
