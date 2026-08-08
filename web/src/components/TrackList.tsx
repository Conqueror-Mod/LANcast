import { useNavigate } from "react-router-dom";
import { useFocusable } from "@/focus/FocusController";
import { clock } from "@/lib/format";
import type { Item } from "@/api/types";
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

function TrackRow({
  track,
  queue,
  albumArtist,
  showNumbers,
}: {
  track: Item;
  queue: string;
  albumArtist: string | undefined;
  showNumbers: boolean;
}) {
  const navigate = useNavigate();
  const play = () => navigate(`/watch/${track.id}?queue=${queue}`);
  const focusable = useFocusable(play);

  // A compilation has one album artist and a different performer per track —
  // which is exactly why the scanner groups on album artist (ADR 0024). Showing
  // the performer only when it differs keeps a normal album's rows quiet and
  // makes a compilation legible.
  const performer =
    track.artist && track.artist !== albumArtist ? track.artist : null;

  const played = track.progress?.watched ?? false;

  return (
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
      </span>
      <span className="track-row__time">
        {track.duration_ms ? clock(track.duration_ms / 1000) : ""}
      </span>
    </button>
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

  // Numbering is shown when at least one track has a number. A record where no
  // file was tagged with one — a folder of downloads, typically — has no
  // ordering to display, and a column of dashes claims otherwise.
  const showNumbers = tracks.some((t) => (t.episode ?? 0) > 0);

  // Disc headings appear only on a set that has more than one. Without them a
  // two-disc release counts 1..17 and then 1..15 again, which reads as a
  // numbering fault rather than a second disc. A release with no disc tag at
  // all stores 0, so a single-disc album has exactly one distinct value and
  // shows no heading.
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
            .map((track) => (
              <TrackRow
                key={track.id}
                track={track}
                queue={queue}
                albumArtist={albumArtist}
                showNumbers={showNumbers}
              />
            ))}
        </div>
      ))}
    </div>
  );
}
