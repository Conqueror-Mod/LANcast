import { Link } from "react-router-dom";
import { artworkURL } from "@/api/client";
import type { Credit } from "@/api/types";

/*
 * The cast row, and the two things it is.
 *
 * It is a list of who is in this, and it is a filter control. Extracted from
 * the detail page so the second half can be asserted at all: three of the five
 * features in v0.8.18 shipped built but unreachable, and each passed a suite
 * that had no way to ask "is this wired to anything". A component that renders
 * in isolation is one that can be asked.
 */

/*
 * Initials for a person with no picture.
 *
 * Two letters at most: "Jamie Lee Curtis" is JC rather than JLC, because the
 * circle is sized for a face and three letters in it stop looking like a
 * monogram and start looking like a mistake.
 */
export function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  const first = parts[0][0] ?? "";
  const last = parts.length > 1 ? parts[parts.length - 1][0] : "";
  return (first + last).toUpperCase();
}

function Face({ credit }: { credit: Credit }) {
  /*
   * A face when there is one, initials when there is not — which is most of a
   * cast list below the billed few, so the fallback is the design rather than
   * an afterthought. A row where three are pictures and nine are gaps reads as
   * broken; one where every entry is the same shape reads as a cast list.
   */
  if (credit.thumb) {
    return (
      <img
        className="detail__cast-face"
        src={artworkURL(credit.thumb, "thumb")}
        alt=""
        loading="lazy"
      />
    );
  }
  return (
    <span className="detail__cast-face detail__cast-face--none">
      {initialsOf(credit.name)}
    </span>
  );
}

/*
 * One cast member, as a filter control where that is possible.
 *
 * `actor` and not `person`: somebody clicking a name in a *cast* list means
 * "the films they are in", not "the films they had any hand in" — an
 * actor-director would otherwise bring their own direction back with them.
 * Both filters exist on the server and that difference is the only reason
 * there are two.
 *
 * A link rather than a button, so it opens in a new tab, copies as a URL and
 * comes back with Back. Filter state lives in the address here, which is what
 * makes all of that free.
 *
 * No id, or no library, means no link. A provider that gave only a name leaves
 * nothing to filter on, and a control that looks clickable and goes nowhere is
 * worse than one that never claimed to.
 */
function Member({ credit, libraryID }: { credit: Credit; libraryID?: number }) {
  const inner = (
    <>
      <Face credit={credit} />
      <span className="detail__cast-name">{credit.name}</span>
      {credit.character && (
        <span className="detail__cast-character">{credit.character}</span>
      )}
    </>
  );

  if (!credit.person_id || !libraryID) {
    return <div className="detail__cast-member">{inner}</div>;
  }
  return (
    <Link
      className="detail__cast-member detail__cast-member--link"
      to={`/library/${libraryID}?actor=${credit.person_id}`}
      title={`Everything ${credit.name} is in, in this library`}
    >
      {inner}
    </Link>
  );
}

export function CastRow({
  cast,
  libraryID,
}: {
  cast: Credit[];
  libraryID?: number;
}) {
  if (cast.length === 0) return null;
  return (
    <div className="detail__cast">
      <span className="section-label">Cast</span>
      <div className="detail__cast-row">
        {cast.map((c, i) => (
          <Member key={c.person_id ?? i} credit={c} libraryID={libraryID} />
        ))}
      </div>
    </div>
  );
}
