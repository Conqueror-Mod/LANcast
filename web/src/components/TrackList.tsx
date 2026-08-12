import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useFocusable } from "@/focus/FocusController";
import { useIsAdmin, useItem } from "@/api/hooks";
import { clock } from "@/lib/format";
import type { Item } from "@/api/types";
import { RemoveDialog } from "./RemoveDialog";
import "./TrackList.css";

// A record is a numbered list, not a grid. Rendering an album's tracks as
// poster tiles the way a season renders its episodes would show one cover
// twelve times and drop the two things that identify a track: its number and
// its length.
//
// Rows carry the whole album as their queue, so playing track 5 continues
// through the record rather than stopping at the end of one song. That is the
// same ?queue= the container Play-all builds — one mechanism, not a second one
// for music.

function trackQueue(tracks: Item[]): string {
  return tracks.map((t) => t.id).join(",");
}

/*
 * Titles that appear more than once in the same list.
 *
 * Embedded tags are the authoritative local source for music (ADR 0024), and
 * real files lie: a folder here has You're Pretty When I'm Drunk.mp3 tagged
 * title="Fire Water Burn" track=2, sitting beside the actual Fire Water Burn.
 * The list then shows two rows that are identical except for their length,
 * which reads as the application duplicating something rather than as the
 * record being mis-tagged.
 *
 * That was survivable while a row could only be played. It is not now there is
 * a delete control on each one: two indistinguishable rows, a destructive
 * button apiece, and no way to tell which is the file whose tags are right.
 *
 * The detail page already answers this for films by showing the filename, on
 * the grounds that a wrongly matched title cannot be corrected if you cannot
 * tell which file it is. Same answer, applied only where it is needed — a
 * correctly tagged record shows nothing extra.
 */
function collidingTitles(tracks: Item[]): Set<string> {
  const seen = new Set<string>();
  const twice = new Set<string>();
  for (const t of tracks) {
    const key = (t.title ?? "").toLowerCase();
    if (seen.has(key)) twice.add(key);
    else seen.add(key);
  }
  return twice;
}

function TrackRow({
  track,
  queue,
  albumArtist,
  showNumbers,
  ambiguous,
  onRemove,
}: {
  track: Item;
  queue: string;
  albumArtist: string | undefined;
  showNumbers: boolean;
  /** Another track in this list carries the same title. */
  ambiguous: boolean;
  /** Admin only; absent for everyone else, and the control is not rendered. */
  onRemove?: (track: Item) => void;
}) {
  const navigate = useNavigate();
  const play = () => navigate(`/watch/${track.id}?queue=${queue}`);
  const focusable = useFocusable(play);

  /*
   * The filename is detail-only — deliberately, since it is a fragment of the
   * server's filesystem and the grid has no use for it. So the row fetches its
   * own detail, and only when it has to: useItem(0) is disabled, so a row whose
   * title is unique never asks.
   *
   * The alternative was adding file_name to every list response, which would
   * put a base name on every poster in every grid in the application to serve a
   * case that is rare by definition. This costs one request per colliding row,
   * on the rare records that have any, cached afterwards by the same key the
   * track's own detail page would use.
   */
  const { data: detail } = useItem(ambiguous ? track.id : 0);
  const fileName = detail?.file_name;

  // A compilation has one album artist and a different performer per track —
  // which is exactly why the scanner groups on album artist (ADR 0024). Showing
  // the performer only when it differs keeps a normal album's rows quiet and
  // makes a compilation legible.
  const performer =
    track.artist && track.artist !== albumArtist ? track.artist : null;

  const played = track.progress?.watched ?? false;

  /*
   * A row is a line containing a play button and, for an admin, a remove
   * control beside it — not one button, which is what it used to be. A button
   * cannot contain a button, and the removal has to be a separate control
   * rather than a click target inside the play target, or pressing it would
   * play the track it is meant to delete.
   *
   * Same shape as the subtitle menu's rows (.submenu__line), for the same
   * reason and deliberately not a second pattern.
   */
  return (
    <div className="track-line">
    <button
      {...focusable}
      className={
        "track-row" +
        (played ? " track-row--played" : "") +
        (showNumbers ? "" : " track-row--unnumbered")
      }
      onClick={play}
      aria-label={`Play ${track.title}`}
    >
      {/* An em dash where a number should be is honest for the odd untagged
          track on an otherwise numbered record. A whole column of them, on a
          record where no file carries a number, is just noise pretending to be
          data — so the column goes instead. */}
      {showNumbers && (
        <span className="track-row__num">{track.episode || "—"}</span>
      )}
      <span className="track-row__title">
        {track.title}
        {performer && <span className="track-row__artist">{performer}</span>}
        {ambiguous && fileName && (
          <span className="track-row__file" title={fileName}>
            {fileName}
          </span>
        )}
      </span>
      <span className="track-row__time">
        {track.duration_ms ? clock(track.duration_ms / 1000) : ""}
      </span>
    </button>
      {onRemove && (
        <button
          className="track-line__remove"
          onClick={() => onRemove(track)}
          aria-label={`Remove ${track.title}`}
          title="Remove this track"
        >
          ×
        </button>
      )}
    </div>
  );
}

export function TrackList({
  tracks,
  albumArtist,
}: {
  tracks: Item[];
  albumArtist?: string;
}) {
  const queue = trackQueue(tracks);
  const isAdmin = useIsAdmin();
  /*
   * Removal was already permitted for a track — canRemove on the detail page
   * allows it, and RemoveDialog already offers the two answers a duplicate
   * needs: drop it from the library, or delete the file. It was simply
   * unreachable, because a track row's only action is play and nothing
   * navigates to a track's page. So the capability existed and could not be
   * used, which is indistinguishable from not having it.
   *
   * One dialog for the list rather than one per row: only one can be open, and
   * a RemoveDialog mounted per track would be a hundred dialogs on a long
   * record.
   */
  const [removing, setRemoving] = useState<Item | null>(null);

  // Numbering is shown when at least one track has a number. A record where no
  // file was tagged with one — a folder of downloads, typically — has no
  // ordering to display, and a column of dashes claims otherwise.
  const showNumbers = tracks.some((t) => (t.episode ?? 0) > 0);

  // Disc headings appear only on a set that has more than one. Without them a
  // two-disc release counts 1..17 and then 1..15 again, which reads as a
  // numbering fault rather than a second disc. A release with no disc tag at
  // all stores 0, so a single-disc album has exactly one distinct value and
  // shows no heading.
  const ambiguousTitles = collidingTitles(tracks);

  const discs = [...new Set(tracks.map((t) => t.season ?? 0))];
  const multiDisc = discs.length > 1;

  return (
    <div className="track-list">
      {discs.map((disc) => (
        <div className="track-list__disc" key={disc}>
          {multiDisc && (
            <div className="track-list__disc-head">
              <span className="section-label">Disc {disc || "—"}</span>
            </div>
          )}
          {tracks
            .filter((t) => (t.season ?? 0) === disc)
            .map((track, i) => (
              <TrackRow
                // Position, not id. A playlist may hold the same track twice
                // (ADR 0030), and keying on id would collapse the repeat into
                // one row — silently shortening the record the user is looking
                // at. The index is stable here because the list is an ordered
                // sequence that only changes when the whole thing is replaced.
                key={`${track.id}@${i}`}
                track={track}
                queue={queue}
                albumArtist={albumArtist}
                showNumbers={showNumbers}
                ambiguous={ambiguousTitles.has((track.title ?? "").toLowerCase())}
                onRemove={isAdmin ? setRemoving : undefined}
              />
            ))}
        </div>
      ))}

      {removing && (
        <RemoveDialog
          item={removing}
          onClose={() => setRemoving(null)}
          // Deleting invalidates "children", so the list refetches without this
          // component knowing how it is loaded. Nothing to navigate away from:
          // the record outlives the track.
          onDone={() => setRemoving(null)}
        />
      )}
    </div>
  );
}
