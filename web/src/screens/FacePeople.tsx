import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useLibraries,
  useFacePeople,
  useFaceCapabilities,
  useClusterFaces,
  useNamePerson,
  useStartFacePass,
  useIsAdmin,
  type FacePerson,
} from "@/api/hooks";
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
  person: FacePerson;
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
        {person.cover_face_id ? (
          <img src={faceThumb(person.cover_face_id)} alt="" loading="lazy" />
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
  person: FacePerson;
  onClose: () => void;
}) {
  const { data } = useClusterFaces(person.id);
  const name = useNamePerson(libraryID);
  const [value, setValue] = useState(person.name ?? "");

  const submit = () => {
    name.mutate(
      { id: person.id, name: value.trim() },
      { onSuccess: onClose },
    );
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

      <div className="facenamer__actions">
        <button className="facenamer__save" onClick={submit} disabled={name.isPending}>
          {name.isPending ? "Saving…" : "Save"}
        </button>
        {person.name ? (
          // Clearing is offered next to saving rather than hidden, because a
          // typo that cannot be undone is what makes people afraid to type.
          <button
            className="facenamer__clear"
            onClick={() =>
              name.mutate({ id: person.id, name: "" }, { onSuccess: onClose })
            }
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
  const [naming, setNaming] = useState<FacePerson | null>(null);

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

      {pending > 0 && (
        <p className="faces-page__note">
          Still looking — {pending.toLocaleString()} photograph
          {pending === 1 ? "" : "s"} to go. Groups appear as they are found.
        </p>
      )}

      <div className="faces-page__grid">
        {ordered.map((p) => (
          <PersonTile
            key={p.id}
            person={p}
            onOpen={() => isAdmin && setNaming(p)}
          />
        ))}
      </div>

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
