import { useSetCollectionPoster } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useBackHandler } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./ChoosePoster.css";

/*
 * Which of its films a collection wears.
 *
 * The server picks the earliest release when a collection has no image of its
 * own, which is right for almost every franchise and wrong for some — a Marvel
 * Cinematic Universe wearing Iron Man (2008) is defensible and is not what
 * somebody who has looked at it wants. So this is the disagreement, and it is
 * the shape every correction in this client takes: the rule is the default, and
 * a person may overrule it.
 *
 * A grid of the posters themselves rather than a list of titles, because the
 * question being answered is "which of these looks right as the face of the
 * franchise" — and that is not a question anybody can answer from names.
 */
export function ChoosePoster({
  collection,
  members,
  onClose,
}: {
  collection: Item;
  members: Item[];
  onClose: () => void;
}) {
  const set = useSetCollectionPoster(collection.id);
  useBackHandler(onClose);

  // Only films that have one to lend. A member with no poster of its own is a
  // blank tile in a picker of pictures, and the server refuses it anyway.
  const choices = members.filter((m) => m.artwork?.poster);

  /*
   * `inherited` is the whole reason this can say something useful.
   *
   * A borrowed poster means no choice has been made and the default is in
   * force; an owned one means somebody chose. Without that flag the dialog
   * could not tell "showing Iron Man because it is first" from "showing Iron
   * Man because it was picked", and Reset would be offered when there is
   * nothing to reset.
   */
  const chosen = !collection.artwork?.inherited && !!collection.artwork?.poster;

  const pick = (id: number) =>
    set.mutate(id, { onSuccess: () => onClose() });

  return (
    <div className="chooseposter__overlay" onClick={onClose}>
      <div
        className="chooseposter"
        role="dialog"
        aria-label={`Choose a poster for ${collection.title}`}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="chooseposter__head">
          <span className="section-label">Collection poster</span>
          <button
            className="chooseposter__x"
            onClick={onClose}
            aria-label="Close"
          >
            ✕
          </button>
        </div>

        <p className="chooseposter__lead">
          {chosen
            ? "This collection is using a poster you chose."
            : "This collection is borrowing its earliest film's poster."}
        </p>

        {set.isError && (
          <p className="chooseposter__error">
            {(set.error as Error)?.message ?? "Could not set that poster."}
          </p>
        )}

        <div className="chooseposter__grid">
          {choices.map((m) => {
            const current =
              collection.artwork?.poster === m.artwork?.poster;
            return (
              <button
                key={m.id}
                type="button"
                className={
                  "chooseposter__choice" +
                  (current ? " chooseposter__choice--current" : "")
                }
                disabled={set.isPending}
                onClick={() => pick(m.id)}
                title={m.title}
              >
                <img
                  src={artworkURL(m.artwork?.poster, "poster")}
                  alt={m.title}
                  loading="lazy"
                  draggable={false}
                />
                <span className="chooseposter__caption">{m.title}</span>
              </button>
            );
          })}
        </div>

        {/*
          Reset only when there is something to reset. An override somebody
          cannot take back is a trap, and the default is a rule that improves --
          a franchise whose first film arrives later should start wearing it
          again.
        */}
        {chosen && (
          <button
            type="button"
            className="chooseposter__reset"
            disabled={set.isPending}
            onClick={() => set.mutate(0, { onSuccess: () => onClose() })}
          >
            Use the default again
          </button>
        )}
      </div>
    </div>
  );
}
