import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useFocusable } from "@/focus/FocusController";
import {
  useIsAdmin,
  useItem,
  useRemovePlaylistEntry,
  useSetPlaylistEntries,
  useSetWatchedByID,
} from "@/api/hooks";
import { clock } from "@/lib/format";
import type { Item } from "@/api/types";
import { RemoveDialog } from "./RemoveDialog";
import { AddToPlaylist } from "./AddToPlaylist";
import { PointMenu, type MenuAction, type MenuPoint } from "./Menu";
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

/**
 * The edits a playlist row offers. Absent everywhere else, which is what keeps
 * an album's rows exactly as they were.
 *
 * `number` overrides the track number with the playlist position: a playlist's
 * tracks come from everywhere, so their own track numbers are the numbers they
 * had on their records — 1, 4, 1, 9 down a list of eleven. The position is the
 * only numbering that describes *this* list.
 */
type PlaylistRowEdits = {
  number: number;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  onRemoveEntry: () => void;
  busy: boolean;
};

function TrackRow({
  track,
  queue,
  albumArtist,
  showNumbers,
  ambiguous,
  onRemove,
  onAddToPlaylist,
  edits,
}: {
  track: Item;
  queue: string;
  albumArtist: string | undefined;
  showNumbers: boolean;
  /** Another track in this list carries the same title. */
  ambiguous: boolean;
  /** Admin only; absent for everyone else, and the control is not rendered. */
  onRemove?: (track: Item) => void;
  /** Opens the playlist picker for this track. */
  onAddToPlaylist: (track: Item) => void;
  /** Playlist rows only. */
  edits?: PlaylistRowEdits;
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
  const setPlayed = useSetWatchedByID();
  const [menuAt, setMenuAt] = useState<MenuPoint | null>(null);

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
  /*
   * The row's right-click menu, which only ever *adds*.
   *
   * Every control on this row stays exactly where it is. The reordering arrows
   * are two buttons rather than a drag handle because this list is driven by a
   * remote as well as a mouse (see the comment on them), and there is no
   * keyboard route into a context menu — so moving anything in here would take
   * it away from the d-pad entirely. This file already carries two comments
   * about capabilities that shipped unreachable; a third would be a pattern
   * rather than an accident.
   *
   * What it genuinely adds is the played flag. A track had no way to be marked
   * heard anywhere in the client: there is no poster tile for one and nothing
   * navigates to its page, so the state existed and only playback could set it.
   *
   * The rest mirror controls already on the row. That is worth the duplication
   * — right-click is where people look, and a menu that offered one item on a
   * row carrying four buttons would read as broken rather than restrained.
   */
  const actions: MenuAction[] = [
    { label: "Play", onSelect: play },
    {
      label: played ? "Mark as unplayed" : "Mark as played",
      onSelect: () => setPlayed.mutate({ itemID: track.id, watched: !played }),
    },
    { label: "Add to playlist", onSelect: () => onAddToPlaylist(track) },
    ...(edits
      ? [
          {
            label: "Remove from this playlist",
            disabled: edits.busy,
            onSelect: () => edits.onRemoveEntry(),
          },
        ]
      : []),
    ...(onRemove
      ? [
          {
            label: "Remove from library…",
            danger: true,
            onSelect: () => onRemove(track),
          },
        ]
      : []),
  ];

  return (
    <div
      className="track-line"
      onContextMenu={(e) => {
        e.preventDefault();
        setMenuAt({ x: e.clientX, y: e.clientY });
      }}
    >
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
        <span className="track-row__num">
          {edits ? edits.number : track.episode || "—"}
        </span>
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
      {/*
        * Add to playlist lives on the row because a track's detail page is
        * unreachable: nothing in the client navigates to /item/{id} for a
        * track, since tracks are rows here and never poster tiles. The control
        * shipped on that page in v0.6.10 and could not be used by anyone with a
        * music library — the same failure the remove control had, and it is
        * recorded three comments up. A capability you cannot reach is not a
        * capability.
        */}
      <button
        className="track-line__act"
        onClick={() => onAddToPlaylist(track)}
        aria-label={`Add ${track.title} to a playlist`}
        title="Add to playlist"
      >
        +
      </button>
      {/* Reordering is two buttons rather than a drag handle, on purpose: this
          list is used with a remote as well as a mouse, and a drag is not
          something a d-pad can do. The ends are disabled rather than hidden so
          the controls do not move around under the pointer as you work down a
          list. */}
      {edits && (
        <>
          <button
            className="track-line__act"
            onClick={edits.onMoveUp}
            disabled={!edits.onMoveUp || edits.busy}
            aria-label={`Move ${track.title} up`}
            title="Move up"
          >
            ↑
          </button>
          <button
            className="track-line__act"
            onClick={edits.onMoveDown}
            disabled={!edits.onMoveDown || edits.busy}
            aria-label={`Move ${track.title} down`}
            title="Move down"
          >
            ↓
          </button>
          {/* No confirmation. This removes an entry from a list, not a file
              from a disk — the destructive control that needs a dialog is the
              admin one below, and conflating them is how someone deletes media
              while tidying a playlist. */}
          <button
            className="track-line__act"
            onClick={edits.onRemoveEntry}
            disabled={edits.busy}
            aria-label={`Remove ${track.title} from this playlist`}
            title="Remove from playlist"
          >
            ×
          </button>
        </>
      )}
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
      {menuAt && (
        <PointMenu
          at={menuAt}
          actions={actions}
          onClose={() => setMenuAt(null)}
        />
      )}
    </div>
  );
}

export function TrackList({
  tracks,
  albumArtist,
  playlistID,
}: {
  tracks: Item[];
  albumArtist?: string;
  /**
   * Set when this list *is* a playlist, which turns on the row edits. Absent
   * for an album, whose order is the record's and not the viewer's to change.
   */
  playlistID?: number;
}) {
  const queue = trackQueue(tracks);
  const isAdmin = useIsAdmin();
  const editing = (playlistID ?? 0) > 0;
  const reorder = useSetPlaylistEntries(playlistID ?? 0);
  const removeEntry = useRemovePlaylistEntry(playlistID ?? 0);
  const busy = reorder.isPending || removeEntry.isPending;

  /*
   * A move sends the whole sequence, because that is what the endpoint takes —
   * a playlist is an ordered list and the client already knows what the order
   * should be. Built from `tracks` by index, so a repeated track moves the copy
   * that was pressed rather than the first one with that id.
   */
  const move = (from: number, to: number) => {
    const ids = tracks.map((t) => t.id);
    const [moved] = ids.splice(from, 1);
    ids.splice(to, 0, moved);
    reorder.mutate(ids);
  };
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
  // One picker for the list, for the same reason there is one RemoveDialog:
  // only one can be open, and a dialog per row is a hundred dialogs on a long
  // record.
  const [adding, setAdding] = useState<Item | null>(null);

  // Numbering is shown when at least one track has a number. A record where no
  // file was tagged with one — a folder of downloads, typically — has no
  // ordering to display, and a column of dashes claims otherwise.
  // A playlist is always numbered, by position — see PlaylistRowEdits.
  const showNumbers = editing || tracks.some((t) => (t.episode ?? 0) > 0);

  // Disc headings appear only on a set that has more than one. Without them a
  // two-disc release counts 1..17 and then 1..15 again, which reads as a
  // numbering fault rather than a second disc. A release with no disc tag at
  // all stores 0, so a single-disc album has exactly one distinct value and
  // shows no heading.
  const ambiguousTitles = collidingTitles(tracks);

  // A playlist is one sequence, never grouped by disc: its tracks carry the
  // disc numbers of the records they came from, so grouping on them would cut a
  // playlist into "Disc 1 / Disc 2" sections that mean nothing about this list
  // and, worse, would reorder it on screen.
  const discs = editing ? [0] : [...new Set(tracks.map((t) => t.season ?? 0))];
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
          {(editing ? tracks : tracks.filter((t) => (t.season ?? 0) === disc))
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
                // A repeat in a playlist is deliberate, so the filename hint is
                // suppressed there. It exists for the mis-tagged album — two
                // rows that differ only by length, where the file is the only
                // way to tell which is which. A playlist that opens and closes
                // with the same song shows two identical rows *on purpose*, and
                // annotating them with a path is noise on the one case where
                // identical rows are correct.
                ambiguous={
                  !editing &&
                  ambiguousTitles.has((track.title ?? "").toLowerCase())
                }
                // In a playlist the row's × removes the entry, and the
                // library-level delete is deliberately not also on the row:
                // two × controls a few pixels apart, one removing a line and
                // one deleting a file, is a mistake waiting for a tired
                // evening. Deleting the file is still on the track's own page.
                onRemove={isAdmin && !editing ? setRemoving : undefined}
                onAddToPlaylist={setAdding}
                edits={
                  editing
                    ? {
                        number: i + 1,
                        onMoveUp: i > 0 ? () => move(i, i - 1) : undefined,
                        onMoveDown:
                          i < tracks.length - 1
                            ? () => move(i, i + 1)
                            : undefined,
                        // Position, which is how the endpoint addresses an
                        // entry — an id cannot, since the same track may be in
                        // the list twice.
                        onRemoveEntry: () => removeEntry.mutate(i),
                        busy,
                      }
                    : undefined
                }
              />
            ))}
        </div>
      ))}

      {adding && (
        <AddToPlaylist item={adding} onClose={() => setAdding(null)} />
      )}

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
