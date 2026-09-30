import { useState } from "react";
import {
  useTags,
  useItemTags,
  useAddTag,
  useRemoveTag,
  useSetFavourite,
} from "@/api/hooks";
import "./TagItem.css";

/*
 * Your own words on an item, and the heart (ADR 0062).
 *
 * THE HEART IS A SHAPE, NOT A COLOUR
 *
 * `docs/design.md` names "favorite" explicitly as a thing gold must never mean,
 * and gold is the system's only accent — so a favourite cannot be gold, and
 * inventing a second accent to carry it would answer the letter of that rule
 * while breaking its purpose. Gold means one thing precisely because nothing
 * competes for the eye.
 *
 * So this carries no hue at all: an outline heart at rest, filled when set,
 * both in the existing text ramp. A star was refused because the rating control
 * beside it already uses stars, and one glyph meaning "I rated this four" and
 * "I like this" is worse than no affordance.
 *
 * PRIVATE, AND SAID SO
 *
 * These are private to the signed-in account. The screen says it once, plainly,
 * because a person deciding whether to write *needs a better copy* on something
 * is entitled to know who reads it — and a feature people use carefully because
 * they are unsure is one that may as well not exist.
 */
export function TagItem({ itemID }: { itemID: number }) {
  const { data } = useItemTags(itemID);
  const add = useAddTag(itemID);
  const remove = useRemoveTag(itemID);
  const favourite = useSetFavourite(itemID);
  const [draft, setDraft] = useState("");
  const [open, setOpen] = useState(false);
  const all = useTags(open).data?.tags ?? [];

  const tags = data?.tags ?? [];
  /*
   * Your other tags, offered while you are adding one.
   *
   * A tag is only useful if it is spelled the same way each time: "Bangers" on
   * one film and "bangers!" on the next are two lists. So the ones you already
   * use are offered as you type — filtered by what is typed, most-used first,
   * and never one this item already carries. Fetched only once the box is
   * focused, so a detail page opened to press Play does not ask for them.
   */
  const onItem = new Set(tags.map((t) => t.id));
  const needle = draft.trim().toLowerCase();
  const suggestions = all
    .filter((t) => !onItem.has(t.id) && t.name.toLowerCase().includes(needle))
    .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
    .slice(0, 8);
  const addTag = (name: string) =>
    add.mutate(name, { onSuccess: () => setDraft("") });
  const isFavourite = data?.favourite ?? false;

  return (
    <div className="tagitem">
      <div className="tagitem__row">
        <button
          className={"tagitem__heart" + (isFavourite ? " is-set" : "")}
          onClick={() => favourite.mutate(!isFavourite)}
          disabled={favourite.isPending}
          aria-pressed={isFavourite}
          aria-label={
            isFavourite ? "Remove from favourites" : "Add to favourites"
          }
          title={isFavourite ? "Remove from favourites" : "Add to favourites"}
        >
          {/*
            One path, filled or stroked. Two icons would drift apart the first
            time either was adjusted.
          */}
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <path
              d="M12 20.5 3.8 12.3a5.1 5.1 0 0 1 7.2-7.2l1 1 1-1a5.1 5.1 0 1 1 7.2 7.2Z"
              fill={isFavourite ? "currentColor" : "none"}
              stroke="currentColor"
              strokeWidth="1.6"
              strokeLinejoin="round"
            />
          </svg>
        </button>

        <span className="tagitem__label">Your tags</span>
      </div>

      <div className="tagitem__tags">
        {tags.map((t) => (
          <span className="tagitem__tag" key={t.id}>
            {t.name}
            <button
              className="tagitem__remove"
              onClick={() => remove.mutate(t.id)}
              disabled={remove.isPending}
              aria-label={`Remove the tag ${t.name}`}
              title={`Remove the tag ${t.name}`}
            >
              ×
            </button>
          </span>
        ))}

        {/* Open while focus is anywhere inside, so tabbing or clicking from
            the box to a suggestion does not close the list under it. */}
        <div
          className="tagitem__adder"
          onFocus={() => setOpen(true)}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
          }}
        >
          <form
            className="tagitem__add"
            onSubmit={(e) => {
              e.preventDefault();
              const name = draft.trim();
              if (!name) return;
              addTag(name);
            }}
          >
            <input
              className="tagitem__input"
              type="text"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="Add a tag"
              aria-label="Add a tag"
              maxLength={64}
            />
          </form>
          {open && suggestions.length > 0 && (
            <div className="tagitem__suggest" role="group" aria-label="Your other tags">
              {suggestions.map((t) => (
                <button
                  key={t.id}
                  type="button"
                  className="tagitem__suggestion"
                  onClick={() => addTag(t.name)}
                  disabled={add.isPending}
                  title={`Add the tag ${t.name}`}
                >
                  {t.name}
                  <span className="tagitem__suggestcount">{t.count}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      <p className="tagitem__note">
        Only you can see your tags and favourites. Nobody else on this server
        can read them, and they are not written into files beside your media.
      </p>
    </div>
  );
}
