import { useEffect, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useGlobalSearch, useLibraries } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import { PosterTile } from "@/components/PosterTile";
import { useItemActions } from "@/components/itemActions";
import type { Item } from "@/api/types";
import "./Browse.css";

/*
 * Search, across everything.
 *
 * Searching was per-library, which asks you to know which library a thing is in
 * before you can look for it — the opposite of what a search is for, and
 * particularly silly on a server whose whole job is that you do not have to
 * remember where anything lives.
 *
 * Grouped by library rather than merged into one grid. "Terminator" matching a
 * film and a soundtrack is two different answers, and a single sorted list
 * would interleave them by title and make the viewer sort out which is which.
 *
 * The query lives in the URL like every other control here, so a search is a
 * link, survives a reload, and comes back with Back.
 */
export function Search() {
  // The same menu a library grid offers; a poster is a poster.
  const { actions, dialogs } = useItemActions();
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const { data, isFetching } = useGlobalSearch(q);
  const input = useRef<HTMLInputElement>(null);

  useBackHandler(() => navigate(-1));

  // The cursor goes where the typing is meant to go. Landing on a search page
  // and having to click the box first is the kind of thing that makes people
  // stop using a search.
  useEffect(() => {
    input.current?.focus();
  }, []);

  const items = data?.items ?? [];
  const byLibrary = new Map<number, Item[]>();
  for (const item of items) {
    const list = byLibrary.get(item.library_id) ?? [];
    list.push(item);
    byLibrary.set(item.library_id, list);
  }
  const libraryName = (id: number) =>
    libraries?.find((l) => l.id === id)?.name ?? "Library";

  const searching = q.trim().length >= 2;

  return (
    <div className="browse">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Search</h1>
        <input
          ref={input}
          className="browse__search"
          type="search"
          placeholder="Search everything"
          value={q}
          aria-label="Search every library"
          onChange={(e) => {
            const next = new URLSearchParams(params);
            if (e.target.value) next.set("q", e.target.value);
            else next.delete("q");
            // Replace rather than push: typing eight characters should not put
            // eight entries in the history for Back to walk out through.
            setParams(next, { replace: true });
          }}
        />
      </div>

      {!searching && (
        <p className="browse__message">
          Type at least two characters. This looks in every library at once.
        </p>
      )}

      {searching && items.length === 0 && !isFetching && (
        <p className="browse__message">Nothing matches “{q}”.</p>
      )}

      {[...byLibrary.entries()].map(([libraryID, group]) => (
        <section className="browse__group" key={libraryID}>
          <span className="section-label">{libraryName(libraryID)}</span>
          <div className="browse__grid">
            {group.map((item) => (
              <PosterTile key={item.id} item={item} actions={actions} />
            ))}
          </div>
        </section>
      ))}
      {dialogs}
    </div>
  );
}
