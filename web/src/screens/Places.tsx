import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useIsAdmin,
  useLibraries,
  usePhotoPlaces,
  usePhotosInPlace,
  useUpdateSettings,
} from "@/api/hooks";
import type { PhotoPlace } from "@/api/types";
import { PosterTile } from "@/components/PosterTile";
import { PhotoViewer } from "@/components/PhotoViewer";
import { useFocusable } from "@/focus/FocusController";
import type { Item } from "@/api/types";
import "./Timeline.css";
import "./Places.css";

type PlaceKey = number | "elsewhere";

/*
 * Where a place is, said the way a person would: the town, then the region
 * and country it is in. The country is left off when it is the only one the
 * library has, because "Texas, United States" ten times over is noise.
 */
function where(p: PhotoPlace, oneCountry: boolean): string {
  const parts = [p.region, oneCountry ? "" : p.country].filter(Boolean);
  return parts.join(", ");
}

function Place({
  libraryID,
  place,
  onShow,
}: {
  libraryID: number;
  place: PlaceKey;
  onShow: (photos: Item[], at: number) => void;
}) {
  const { data, isLoading } = usePhotosInPlace(libraryID, place);
  const items = data?.items ?? [];
  return (
    <div className="timeline__grid">
      {isLoading && <p className="timeline__loading">Loading…</p>}
      {/* The whole place goes to the viewer, so arrowing walks the town's
          photographs rather than stopping at the one that was opened. */}
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

function PlaceHeader({
  name,
  detail,
  count,
  open,
  onToggle,
}: {
  name: string;
  detail: string;
  count: number;
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
      <span className="timeline__month">
        {name}
        {detail && <span className="places__where">{detail}</span>}
      </span>
      <span className="timeline__rule" />
      <span className="timeline__count">{count}</span>
    </button>
  );
}

/*
 * The switch, offered where the feature is missing rather than three screens
 * away. Only an administrator can press it; anybody else is told who can.
 */
function TurnOn() {
  const isAdmin = useIsAdmin();
  const update = useUpdateSettings();
  return (
    <div className="places__off">
      <p>
        LANcast can group these photographs by the town they were taken in,
        using the location a phone or camera writes into each picture. Nothing
        is looked up online: the towns come from a list built into the server.
      </p>
      <p>
        It is off until somebody turns it on, and turning it off again deletes
        every location it read.
      </p>
      {isAdmin ? (
        <button
          className="browse__playall-btn"
          disabled={update.isPending}
          onClick={() => update.mutate({ photo_places: true })}
        >
          Read where photos were taken
        </button>
      ) : (
        <p className="places__hint">
          An administrator can turn this on in Settings, under Pictures.
        </p>
      )}
      {update.isError && (
        <p className="places__hint">That did not save. Try again.</p>
      )}
    </div>
  );
}

/*
 * A picture library by where the pictures were taken (ADR 0078).
 *
 * The Timeline's sibling, and built from its parts: a folder grid answers
 * "where did I put it", the timeline "when was that", and this "where were
 * we". Most photographs in any library carry no location, so the count of
 * those is said plainly at the foot rather than left to look like loss.
 *
 * Marked folders are not here, for the timeline's reason (ADR 0051, amended).
 */
export function Places() {
  const { id } = useParams();
  const libraryID = Number(id);
  const navigate = useNavigate();
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);
  const { data, isLoading } = usePhotoPlaces(libraryID);
  const [openKeys, setOpenKeys] = useState<Set<PlaceKey> | null>(null);
  const [shown, setShown] = useState<{ photos: Item[]; at: number } | null>(
    null,
  );

  const places = data?.places ?? [];
  const elsewhere = data?.elsewhere ?? 0;
  const located = places.reduce((n, p) => n + p.count, 0) + elsewhere;
  const oneCountry = new Set(places.map((p) => p.country_code)).size <= 1;
  // The most photographed place starts open, as the newest month does on the
  // timeline: everything open would fetch the whole library, nothing open
  // reads as broken.
  const open =
    openKeys ?? new Set<PlaceKey>(places.length > 0 ? [places[0].id] : []);
  const toggle = (k: PlaceKey) => {
    const next = new Set(open);
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
          {library?.name ?? "Photos"} <span>by place</span>
        </h1>
        {data?.enabled && located > 0 && (
          <span className="timeline__total">{located} photos</span>
        )}
      </div>

      {isLoading && <p className="timeline__loading">Loading places…</p>}

      {data && !data.enabled && <TurnOn />}

      {data?.enabled && data.reading && (
        <p className="timeline__loading">Reading where photos were taken…</p>
      )}

      {data?.enabled && !data.reading && located === 0 && (
        <p className="timeline__empty">
          None of these photographs say where they were taken. Phones and
          cameras only write a location when theirs is switched on.
        </p>
      )}

      {data?.enabled &&
        places.map((p) => (
          <section key={p.id} className="timeline__section">
            <PlaceHeader
              name={p.name}
              detail={where(p, oneCountry)}
              count={p.count}
              open={open.has(p.id)}
              onToggle={() => toggle(p.id)}
            />
            {open.has(p.id) && (
              <Place
                libraryID={libraryID}
                place={p.id}
                onShow={(photos, at) => setShown({ photos, at })}
              />
            )}
          </section>
        ))}

      {data?.enabled && elsewhere > 0 && (
        <section className="timeline__section">
          <PlaceHeader
            name="Far from any town"
            detail=""
            count={elsewhere}
            open={open.has("elsewhere")}
            onToggle={() => toggle("elsewhere")}
          />
          {open.has("elsewhere") && (
            <Place
              libraryID={libraryID}
              place="elsewhere"
              onShow={(photos, at) => setShown({ photos, at })}
            />
          )}
        </section>
      )}

      {data?.enabled && data.unlocated > 0 && (
        <p className="places__foot">
          {data.unlocated} {data.unlocated === 1 ? "photo says" : "photos say"}{" "}
          nothing about where {data.unlocated === 1 ? "it was" : "they were"}{" "}
          taken.
        </p>
      )}

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
