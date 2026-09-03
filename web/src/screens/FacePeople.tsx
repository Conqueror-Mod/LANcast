import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useLibraries,
  useFacePeople,
  useFaceCapabilities,
  useClusterFaces,
  useClusterSuggestions,
  useNamePerson,
  useStartFacePass,
  useIsAdmin,
} from "@/api/hooks";
import { collapsePeople, type CollapsedPerson } from "@/lib/collapsePeople";
import { useFocusable } from "@/focus/FocusController";
import "./FacePeople.css";

/*
 * The people in a picture library (ADR 0052).
 *
 * The model and the worker are the large part of face grouping by effort; this
 * screen is the part that makes it worth having. A group of unnamed faces is a
 * curiosity. A named one is how somebody finds a photograph.
 *
 * So the naming is the design, not a field bolted onto a grid: the largest
 * unnamed group leads, naming one is two keystrokes and moves to the next, and
 * nothing here asks a question the person cannot answer by looking.
 */

function faceThumb(id: number): string {
  return `/api/faces/${id}/thumb`;
}

/*
 * One group.
 *
 * The count is shown because it is what makes a group worth naming: forty
 * photographs of one person is an afternoon of finding them later, and one is
 * probably a stranger in the background of a holiday.
 */
function PersonTile({
  person,
  onOpen,
}: {
  person: CollapsedPerson;
  onOpen: () => void;
}) {
  const focusable = useFocusable(onOpen);
  const named = person.name && person.name.length > 0;
  return (
    <button
      {...focusable}
      className={"faceperson" + (named ? " faceperson--named" : "")}
      onClick={onOpen}
      aria-label={
        named ? `${person.name}, ${person.count} photographs` : "Unnamed person"
      }
    >
      <span className="faceperson__face">
        {person.coverFaceID ? (
          <img src={faceThumb(person.coverFaceID)} alt="" loading="lazy" />
        ) : (
          <span className="faceperson__blank" />
        )}
      </span>
      <span className="faceperson__name">
        {named ? person.name : "Who is this?"}
      </span>
      <span className="faceperson__count">{person.count}</span>
    </button>
  );
}

/*
 * The naming panel.
 *
 * Several faces rather than one, because a single crop is a bad basis for a
 * decision — one bad angle and somebody names the group wrongly, and a wrong
 * name is worse than no name. Showing the group's clearest examples is what
 * makes the question answerable at a glance.
 */
function NamePanel({
  libraryID,
  person,
  onClose,
}: {
  libraryID: number;
  person: CollapsedPerson;
  onClose: () => void;
}) {
  /*
   * The largest group represents the person here.
   *
   * Its faces are the clearest examples, and it is the one a re-cluster draws
   * others toward, so it is the right one to show and the right one to ask
   * suggestions about. Renaming still reaches every group — see submit.
   */
  const primary = person.clusterIDs[0];
  const { data } = useClusterFaces(primary);
  const name = useNamePerson(libraryID);
  const [value, setValue] = useState(person.name ?? "");

  /*
   * After naming, offer the near-misses.
   *
   * The panel does not close on save any more, and that is the change. 126
   * faces on a real library grouped with nothing at all — not false
   * detections, just harder ones that fell short of the similarity that
   * decides two faces are one person. Clustering cannot reach them by
   * relaxing that, because erring low puts somebody's face under somebody
   * else's name.
   *
   * A person can answer what the threshold cannot, and the moment they can
   * answer it is the moment they have just said who this is. Asking before a
   * name exists would be a row of strangers beside an empty field.
   */
  const [named, setNamed] = useState("");
  const suggestions = useClusterSuggestions(primary, named !== "");
  /*
   * Which suggestions have been accepted, held here as well as refetched.
   *
   * The refetch is the truth and arrives in a moment; this is what makes the
   * click feel like it did something *now*. Without it there is a gap where a
   * pressed face is unchanged, which is the gap that made the whole row read
   * as unclickable.
   */
  const [taken, setTaken] = useState<Set<number>>(new Set());

  const submit = () => {
    const given = value.trim();
    if (!given) {
      onClose();
      return;
    }
    /*
     * Every group under this person, not just the one on screen.
     *
     * Renaming one of four groups called Georgia Bowles would split her back
     * into two people — the exact fault collapsing exists to fix, arriving by
     * a different route.
     */
    for (const id of person.clusterIDs) {
      name.mutate(
        { id, name: given },
        id === primary ? { onSuccess: () => setNamed(given) } : undefined,
      );
    }
  };

  return (
    <div className="facenamer" role="dialog" aria-label="Name this person">
      <div className="facenamer__faces">
        {(data?.faces ?? []).slice(0, 12).map((f) => (
          <img key={f.id} src={faceThumb(f.id)} alt="" loading="lazy" />
        ))}
      </div>

      <label className="facenamer__field">
        <span>Name</span>
        <input
          autoFocus
          value={value}
          placeholder="Who is this?"
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
            if (e.key === "Escape") onClose();
          }}
        />
      </label>

      <p className="facenamer__note">
        {/*
          Said plainly, because it is the promise that makes naming safe to do.
          Somebody who believes a re-run might undo their work will not do the
          work.
        */}
        A name you give stays. Re-grouping can add faces to this person, and
        will never rename or dissolve them.
      </p>

      {/*
        * Shown only after a name has been given, and never merging anything on
        * its own. Each is a question with a face attached, and the answer is a
        * click that names it the same thing.
        */}
      {named !== "" && (suggestions.data?.people?.length ?? 0) > 0 && (
        <div className="facenamer__also">
          <p className="facenamer__note">
            Is <strong>{named}</strong> also one of these? They were close, but
            not close enough for LANcast to say so on its own.
          </p>
          <div className="facenamer__suggestions">
            {suggestions.data?.people?.map((p) => (
              <button
                key={p.id}
                className="facenamer__suggestion"
                data-taken={taken.has(p.id) || undefined}
                onClick={() => {
                  // Marked before the request answers. The refetch confirms it
                  // and removes the tile; this is what makes the press land.
                  setTaken((s) => new Set(s).add(p.id));
                  name.mutate({ id: p.id, name: named });
                }}
                disabled={taken.has(p.id)}
                title={taken.has(p.id) ? `Named ${named}` : `Also ${named}`}
              >
                {p.cover_face_id != null && (
                  <img src={faceThumb(p.cover_face_id)} alt="" loading="lazy" />
                )}
                <span>{taken.has(p.id) ? "✓" : p.count}</span>
              </button>
            ))}
          </div>
        </div>
      )}

      {named !== "" && (suggestions.data?.people?.length ?? 0) === 0 &&
        !suggestions.isLoading && (
          <p className="facenamer__note">
            Saved. Nothing else looked close enough to ask about.
          </p>
        )}

      <div className="facenamer__actions">
        {/*
          * Save becomes Done once the name is given.
          *
          * The panel used to close on save, and now it stays open to ask about
          * near-misses — so it has to say how to leave. A Save button that has
          * already saved is a button that appears to do nothing.
          */}
        <button
          className="facenamer__save"
          onClick={named !== "" ? onClose : submit}
          disabled={name.isPending}
        >
          {name.isPending ? "Saving…" : named !== "" ? "Done" : "Save"}
        </button>
        {person.name ? (
          // Clearing is offered next to saving rather than hidden, because a
          // typo that cannot be undone is what makes people afraid to type.
          <button
            className="facenamer__clear"
            onClick={() => {
              // Every group, for the reason renaming touches every group:
              // clearing one of four would leave the other three named, which
              // is a person who half exists.
              person.clusterIDs.forEach((id, i) =>
                name.mutate(
                  { id, name: "" },
                  i === 0 ? { onSuccess: onClose } : undefined,
                ),
              );
            }}
          >
            Clear name
          </button>
        ) : null}
        <button className="facenamer__cancel" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  );
}

export function FacePeople() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const isAdmin = useIsAdmin();

  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data: caps } = useFaceCapabilities();
  const { data, isLoading } = useFacePeople(libraryID);
  const startPass = useStartFacePass(libraryID);
  const [naming, setNaming] = useState<CollapsedPerson | null>(null);
  const [showSingles, setShowSingles] = useState(false);

  const people = data?.people ?? [];
  const pending = data?.pending ?? 0;

  /*
   * Unnamed groups first, largest first within that.
   *
   * The screen's job is to get faces named, and the most valuable thing on it
   * is always the biggest group nobody has identified yet. Sorting named people
   * to the front would bury the work under the finished part of it.
   */
  const ordered = [...people].sort((a, b) => {
    const an = a.name ? 1 : 0;
    const bn = b.name ? 1 : 0;
    if (an !== bn) return an - bn;
    return b.count - a.count;
  });

  /*
   * A group of one is held back until asked for.
   *
   * Measured on a real library: 4,620 faces in 343 groups, of which 126 hold a
   * single face — **37% of the groups and 2.7% of the faces**. Every tile is
   * the same size, so that minority took up as much of the page as the groups
   * of 301, 222 and 212 that are the reason to be here at all, and the page
   * read as mostly noise.
   *
   * Held back rather than hidden. A face that grouped with nothing is a real
   * face of a real person — measured at the same detection confidence as every
   * other, just smaller on average — and it is nameable. It is simply the last
   * thing worth looking at, not the first.
   *
   * Not thrown away either: the obvious "fix" is to stop keeping faces below
   * some size, and the numbers refuse it. Cutting at 100px would drop 32% of
   * the singletons and 22% of the faces that group perfectly well, which is a
   * bad trade made on a correlation that is real but far too weak to act on.
   */
  /*
   * Collapsed before anything else looks at the list.
   *
   * Naming does not merge groups — a re-cluster seeds named ones as anchors
   * and never dissolves them — so accepting three suggestions leaves four
   * groups called Georgia Bowles. Reported as her appearing three times, at
   * 80, 1 and 1, which reads as three people who share a name.
   */
  const collapsed = collapsePeople(ordered);
  const grouped = collapsed.filter((p) => p.count > 1 || p.name);
  const singles = collapsed.filter((p) => p.count <= 1 && !p.name);
  const shown = showSingles ? [...grouped, ...singles] : grouped;

  return (
    <div className="faces-page">
      <div className="faces-page__bar">
        <button className="faces-page__back" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1 className="faces-page__title">
          {library?.name ?? "Pictures"} <span>people</span>
        </h1>
        {isAdmin && caps?.ready && (
          <button
            className="faces-page__scan"
            onClick={() => startPass.mutate()}
            disabled={startPass.isPending}
          >
            {startPass.isPending ? "Starting…" : "Find faces"}
          </button>
        )}
      </div>

      {/*
        The two empty states are kept apart, which is the whole reason the API
        returns `pending` and `ready` at all. "Nobody is in your photographs"
        and "nothing has looked at them" are the same empty grid and completely
        different sentences.
      */}
      {caps && !caps.ready && (
        <p className="faces-page__note">
          Face grouping is not set up on this server.
          {caps.reason ? ` ${caps.reason}.` : ""}
        </p>
      )}

      {caps?.ready && !isLoading && people.length === 0 && pending === 0 && (
        <p className="faces-page__note">
          No faces yet. Press <strong>Find faces</strong> to look through this
          library — it takes a while, and you can leave this page while it runs.
        </p>
      )}

      {/*
        * Says when groups appear, because it used to say the wrong thing.
        *
        * "Groups appear as they are found" is not what happens: grouping runs
        * once, after every photograph has been examined, and deliberately —
        * a face is compared against everything already known, so grouping per
        * batch would redo the work and let a half-finished library form groups
        * the rest of it then has to be fitted around.
        *
        * The old sentence made a running pass look like a stalled one. With
        * 2,810 faces found and no groups yet, the only reading available was
        * that it had stopped, so it was pressed again — reported exactly that
        * way. The work was fine; the sentence was wrong.
        */}
      {pending > 0 && (
        <p className="faces-page__note">
          Still looking — {pending.toLocaleString()} photograph
          {pending === 1 ? "" : "s"} to go. People appear once every photograph
          has been looked at, so this page stays empty until it finishes. You
          can leave it running.
        </p>
      )}

      <div className="faces-page__grid">
        {shown.map((p) => (
          <PersonTile
            key={p.key}
            person={p}
            onOpen={() => isAdmin && setNaming(p)}
          />
        ))}
      </div>

      {/*
        * The toggle sits under the grid, not above it.
        *
        * Above, it is a control to get past before reaching what you came for.
        * Below, it is what it actually is: the end of the list, and an offer to
        * see the remainder. The count is named rather than hidden behind the
        * word "more", because 126 and 6 are different decisions.
        */}
      {singles.length > 0 && (
        <button
          className="faces-page__singles"
          onClick={() => setShowSingles((v) => !v)}
        >
          {showSingles
            ? `Hide ${singles.length.toLocaleString()} face${singles.length === 1 ? "" : "s"} that matched nobody else`
            : `Show ${singles.length.toLocaleString()} face${singles.length === 1 ? "" : "s"} that matched nobody else`}
        </button>
      )}

      {naming && (
        <div className="faces-page__scrim" onClick={() => setNaming(null)}>
          <div onClick={(e) => e.stopPropagation()}>
            <NamePanel
              libraryID={libraryID}
              person={naming}
              onClose={() => setNaming(null)}
            />
          </div>
        </div>
      )}
    </div>
  );
}
