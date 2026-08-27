import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import {
  useItem,
  useTrailer,
  useChildren,
  useCollectionMembers,
  useIsAdmin,
  fetchArtistQueue,
  usePlaylistEntries,
  useDeletePlaylist,
  fetchShowContinue,
  fetchShowEpisodes,
} from "@/api/hooks";
import { artworkURL } from "@/api/client";
import { useFocusable, useBackHandler } from "@/focus/FocusController";
import {
  runtime,
  rating,
  ratingLabel,
  episodeLabel,
  episodeCode,
} from "@/lib/format";
import {
  isContainer,
  childLabel,
  childCountLabel,
  isPicture,
} from "@/lib/kind";
import type { Item } from "@/api/types";
import { FixMatch } from "@/components/FixMatch";
import { RemoveDialog } from "@/components/RemoveDialog";
import { AddToPlaylist } from "@/components/AddToPlaylist";
import { ChoosePoster } from "@/components/ChoosePoster";
import { RenamePlaylist } from "@/components/RenamePlaylist";
import { PosterTile } from "@/components/PosterTile";
import { useItemActions } from "@/components/itemActions";
import { startOf } from "@/playback/queueOrder";
import { PhotoBanner } from "@/components/PhotoBanner";
import { PhotoViewer } from "@/components/PhotoViewer";
import { TrackList } from "@/components/TrackList";
import { EpisodeList } from "@/components/EpisodeList";
import { TrailerModal } from "@/components/TrailerModal";
import { useDownloads, downloadURL } from "@/lib/downloads";
import { RateItem } from "@/components/RateItem";
import type { Credit } from "@/api/types";
import "./Detail.css";

function PlayButton({
  onPlay,
  label = "Play",
}: {
  onPlay: () => void;
  label?: string;
}) {
  const focusable = useFocusable(onPlay);
  return (
    <button {...focusable} className="detail__play" onClick={onPlay}>
      <span className="detail__play-icon" aria-hidden="true">
        ▶
      </span>
      {label}
    </button>
  );
}

function BackButton({ onBack }: { onBack: () => void }) {
  const focusable = useFocusable(onBack);
  return (
    <button {...focusable} className="detail__back" onClick={onBack}>
      ← Back
    </button>
  );
}

function FixMatchButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__fix" onClick={onOpen}>
      Fix match
    </button>
  );
}

function TrailerButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__trailer-btn" onClick={onOpen}>
      <span aria-hidden="true">▶</span> Trailer
    </button>
  );
}

/*
 * Download.
 *
 * A real anchor rather than a button with a click handler, because the browser
 * already knows how to download a URL and any JavaScript version of that is a
 * worse copy — no resume, no native progress, and nothing in the browser's own
 * downloads list. `download` names the file; the server sends the same name in
 * Content-Disposition, which is what wins for a cross-origin or proxied setup.
 *
 * The receipt is written on click, not on completion. Completion is not
 * observable from here (see lib/downloads.ts), and a list that only recorded
 * what it could prove finished would record almost nothing.
 */
function DownloadButton({ item }: { item: Item }) {
  const [, record] = useDownloads();
  const filename = downloadFilename(item);
  const focusable = useFocusable(() => {
    // Enter on a focused anchor does not follow it the way a click does when
    // the focus controller is driving, so the navigation is explicit.
    record(receiptFor(item, filename));
    window.location.href = downloadURL(item.id);
  });

  return (
    <a
      {...focusable}
      className="detail__fix"
      href={downloadURL(item.id)}
      download={filename}
      onClick={() => record(receiptFor(item, filename))}
    >
      Download
    </a>
  );
}

// The name the receipt shows. The server decides the real filename — this is
// the client's best guess at the same rule, used for the `download` attribute
// and for the downloads list, so the two read alike.
function downloadFilename(item: Item): string {
  // Same trap as the tile label: a track carries its album in `series` and its
  // disc and track number in `season`/`episode`, so building this from "has
  // numbers" named a downloaded song "Album - S00E14 - Title". episodeCode is
  // the one place that knows the difference.
  const code = episodeCode(item);
  const base = code
    ? `${item.series ? item.series + " - " : ""}${code} - ${item.title}`
    : item.year
      ? `${item.title} (${item.year})`
      : item.title;
  const ext = item.container ? `.${item.container.toLowerCase()}` : "";
  return base.replace(/[/\:*?"<>|]/g, "-") + ext;
}

function receiptFor(item: Item, filename: string) {
  return {
    itemId: item.id,
    title: item.title,
    filename,
    detail: episodeLabel(item) ?? (item.year ? String(item.year) : undefined),
    bytes: item.size_bytes ?? undefined,
    at: Date.now(),
  };
}

function SecondaryButton({
  label,
  onPress,
  className = "detail__fix",
}: {
  label: string;
  onPress: () => void;
  className?: string;
}) {
  const focusable = useFocusable(onPress);
  return (
    <button {...focusable} className={className} onClick={onPress}>
      {label}
    </button>
  );
}

function RemoveButton({ onOpen }: { onOpen: () => void }) {
  const focusable = useFocusable(onOpen);
  return (
    <button {...focusable} className="detail__remove" onClick={onOpen}>
      Remove
    </button>
  );
}

/*
 * Initials for a person with no picture.
 *
 * Two letters at most: "Jamie Lee Curtis" is JC rather than JLC, because the
 * circle is sized for a face and three letters in it stop looking like a
 * monogram and start looking like a mistake.
 */
function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  const first = parts[0][0] ?? "";
  const last = parts.length > 1 ? parts[parts.length - 1][0] : "";
  return (first + last).toUpperCase();
}

function castOf(credits: Credit[] | undefined) {
  return (credits ?? []).filter((c) => c.role === "actor").slice(0, 12);
}

export function Detail() {
  const { id } = useParams();
  const itemID = Number(id);
  const navigate = useNavigate();

  const { data: item, isLoading, isError } = useItem(itemID);
  const { data: trailer } = useTrailer(itemID);

  // A container (show, season, collection, or a multi-part work) has no file to
  // play — it holds other items. Fetch those; the query stays idle for a plain
  // leaf. A collection's members come through the join table, everything else
  // through parent_id, so exactly one of these fires.
  const container = item ? isContainer(item) : false;
  const isCollection = item?.kind === "collection";
  // An album is asked for in the order it plays; everything else takes the
  // default. Without this a record arrives alphabetically (see useChildren).
  const isAlbum = item?.kind === "album";
  const { data: parentChildren } = useChildren(
    itemID,
    container && !isCollection,
    isAlbum ? "track" : undefined,
  );
  const { data: members } = useCollectionMembers(
    itemID,
    container && isCollection,
  );
  // A playlist's entries come from playlist_entry, not parent_id — and unlike
  // every other container they may repeat (ADR 0030).
  const isPlaylist = item?.kind === "playlist";
  const { data: entries } = usePlaylistEntries(itemID, isPlaylist);
  const children = isPlaylist
    ? entries
    : isCollection
      ? members
      : parentChildren;

  // ---- an artist's Play all -------------------------------------------------
  const isArtist = item?.kind === "artist";
  const albumIDs = isArtist ? (children ?? []).map((c) => c.id) : [];
  const qc = useQueryClient();
  const [queueing, setQueueing] = useState(false);
  const playArtist = useCallback(async () => {
    if (queueing) return;
    setQueueing(true);
    try {
      const queue = await fetchArtistQueue(qc, albumIDs);
      if (queue.length > 0) {
        navigate(`/watch/${queue[0]}?queue=${queue.join(",")}`);
      }
    } finally {
      // Cleared even on failure: a button stuck reading "Gathering…" forever is
      // a worse answer than one that simply did not work and can be pressed
      // again.
      setQueueing(false);
    }
    // albumIDs is rebuilt each render; its contents are what matter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [qc, albumIDs.join(","), navigate, queueing]);

  const [fixOpen, setFixOpen] = useState(false);
  // The picture currently in the banner, and the one the viewer opened at.
  // Selecting is not navigating: the banner is the detail view for a photo.
  const [shown, setShown] = useState<Item | null>(null);
  const [viewerOpen, setViewerOpen] = useState(false);
  const [trailerOpen, setTrailerOpen] = useState(false);
  const [removeOpen, setRemoveOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  // Deleting a playlist asks once, in the button itself. A dialog would be the
  // RemoveDialog's shape and RemoveDialog is about files; a native confirm() is
  // never an option in this application (it steals focus from the web contents
  // in a frameless window and does not reliably give it back).
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [renaming, setRenaming] = useState(false);

  /*
   * A show's action state, declared here with the other hooks rather than beside
   * the handlers that use it.
   *
   * These were originally written next to the show buttons, which sits *below*
   * the `if (isLoading) return` above — so the first render registered two fewer
   * hooks than the second, React refused to reconcile, and the whole screen
   * unmounted into a blank window with no way back. It only showed on shows,
   * because a film opened from the grid is usually already cached and never has
   * a loading render to differ from.
   */
  const [showBusy, setShowBusy] = useState<null | "continue" | "play" | "random">(
    null,
  );
  const [showNote, setShowNote] = useState<string | null>(null);
  const deletePlaylist = useDeletePlaylist(itemID);
  const isAdmin = useIsAdmin();
  const [posterOpen, setPosterOpen] = useState(false);
  /*
   * The children grid gets the same menu a library grid does. The gallery
   * branch above deliberately does not: a photograph is not watched, has no
   * page worth visiting, and selects into the banner rather than navigating.
   */
  const { actions: childActions, dialogs: childDialogs } = useItemActions();
  const back = useCallback(() => navigate(-1), [navigate]);
  useBackHandler(back);

  if (isLoading) return <div className="detail detail--empty" />;
  if (isError || !item) {
    return (
      <div className="detail detail--empty">
        <p className="detail__message">This item could not be loaded.</p>
        <BackButton onBack={back} />
      </div>
    );
  }

  /*
   * Theme-music hook point. design.md specifies an ~800ms-delayed, fading theme
   * on the detail page, but the theme-music subsystem is not built yet (blocked
   * on OST identification; ADR 0005). When it lands, it starts here — and must
   * degrade silently when no theme is available. Nothing to play for now.
   */

  const fanart = artworkURL(item.artwork?.fanart, "fanart");
  const poster = artworkURL(item.artwork?.poster, "poster2x");
  const cast = castOf(item.credits);
  // Fix match corrects a provider's identification. A season and a collection
  // have none to correct — one is structural, the other provider-derived — and
  // neither does music: ADR 0024 ships no music provider, so the only thing to
  // search would be TMDB, which would answer a record with films. An artist and
  // an album are assembled from the tags of the files beneath them.
  // Pictures are the same argument again, one step further: ADR 0028 ships no
  // picture provider and never will — there is nothing to identify a family
  // photograph against. A gallery is a folder the scanner read.
  const isMusic =
    item.kind === "artist" || item.kind === "album" || item.kind === "track";
  const isGallery = item.kind === "gallery";
  const canFixMatch =
    item.kind !== "collection" &&
    item.kind !== "season" &&
    !isMusic &&
    !isPicture(item) &&
    // A playlist has no provider identity to correct. It is a list somebody
    // named, and the only thing a search could do with "Road Trip" is offer the
    // film — the same reason a collection and a season are excluded, arrived at
    // one media type later.
    !isPlaylist;
  // Removing something removes files. An artist and an album have none — they
  // are rows the scanner invented and sweeps when they empty — so, like a
  // collection, they are not offered. A track is a real file and keeps it.
  const canRemove =
    item.kind !== "collection" &&
    item.kind !== "artist" &&
    item.kind !== "album" &&
    // A gallery is a folder the scanner invented and sweeps when it empties,
    // exactly like an artist. A photo is a real file and keeps the option.
    item.kind !== "gallery" &&
    // A playlist gets its own delete, below. This dialog is about files: for a
    // playlist imported from disk, "delete from disk" would remove the .m3u and
    // "remove from library" would add it to the ignore list — two filesystem
    // side effects for what a person meant as "I don't want this list".
    !isPlaylist;
  // Children that can be played directly (not themselves containers), in order —
  // the queue behind Play all. A show's children are seasons, so it gets none;
  // a season's episodes, a work's parts, and a collection's films all qualify.
  // Pictures are not playable. Without this a gallery offers "Play all" over
  // its photos and hands the player a JPEG — the kind of nonsense that only
  // appears when a new media type arrives and every screen still assumes a leaf
  // is something you press play on. The viewer replaces this in the next phase
  // (ADR 0028).
  const playableChildren = (children ?? []).filter(
    (c) => !isContainer(c) && !isPicture(c),
  );

  /*
   * A show's three actions.
   *
   * A show's children are seasons, so the rule above finds nothing playable and
   * offered nothing at all — you had to drill into a season. But "put this
   * programme on" is the most ordinary thing anybody asks of a show, and it
   * splits into three different questions: start it, carry on with it, or play
   * something at random from it.
   *
   * Every one of them asks the server at the moment it is pressed. Nothing here
   * is cached, because the bug being designed against is a stale read: continue
   * landing on an episode already watched.
   */
  const isShow = item?.kind === "show";
  /*
   * Whether the children are episodes, whatever their container calls itself.
   *
   * Keyed on what the children *are* rather than on the parent being a season:
   * a show whose episodes hang directly off it (the loose shape `shapecheck`
   * allows) gets the same list, and a season that somehow holds something else
   * does not.
   */
  const isEpisodeList =
    (children ?? []).length > 0 &&
    (children ?? []).every((c) => c.kind === "episode");

  const continueShow = async () => {
    if (!item || showBusy) return;
    setShowBusy("continue");
    setShowNote(null);
    try {
      const next = await fetchShowContinue(item.id);
      if (next.exhausted) {
        // Said rather than silently replayed. Finishing a show is an outcome,
        // and a button that quietly restarts the finale is one nobody trusts
        // afterwards.
        setShowNote("You have watched every episode. Use Play to start again.");
        return;
      }
      if (!next.episode) {
        setShowNote("This show has no episodes yet.");
        return;
      }
      /*
       * Continue hands over the rest of the show, not one episode.
       *
       * It used to navigate with no queue at all, and the player falls back to
       * a queue of the single item it was given — so the episode you resumed
       * played and the show stopped dead, because there was nothing after it to
       * advance to. With repeat on it was worse: a one-item queue wraps onto
       * itself, and the same episode replayed for ever. That is how it was
       * found.
       *
       * The episodes from this one onward rather than all of them, because
       * Continue means "carry on from here" — putting the earlier ones in the
       * queue would make the *previous* button walk back through episodes the
       * viewer has already finished, which Play from the top is for.
       *
       * Same shape as playShow, deliberately: history state rather than the
       * URL, since a long-running show is far too many ids for a query string.
       */
      const eps = await fetchShowEpisodes(item.id);
      const from = eps.findIndex((e) => e.id === next.episode!.id);
      const queue = from >= 0 ? eps.slice(from) : [next.episode];
      navigate(`/watch/${next.episode.id}`, {
        state: { queue: queue.map((e) => e.id) },
      });
    } finally {
      setShowBusy(null);
    }
  };

  const playShow = async (shuffle: boolean) => {
    if (!item || showBusy) return;
    setShowBusy(shuffle ? "random" : "play");
    setShowNote(null);
    try {
      const eps = await fetchShowEpisodes(item.id);
      if (eps.length === 0) {
        setShowNote("This show has no episodes yet.");
        return;
      }
      // The queue goes in history state rather than the URL: a long-running
      // show is far too many ids for a query string, and the player already
      // takes it this way from a library's Play all.
      const ids = eps.map((e) => e.id);
      // Not ids[0] when shuffling — shuffledStartingWith pins the id it is
      // given, so a fixed start shuffled every episode except the first. See
      // startOf.
      const start = startOf(ids, shuffle);
      if (start === undefined) return;
      navigate(`/watch/${start}`, { state: { queue: ids, shuffle } });
    } finally {
      setShowBusy(null);
    }
  };

  const meta = [
    // An album's artist belongs on the line that says what this is, ahead of
    // the year — "Between the Buried and Me · 2021", the way a record is named.
    isAlbum ? (item.artist ?? "") : "",
    item.year ? String(item.year) : "",
    runtime(item.duration_ms),
    item.content_rating ?? "",
    rating(item.rating) && `★ ${rating(item.rating)}`,
    // What a container holds. Every other kind has something factual on this
    // line; an artist had nothing at all — no year, no runtime, no rating, no
    // certificate — so the page opened with a title and a button and asserted
    // nothing about the thing you were looking at. "12 albums" is small, but it
    // is the difference between a page and a heading.
    //
    // Counted from the children rather than from child_count so it can never
    // disagree with the list directly beneath it, and only once they have
    // arrived — a flash of "0 albums" is worse than a line that appears a beat
    // late.
    container && children && children.length > 0
      ? childCountLabel(children.length, children[0].kind)
      : "",
  ].filter(Boolean);

  return (
    <div className="detail">
      {fanart && (
        <div
          className="detail__fanart"
          style={{ backgroundImage: `url(${fanart})` }}
          aria-hidden="true"
        />
      )}
      <div className="detail__scrim" aria-hidden="true" />

      <div className="detail__body">
        <BackButton onBack={back} />

        <div className="detail__hero">
          {/*
            A collection's poster is a control; every other item's is a picture.
            
            Deliberately *outside* the `poster &&` guard, which is where it was
            and which made the control unreachable exactly when it was most
            needed: a collection with no image renders no poster, so the branch
            holding the button never ran, and the only collection anybody wanted
            to fix was the one that could not be. A collection whose films have
            no posters either still gets the button, and the picker says there
            is nothing to choose -- which is an answer, where a missing control
            is a mystery.
            
            Admin only, because there is one poster and everybody sees it. The
            poster is a button only where it does something: one that looks
            pressable and is not is worse than one that never invited the press.
          */}
          {isCollection && isAdmin ? (
            <button
              type="button"
              className="detail__poster detail__poster--editable"
              onClick={() => setPosterOpen(true)}
              /*
               * No `title`. It said the same thing as the visible label a few
               * lines down, so both appeared at once and the browser's own
               * tooltip painted over the poster's lower edge -- the same shape
               * as the v0.8.11 bug where a tile's tooltip covered the menu it
               * had just opened. A native tooltip is drawn above everything the
               * page can produce, so a control that already labels itself
               * should not ask for a second one.
               *
               * aria-label carries the fuller sentence instead: it is not
               * drawn, and "Change poster" alone is thinner than a screen
               * reader deserves.
               */
              aria-label={`Change the poster for ${item.title} to one of its films`}
            >
              {poster ? (
                <img src={poster} alt="" draggable={false} />
              ) : (
                <span className="detail__poster-empty">No poster</span>
              )}
              <span className="detail__poster-edit">Change poster</span>
            </button>
          ) : (
            poster && (
              <img
                className={
                  "detail__poster" + (isMusic ? " detail__poster--square" : "")
                }
                src={poster}
                alt=""
                draggable={false}
              />
            )
          )}

          <div className="detail__info">
            <h1 className="detail__title">{item.title}</h1>

            {meta.length > 0 && (
              <div className="detail__meta">
                {meta.map((m, i) => (
                  <span key={i}>{m}</span>
                ))}
              </div>
            )}

            {item.genres && item.genres.length > 0 && (
              <div className="detail__genres">{item.genres.join(" · ")}</div>
            )}

            {/* The file this row came from. Shown because a wrongly matched
                title is impossible to correct if you cannot tell which file it
                is — the numbered pieces of an anthology look identical
                otherwise. */}
            {item.file_name && (
              <div className="detail__file" title={item.file_name}>
                {item.file_name}
              </div>
            )}

            {item.ratings && item.ratings.length > 0 && (
              <div className="detail__ratings">
                {item.ratings.map((r) => (
                  <span className="detail__rating" key={r.source}>
                    <span className="detail__rating-src">
                      {ratingLabel(r.source)}
                    </span>
                    <span className="detail__rating-val">{r.display}</span>
                  </span>
                ))}
              </div>
            )}

            {/* Metadata correction lives with the metadata, not the playback
                controls — that was the point of moving it here. A collection
                and a season have no user-correctable identity of their own
                (one is provider-derived, the other structural), so Fix match
                does not apply to them. */}
            {canFixMatch && (
              <div className="detail__metafix">
                {item.metadata_updated_at != null &&
                  (item.match_state === "review" ||
                    item.match_state === "unmatched") && (
                    <span className="detail__matchbadge">
                      {item.match_state === "review"
                        ? "Needs review"
                        : "No match"}
                    </span>
                  )}
                <FixMatchButton onOpen={() => setFixOpen(true)} />
              </div>
            )}

            {/* A show: start it, carry on with it, or play it at random.
                Continue leads, because on a show you have started it is the
                only one of the three anybody presses. */}
            {isShow && (
              <div className="detail__actions">
                <PlayButton
                  label={showBusy === "continue" ? "Finding…" : "Continue watching"}
                  onPlay={() => void continueShow()}
                />
                <button
                  className="detail__play detail__play--secondary"
                  onClick={() => void playShow(false)}
                  disabled={showBusy !== null}
                >
                  {showBusy === "play" ? "Gathering…" : "Play from start"}
                </button>
                <button
                  className="detail__play detail__play--secondary"
                  onClick={() => void playShow(true)}
                  disabled={showBusy !== null}
                >
                  {showBusy === "random" ? "Gathering…" : "Randomize episodes"}
                </button>
              </div>
            )}
            {showNote && (
              <p className="detail__note" role="status">
                {showNote}
              </p>
            )}

            {/* A leaf plays itself. A container whose children are themselves
                playable (a work's parts, a serial's chapters, a season's
                episodes, a collection's films) offers Play all, which queues
                them in order. A show, whose children are seasons, gets neither —
                you drill into a season first. */}
            <div className="detail__actions">
              {!container && !isPicture(item) && (
                <PlayButton onPlay={() => navigate(`/watch/${item.id}`)} />
              )}
              {/*
                * A season leads with Continue, matching the show page so the
                * two do not disagree about what a season offers. It asks the
                * same endpoint: the query matches episodes by their parent, so
                * a season id answers with that season's next episode rather
                * than the show's.
                */}
              {isEpisodeList && (
                <PlayButton
                  label={showBusy === "continue" ? "Finding…" : "Continue"}
                  onPlay={() => void continueShow()}
                />
              )}
              {container && playableChildren.length > 0 && (
                <SecondaryButton
                  label="Play all"
                  className={isEpisodeList ? "detail__play detail__play--secondary" : "detail__play"}
                  onPress={() =>
                    navigate(
                      `/watch/${playableChildren[0].id}?queue=${playableChildren
                        .map((c) => c.id)
                        .join(",")}`,
                    )
                  }
                />
              )}
              {/* An artist's children are albums, so the rule above finds
                  nothing playable and offers nothing — the same reason a show
                  offers nothing over its seasons. But a discography is a
                  perfectly ordinary thing to put on, where "every episode of
                  this programme" is not, so the artist gets the two-level
                  version: every track of every album, records in the order
                  shown and tracks in the order they play. */}
              {isArtist && albumIDs.length > 0 && (
                <PlayButton
                  label={queueing ? "Gathering…" : "Play all"}
                  onPlay={playArtist}
                />
              )}
              {trailer && <TrailerButton onOpen={() => setTrailerOpen(true)} />}
              {/* Anything with a file of its own can be taken away. A container
                  has no file — "download this season" would have to mean a zip
                  the server does not build — and a missing item's file is not
                  there to hand over. */}
              {!container && !item.missing && (
                <DownloadButton item={item} />
              )}
              {/* Anything that plays on its own can go in a playlist — a
                  track, a film, an episode. A container cannot: a playlist
                  holds entries, not albums, so "add this record" would have to
                  silently mean "add its twelve tracks", which is a different
                  request and not one this button asked. Pictures are not
                  playable at all. */}
              {!container && !isPicture(item) && (
                <SecondaryButton
                  label="Add to playlist"
                  onPress={() => setAddOpen(true)}
                />
              )}
              {isPlaylist && (
                <SecondaryButton
                  label="Rename"
                  onPress={() => setRenaming(true)}
                />
              )}
              {isPlaylist && (
                <SecondaryButton
                  className="detail__remove"
                  label={
                    deletePlaylist.isPending
                      ? "Deleting…"
                      : confirmDelete
                        ? "Delete this playlist?"
                        : "Delete playlist"
                  }
                  onPress={() => {
                    if (!confirmDelete) {
                      setConfirmDelete(true);
                      return;
                    }
                    deletePlaylist.mutate(undefined, { onSuccess: back });
                  }}
                />
              )}
              {isAdmin && canRemove && (
                <RemoveButton onOpen={() => setRemoveOpen(true)} />
              )}
            </div>

            {/* Rating sits under the actions and above the synopsis: it is
                something you do *after* watching, so it belongs below the play
                controls rather than competing with them — and above the
                synopsis, because a note you wrote is worth more to you than a
                summary you have already read. Containers and photos are not
                things anybody rates. */}
            {!container && !isPicture(item) && <RateItem itemID={item.id} />}

            {item.overview && (
              <p className="detail__overview">{item.overview}</p>
            )}

            {cast.length > 0 && (
              <div className="detail__cast">
                <span className="section-label">Cast</span>
                <div className="detail__cast-row">
                  {cast.map((c, i) => (
                    <div className="detail__cast-member" key={i}>
                      {/*
                        A face when there is one, and the initials when there is
                        not — which is most of the time further down a cast
                        list, so the fallback is the design rather than an
                        afterthought. A row of twelve where three are pictures
                        and nine are gaps reads as broken; one where every
                        entry is the same shape reads as a cast list.
                      */}
                      {c.thumb ? (
                        <img
                          className="detail__cast-face"
                          src={artworkURL(c.thumb, "thumb")}
                          alt=""
                          loading="lazy"
                        />
                      ) : (
                        <span className="detail__cast-face detail__cast-face--none">
                          {initialsOf(c.name)}
                        </span>
                      )}
                      <span className="detail__cast-name">{c.name}</span>
                      {c.character && (
                        <span className="detail__cast-character">
                          {c.character}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>

        {isGallery && children && children.length > 0 && (
          <PhotoBanner
            photos={children}
            selected={shown}
            label={item.title}
            onExpand={(p) => {
              setShown(p);
              setViewerOpen(true);
            }}
          />
        )}

        {container && children && children.length > 0 && (
          <section className="detail__children">
            <span className="section-label">
              {childLabel(children[0]?.kind)}
            </span>
            {/* A record is a numbered list, not a grid: twelve copies of one
                cover say nothing, and a track is identified by its number and
                its length. The album's `artist` is the album artist, so a
                per-track performer shows only where it differs — the
                compilation case (ADR 0024). */}
            {isEpisodeList ? (
              /*
               * A season is a list, not a grid.
               *
               * The same correction this file already makes for an album one
               * branch down: a leaf that is not a poster should not be drawn
               * with the poster grid. Episodes were 2:3 tiles with no room for
               * a synopsis, which is the one thing a season page is for
               * (season-page-plan.md).
               */
              <EpisodeList
                episodes={playableChildren}
                queue={playableChildren.map((c) => c.id)}
                parentID={item.id}
              />
            ) : isAlbum || isPlaylist ? (
              // A playlist is a numbered list for the same reason a record is,
              // and more so: its tracks come from everywhere, so the performer
              // shows on every row rather than only where it differs from an
              // album artist there is no such thing as here.
              <TrackList
                tracks={children}
                albumArtist={isPlaylist ? undefined : (item.artist ?? undefined)}
                // Turns on the row edits: reorder, and remove from the list.
                playlistID={isPlaylist ? item.id : undefined}
              />
            ) : isGallery ? (
              <div className="detail__children-grid">
                {children.map((child) => (
                  <PosterTile
                    key={child.id}
                    item={child}
                    onOpen={() => {
                      setShown(child);
                      // Scroll the banner back into view: on a gallery of 779
                      // photographs the tile pressed is usually far below it,
                      // and replacing a banner nobody can see is the same as
                      // doing nothing.
                      window.scrollTo({ top: 0, behavior: "smooth" });
                    }}
                  />
                ))}
              </div>
            ) : (
              <div className="detail__children-grid">
                {children.map((child) => (
                  <PosterTile
                    key={child.id}
                    // A part or chapter often has no artwork of its own (a
                    // miniseries's pieces are not separately matched), which
                    // would leave a blank tile. Fall back to the parent's poster
                    // so the grid reads as one work rather than empty cards.
                    item={
                      child.artwork?.poster
                        ? child
                        : { ...child, artwork: item.artwork }
                    }
                    actions={childActions}
                  />
                ))}
              </div>
            )}
          </section>
        )}
      </div>

      {viewerOpen && children && (
        <PhotoViewer
          photos={children}
          startAt={Math.max(
            0,
            children.findIndex((c) => c.id === shown?.id),
          )}
          onClose={() => setViewerOpen(false)}
        />
      )}
      {fixOpen && <FixMatch item={item} onClose={() => setFixOpen(false)} />}
      {removeOpen && (
        <RemoveDialog
          item={item}
          onClose={() => setRemoveOpen(false)}
          onDone={() => {
            setRemoveOpen(false);
            back();
          }}
        />
      )}
      {addOpen && (
        <AddToPlaylist item={item} onClose={() => setAddOpen(false)} />
      )}
      {renaming && (
        <RenamePlaylist playlist={item} onClose={() => setRenaming(false)} />
      )}
      {/* The children grid's own dialogs. Separate from this page's Remove and
          Add-to-playlist above: those act on the item being *looked at*, these
          act on whichever child was right-clicked, and one pair of state
          variables serving both would remove the wrong thing. */}
      {posterOpen && (
        <ChoosePoster
          collection={item}
          members={members ?? []}
          onClose={() => setPosterOpen(false)}
        />
      )}
      {childDialogs}
      {trailerOpen && trailer && (
        <TrailerModal
          trailer={trailer}
          title={item.title}
          onClose={() => setTrailerOpen(false)}
        />
      )}
    </div>
  );
}
