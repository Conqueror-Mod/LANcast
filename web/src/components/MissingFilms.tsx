import { artworkURL } from "@/api/client";
import type { CollectionFilm } from "@/api/types";
import "./MissingFilms.css";

/*
 * The films a collection holds that the library does not (ADR 0093).
 *
 * Drawn after the films that are here, dimmed, and deliberately not buttons:
 * there is nothing to open — no file, no detail page — and a tile that looks
 * pressable and does nothing is worse than one that plainly is not. Each one
 * carries its title and year in text, so the dimming is never the only thing
 * saying what it is.
 *
 * An announced film says so instead of its year: "Coming 2027", or "Not yet
 * released" with no date at all, because "missing" is the wrong word for a
 * film nobody can have yet.
 */
export function MissingFilms({ films }: { films: CollectionFilm[] }) {
  if (films.length === 0) return null;
  return (
    <section className="detail__children missing-films" aria-label="Not in your library">
      <span className="section-label">Not in your library</span>
      <ul className="detail__children-grid missing-films__grid">
        {films.map((f) => {
          const poster = artworkURL(f.poster, "poster");
          return (
            <li key={f.external_id} className="missing-film" title={f.overview || undefined}>
              <div className="missing-film__poster">
                {poster ? (
                  <img src={poster} alt="" loading="lazy" />
                ) : (
                  <span className="missing-film__blank" aria-hidden="true">
                    {f.title}
                  </span>
                )}
              </div>
              <span className="missing-film__title">{f.title}</span>
              <span className="missing-film__year">{when(f)}</span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function when(f: CollectionFilm): string {
  if (f.upcoming) return f.year ? `Coming ${f.year}` : "Not yet released";
  return f.year ? String(f.year) : "";
}
