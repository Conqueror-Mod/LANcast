import { useFocusable } from "@/focus/FocusController";
import { useSetContentRating } from "@/api/hooks";
import { ratingChoices } from "@/lib/ratings";
import "./ContentRatingPicker.css";

/*
 * Rating an item by hand, for an administrator.
 *
 * A ceiling hides everything unrated (internal/rating says why), and until
 * this existed the only way to rate something was a hand-written request — so
 * the rule's own answer, "fixable by rating the item", could not be followed
 * from the app. Most NES and SNES games predate the ESRB and nothing will
 * ever rate them.
 *
 * Buttons rather than a dropdown, so a pad reaches every rung. The chosen one
 * is marked in blue: gold means where focus is and nothing else.
 *
 * The rating locks when set, so a rescan or a provider refresh never undoes
 * it. There is no "Not rated" choice once one is set: unrated is what a
 * ceiling hides, and nobody rates a game in order to hide it.
 */
export function ContentRatingPicker({
  id,
  kind,
  current,
  label = "Content rating",
}: {
  id: number;
  kind: string;
  current?: string | null;
  label?: string;
}) {
  const set = useSetContentRating();
  const choices = ratingChoices(kind, current);
  return (
    <div className="rating-pick" role="group" aria-label={label}>
      <span className="rating-pick__label">{label}</span>
      <div className="rating-pick__choices">
        {choices.map((c) => (
          <Choice
            key={c}
            label={c}
            chosen={c === current}
            busy={set.isPending}
            onPress={() => {
              if (c !== current) set.mutate({ id, rating: c });
            }}
          />
        ))}
      </div>
      {!current && <span className="rating-pick__note">Not rated: hidden from every account with a ceiling.</span>}
      {set.error && (
        <span className="rating-pick__note rating-pick__note--warn">
          {String((set.error as Error).message ?? set.error)}
        </span>
      )}
    </div>
  );
}

function Choice({
  label,
  chosen,
  busy,
  onPress,
}: {
  label: string;
  chosen: boolean;
  busy: boolean;
  onPress: () => void;
}) {
  const focusable = useFocusable(onPress);
  return (
    <button
      {...focusable}
      className={chosen ? "rating-pick__choice is-chosen" : "rating-pick__choice"}
      aria-pressed={chosen}
      disabled={busy}
      onClick={onPress}
    >
      {label.replace(/^ESRB /, "")}
    </button>
  );
}
