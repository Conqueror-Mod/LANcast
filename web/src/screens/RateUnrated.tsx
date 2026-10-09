import { useNavigate, useParams } from "react-router-dom";
import { useIsAdmin, useLibraries, useUnrated } from "@/api/hooks";
import { ContentRatingPicker } from "@/components/ContentRatingPicker";
import { platformLabel } from "@/lib/platforms";
import { episodeLabel } from "@/lib/format";
import type { Item } from "@/api/types";
import "./RateUnrated.css";

/*
 * Everything in a library that a ceiling hides for want of a rating, to be
 * rated by hand (an administrator's page).
 *
 * Built for retro games, where it is most of the library: measured on a real
 * one, 50 of 91 games unrated — every N64 game, and the NES and SNES games
 * that came out before the ESRB existed. Each row leaves the list the moment
 * it is rated, because the list is the server's answer to "what is still
 * hidden", re-asked after every rating.
 */
export function RateUnrated() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const admin = useIsAdmin();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data, isLoading } = useUnrated(libraryID, admin);
  const items = data?.items ?? [];

  return (
    <div className="unrated">
      <div className="unrated__bar">
        <button className="unrated__back" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1 className="unrated__title">
          {library?.name ?? "Library"} <span>not rated</span>
        </h1>
        {data && data.total > 0 && <span className="unrated__total">{data.total} left</span>}
      </div>
      <p className="unrated__lede">
        An account with a content ceiling never sees anything unrated, because LANcast cannot
        tell whether it is suitable. An episode takes its show&apos;s rating, so rating a show
        clears its episodes from this list too. Rate each one here and it appears for every account its
        rating allows. A rating you set is locked, so a rescan never undoes it.
      </p>
      {!admin && <p className="unrated__empty">Only an administrator can rate titles.</p>}
      {admin && isLoading && <p className="unrated__empty">Looking…</p>}
      {admin && data && items.length === 0 && (
        <p className="unrated__empty">Everything here is rated. Nothing is hidden for want of one.</p>
      )}
      <ul className="unrated__list">
        {items.map((it) => (
          <li className="unrated__row" key={it.id}>
            <div className="unrated__what">
              <span className="unrated__name">{it.title}</span>
              <span className="unrated__sub">{subtitle(it)}</span>
            </div>
            <ContentRatingPicker id={it.id} kind={it.kind} current={it.content_rating} label={`Rate ${it.title}`} />
          </li>
        ))}
      </ul>
    </div>
  );
}

// What the row is, under its title: a game's console, an episode's show and
// number (an episode title alone identifies nothing), and the year.
function subtitle(it: Item): string {
  return [
    it.kind === "rom" ? platformLabel(it.platform) : "",
    episodeLabel(it) ?? "",
    it.year ? String(it.year) : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
