import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useLibraries,
  usePhotoTimeline,
  usePhotosInMonth,
  type TimelineBucket,
} from "@/api/hooks";
import { PosterTile } from "@/components/PosterTile";
import { PhotoViewer } from "@/components/PhotoViewer";
import { useFocusable } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./Timeline.css";

const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

function label(b: TimelineBucket): string {
  if (b.undated) return "No date";
  return `${MONTHS[b.month - 1]} ${b.year}`;
}

function key(b: TimelineBucket): string {
  return b.undated ? "undated" : `${b.year}-${b.month}`;
}

/*
 * One month, opened.
 *
 * The photographs are fetched when the month is opened and not before: a
 * library of 3,676 is a page nobody wants, and the counts alone are enough to
 * scroll a decade.
 */
function Month({
  libraryID,
  bucket,
  onShow,
}: {
  libraryID: number;
  bucket: TimelineBucket;
  onShow: (photos: Item[], at: number) => void;
}) {
  const { data, isLoading } = usePhotosInMonth(libraryID, bucket);
  const items = data?.items ?? [];

  return (
    <div className="timeline__grid">
      {isLoading && <p className="timeline__loading">Loading…</p>}
      {/* The whole month is handed to the viewer, not one picture: arrowing
          through a month is the thing a timeline is for, and passing a single
          item would make every photograph a dead end. */}
      {items.map((item, i) => (
        <PosterTile
          key={item.id}
          item={item}
          onOpen={() => onShow(items, i)}
        />
      ))}
    </div>
  );
}

function MonthHeader({
  bucket,
  open,
  onToggle,
}: {
  bucket: TimelineBucket;
  open: boolean;
  onToggle: () => void;
}) {
  const focusable = useFocusable(onToggle);
  return (
    <button
      {...focusable}
      className={"timeline__head" + (open ? " is-open" : "")}
      onClick={onToggle}
      aria-expanded={open}
    >
      <span className="timeline__month">{label(bucket)}</span>
      <span className="timeline__rule" />
      <span className="timeline__count">{bucket.count}</span>
    </button>
  );
}

/*
 * A picture library by when the pictures were taken.
 *
 * A folder grid answers "where did I put it". This answers "when was that",
 * which is the question people actually arrive at a photo library with, and the
 * one a folder grid cannot answer — a holiday spread across three folders is
 * one week, and three folders is how it looks in the grid.
 *
 * **Marked folders are not here**, deliberately (ADR 0051, amended). A cover
 * can only be lifted in the library grid or inside the folder itself, so these
 * could never be uncovered on this screen — and a row of covered tiles in the
 * middle of a holiday still discloses when the marked photographs were taken,
 * which is most of what marking a folder is trying not to say. They stay
 * reachable exactly where the amendment puts them.
 */
export function Timeline() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data, isLoading } = usePhotoTimeline(libraryID);
  /*
   * The newest month starts open, and the rest closed.
   *
   * Opening everything would fetch the whole library through the back door,
   * which is the thing the buckets exist to avoid. Opening nothing gives a
   * screen of headings and no photographs, which reads as broken.
   */
  const [openKeys, setOpenKeys] = useState<Set<string> | null>(null);
  const [shown, setShown] = useState<{ photos: Item[]; at: number } | null>(
    null,
  );

  const buckets = data?.buckets ?? [];
  const open =
    openKeys ?? new Set(buckets.length > 0 ? [key(buckets[0])] : []);

  const toggle = (b: TimelineBucket) => {
    const next = new Set(open);
    const k = key(b);
    if (next.has(k)) next.delete(k);
    else next.add(k);
    setOpenKeys(next);
  };

  return (
    <div className="timeline">
      <div className="timeline__bar">
        <button className="timeline__back" onClick={() => navigate(-1)}>
          ← Back
        </button>
        <h1 className="timeline__title">
          {library?.name ?? "Photos"} <span>by date</span>
        </h1>
        {data && <span className="timeline__total">{data.total} photos</span>}
      </div>

      {isLoading && <p className="timeline__loading">Reading dates…</p>}

      {!isLoading && buckets.length === 0 && (
        // Not "no photos": the library may be full of pictures that carry no
        // capture time, and saying the wrong one sends somebody looking for a
        // scanning fault that is not there.
        <p className="timeline__empty">
          Nothing here carries a date yet. Capture time comes from the
          photograph itself, and a scan reads it.
        </p>
      )}

      {/*
        No cover-lifting wrapper here, and none is needed: marked folders are
        excluded from the timeline entirely (see PhotoTimeline), so there is
        nothing on this screen to uncover.
      */}
      {buckets.map((b) => (
          <section key={key(b)} className="timeline__section">
            <MonthHeader
              bucket={b}
              open={open.has(key(b))}
              onToggle={() => toggle(b)}
            />
            {open.has(key(b)) && (
              <Month
                libraryID={libraryID}
                bucket={b}
                onShow={(photos, at) => setShown({ photos, at })}
              />
            )}
          </section>
        ))}

      {shown && (
        <PhotoViewer
          photos={shown.photos}
          startAt={shown.at}
          onClose={() => setShown(null)}
        />
      )}
    </div>
  );
}
