import { useEffect, useState } from "react";
import { useRating, useSetRating, useClearRating } from "@/api/hooks";
import "./RateItem.css";

/*
 * What *you* thought of it.
 *
 * Deliberately not shown next to the provider's score in a way that invites
 * comparison — the detail page already carries TMDB's rating and, where OMDb
 * answered, IMDb's and Rotten Tomatoes' (ADR 0019). Three numbers about one
 * film is one too many to leave unlabelled, so this one says whose it is in
 * words rather than trusting a star to imply it.
 *
 * Five stars over a ten-point score, with a half-star as the second click on
 * the same star. The API is out of ten precisely so this interface could exist
 * without a migration; the stars are what people expect and the tens are what
 * the database can carry alongside provider ratings on the same scale.
 *
 * Nobody else can see this. That is the decision the roadmap left unmade about
 * ratings generally, made only for the private half — so the control says so
 * once, quietly, rather than leaving somebody to wonder whether the household
 * can read their note.
 */
export function RateItem({ itemID }: { itemID: number }) {
  const { data } = useRating(itemID);
  const set = useSetRating(itemID);
  const clear = useClearRating(itemID);

  const rating = data?.rating ?? null;
  const [hover, setHover] = useState<number | null>(null);
  const [noteOpen, setNoteOpen] = useState(false);
  const [note, setNote] = useState("");

  // The note follows the item: leaving one film's review in the box when you
  // navigate to another is how somebody posts the wrong thing.
  useEffect(() => {
    setNote(rating?.review ?? "");
  }, [rating?.review, itemID]);

  const score = hover ?? rating?.score ?? 0;

  const submit = (next: number) => {
    if (next < 1 || next > 10) return;
    set.mutate({ score: next, review: note });
  };

  return (
    <div className="rate">
      <div className="rate__row">
        <span className="rate__label">Your rating</span>

        <div
          className="rate__stars"
          onMouseLeave={() => setHover(null)}
          role="group"
          aria-label="Your rating out of five stars"
        >
          {[1, 2, 3, 4, 5].map((star) => {
            const full = score >= star * 2;
            const half = !full && score >= star * 2 - 1;
            return (
              <button
                key={star}
                type="button"
                className={
                  "rate__star" +
                  (full ? " is-full" : "") +
                  (half ? " is-half" : "")
                }
                // A second click on the star you are already on gives the half
                // below it, which is the only way to reach an odd score without
                // a second row of controls.
                onClick={() =>
                  submit(rating?.score === star * 2 ? star * 2 - 1 : star * 2)
                }
                onMouseEnter={() => setHover(star * 2)}
                aria-label={`${star} star${star === 1 ? "" : "s"}`}
                aria-pressed={full}
              >
                ★
              </button>
            );
          })}
        </div>

        {rating && (
          <span className="rate__score">{(rating.score / 2).toFixed(1)}</span>
        )}

        {rating && (
          <button
            type="button"
            className="rate__clear"
            onClick={() => clear.mutate()}
            disabled={clear.isPending}
            // Withdrawing is not scoring it one, and the wording has to make
            // that obvious or people give low scores to mean "never mind".
            title="Remove your rating"
          >
            Clear
          </button>
        )}

        <button
          type="button"
          className="rate__notebtn"
          onClick={() => setNoteOpen((o) => !o)}
          aria-expanded={noteOpen}
        >
          {rating?.review ? "Edit note" : "Add a note"}
        </button>
      </div>

      {noteOpen && (
        <div className="rate__note">
          <textarea
            className="rate__textarea"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Why — for you, later. Nobody else can see this."
            rows={3}
            maxLength={4000}
          />
          <div className="rate__noteactions">
            <button
              type="button"
              className="rate__save"
              onClick={() => {
                // A note needs a score to hang off: the API keys a rating on
                // one, so saving a note alone would have nothing to attach to.
                submit(rating?.score ?? 6);
                setNoteOpen(false);
              }}
              disabled={set.isPending}
            >
              Save
            </button>
            <span className="rate__private">Private to you</span>
          </div>
        </div>
      )}

      {!noteOpen && rating?.review && (
        <p className="rate__review">{rating.review}</p>
      )}
    </div>
  );
}
