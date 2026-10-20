import { useAfterwards } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { runtime } from "@/lib/format";
import type { Item } from "@/api/types";

/*
 * The page a film ends on.
 *
 * Before this, a film with nothing queued after it simply stopped: a black
 * frame, a transport bar, and the decision about what to do next left
 * entirely to somebody who had just sat through the credits. Asked for in as
 * many words: when a film ends, suggest what to watch, and when the film is
 * in a collection, one suggestion is the next film in it.
 *
 * # Two kinds of suggestion, said differently
 *
 * The next film in a collection is a fact about the franchise; a similar film
 * is a judgement. So they are labelled apart — "Next in Alien Collection"
 * against "More like Alien" — and the server keeps them apart too: similar
 * films are never from the same collection (store/afterwards.go), so the slot
 * for "something else" cannot be filled by the sequel a second time.
 *
 * # Nothing starts by itself
 *
 * Up Next counts down because an episode's next episode is what nearly
 * everybody wants. After a film it is not: the evening may be over. The page
 * offers and waits.
 *
 * Only for a film. An episode's end belongs to Up Next and its queue, and a
 * track's to Now Playing.
 */
export function AfterFilm({
  film,
  onPlay,
  onAgain,
  onClose,
}: {
  film: Item;
  onPlay: (id: number) => void;
  onAgain: () => void;
  onClose: () => void;
}) {
  const { data, isLoading } = useAfterwards(film.id);
  return (
    <AfterFilmView film={film} items={data ?? []} loading={isLoading} onPlay={onPlay} onAgain={onAgain} onClose={onClose} />
  );
}

/** The page itself, given its suggestions: what the design bench renders. */
export function AfterFilmView({
  film,
  items,
  loading,
  onPlay,
  onAgain,
  onClose,
}: {
  film: Item;
  items: Item[];
  loading: boolean;
  onPlay: (id: number) => void;
  onAgain: () => void;
  onClose: () => void;
}) {
  const backdrop = artworkURL(film.artwork?.fanart, "fanart");

  return (
    <div className="after" role="dialog" aria-label={`Finished ${film.title}`}>
      {backdrop && <div className="after__backdrop" style={{ backgroundImage: `url(${backdrop})` }} aria-hidden="true" />}
      <div className="after__body">
        <div className="after__eyebrow">You finished</div>
        <h2 className="after__title">{film.title}</h2>

        {items.length > 0 && (
          <ul className="after__cards">
            {items.map((it) => (
              <li key={it.id}>
                <Suggestion item={it} film={film} onPlay={onPlay} />
              </li>
            ))}
          </ul>
        )}
        {!loading && items.length === 0 && (
          <p className="after__none">Nothing in this library is close enough to suggest.</p>
        )}

        <div className="after__actions">
          <button className="after__btn" onClick={onAgain}>
            Watch again
          </button>
          <button className="after__btn" onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </div>
  );
}

/** Why this one: the collection it continues, or the film it is like. */
export function suggestionReason(item: Item, film: Item): string {
  const from = item.continues_from;
  if (from) return `Next in ${from.collection_title}`;
  return `More like ${film.title}`;
}

function Suggestion({ item, film, onPlay }: { item: Item; film: Item; onPlay: (id: number) => void }) {
  // Fanart before poster: the card is landscape, and a poster cropped to a
  // letterbox is a face cut in half.
  const art = artworkURL(item.artwork?.fanart, "poster") ?? artworkURL(item.artwork?.poster, "poster");
  const meta = [item.year ? String(item.year) : "", runtime(item.duration_ms)].filter(Boolean).join(" · ");
  return (
    <button className="after__card" onClick={() => onPlay(item.id)}>
      <span className="after__art" style={art ? { backgroundImage: `url(${art})` } : undefined} aria-hidden="true" />
      <span className="after__reason">{suggestionReason(item, film)}</span>
      <span className="after__name">{item.title}</span>
      {meta && <span className="after__meta">{meta}</span>}
    </button>
  );
}
