import { useState } from "react";
import { Link } from "react-router-dom";
import { useYearInReview } from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { runtime } from "@/lib/format";
import "./YearInReview.css";

/*
 * Your year, on your profile.
 *
 * The novelty is precisely that this is *not* a marketing artefact: every
 * number comes off playback history that has never left the machine, and
 * producing it contacts nothing. Which means the honesty is the feature, and
 * the copy has to carry it — a page like this is believed, so a figure that is
 * quietly inflated does more damage here than an error would.
 *
 * Two things are therefore said on screen rather than buried in a tooltip: that
 * a year holds the titles whose *last* play fell in it, and that time counts one
 * viewing of each. Both are consequences of one row per item per user, and both
 * make the numbers smaller than a marketing version would want them.
 */

const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

export function YearInReview() {
  const [chosen, setChosen] = useState<number | undefined>(undefined);
  const { data, isLoading, isError } = useYearInReview(chosen);

  if (isError) {
    return (
      <>
        <span className="section-label profile__label">Your year</span>
        <p className="browse__message">Your year could not be worked out.</p>
      </>
    );
  }

  const months = data?.months ?? [];
  const busiest = months.reduce((m, x) => Math.max(m, x.titles), 0);
  const years = data?.years ?? [];

  return (
    <section className="year">
      <div className="year__head">
        <span className="section-label profile__label">
          {data?.partial ? `${data.year} so far` : `Your ${data?.year ?? ""}`}
        </span>
        {/* Offered only when there is a choice to make. One year of history is
            not a picker, it is a label with a dropdown arrow on it. */}
        {years.length > 1 && (
          <select
            className="set-input year__pick"
            aria-label="Year"
            value={data?.year ?? ""}
            onChange={(e) => setChosen(Number(e.target.value))}
          >
            {years.map((y) => (
              <option key={y} value={y}>
                {y}
              </option>
            ))}
          </select>
        )}
      </div>

      {isLoading && <p className="browse__message">Working it out…</p>}

      {!isLoading && data && data.titles === 0 && (
        <p className="browse__message">
          Nothing played in {data.year}. Anything you watch or listen to shows
          up here.
        </p>
      )}

      {!isLoading && data && data.titles > 0 && (
        <>
          <div className="profile__stats">
            <Figure label="Titles" value={String(data.titles)} />
            <Figure label="Finished" value={String(data.finished)} />
            <Figure
              label="Put down"
              value={String(data.abandoned)}
              note="started and not finished"
            />
            <Figure
              label="Time spent"
              value={runtime(data.watched_ms) || "0m"}
              /*
               * The qualifier is the honest part. Only the most recent play of
               * each title is recorded, so counting a rewatch would mean
               * attributing every viewing to the year of the last one — this
               * figure is deliberately low rather than invented upward.
               */
              note="one viewing of each counted"
            />
            <Figure
              label="Libraries"
              value={String(data.libraries)}
              note="how wide you ranged"
            />
          </div>

          <div className="year__chart" role="img" aria-label={monthSummary(months)}>
            {months.map((m) => (
              <div className="year__month" key={m.month}>
                <div className="year__bar-track">
                  <div
                    className="year__bar"
                    style={{
                      height: busiest > 0 ? `${(m.titles / busiest) * 100}%` : "0%",
                    }}
                  />
                </div>
                <span className="year__month-label">{MONTHS[m.month - 1]}</span>
                <span className="year__month-count">{m.titles || ""}</span>
              </div>
            ))}
          </div>

          {data.kinds && data.kinds.length > 0 && (
            <p className="year__kinds">
              {data.kinds
                .map((k) => `${k.titles} ${plural(k.kind, k.titles)}`)
                .join(" · ")}
            </p>
          )}

          <div className="year__ends">
            {data.first && <End label="Started the year with" item={data.first} />}
            {/* Suppressed when the year holds one title, which is both ends of
                itself — showing it twice reads as a bug rather than as a small
                year. */}
            {data.last && data.last.id !== data.first?.id && (
              <End label={data.partial ? "Most recently" : "Ended the year with"} item={data.last} />
            )}
          </div>

          <p className="year__note">
            Worked out on this machine, from what you have played. Nothing is
            sent anywhere. A title counts in the year you <em>last</em> played
            it: the server keeps where you are in something, not a diary of
            every sitting.
          </p>
        </>
      )}
    </section>
  );
}

/*
 * A local twin of Profile's Stat rather than an import.
 *
 * It renders the same three elements and shares its stylesheet, which is the
 * point — these cards sit directly beneath that row and a second visual
 * language for the same shape would read as two pages stapled together. Kept
 * separate only because exporting a component out of a screen to a component is
 * the wrong direction for this tree.
 */
function Figure({
  label,
  value,
  note,
}: {
  label: string;
  value: string;
  note?: string;
}) {
  return (
    <div className="profile__stat">
      <span className="profile__statvalue">{value}</span>
      <span className="profile__statlabel">{label}</span>
      {note && <span className="profile__statnote">{note}</span>}
    </div>
  );
}

function End({ label, item }: { label: string; item: { id: number; title: string; artwork?: { poster?: string } } }) {
  const poster = artworkURL(item.artwork?.poster, "poster");
  return (
    <Link className="year__end" to={`/item/${item.id}`}>
      {poster ? (
        <img className="year__end-art" src={poster} alt="" draggable={false} />
      ) : (
        <div className="year__end-art year__end-art--blank" aria-hidden="true" />
      )}
      <span className="year__end-label">{label}</span>
      <span className="year__end-title">{item.title}</span>
    </Link>
  );
}

// The chart is decorative to a screen reader unless it says what it shows, and
// twelve individually-labelled bars would be twelve stops on the way past it.
function monthSummary(months: { month: number; titles: number }[]): string {
  const said = months
    .filter((m) => m.titles > 0)
    .map((m) => `${MONTHS[m.month - 1]} ${m.titles}`)
    .join(", ");
  return said ? `Titles by month: ${said}` : "Nothing played this year";
}

function plural(kind: string, n: number): string {
  if (n === 1) return kind;
  return kind.endsWith("s") ? kind : kind + "s";
}
