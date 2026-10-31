# ADR 0108 — Music is levelled by its own ReplayGain

Date: 2026-10-28 · Status: **accepted** · Builds on [ADR 0024](0024-music-libraries.md)

## Context

A shuffled library jumps in level from one track to the next: a record
mastered in 1975 next to one mastered in 2010 can be 10 dB apart, and the
person reaches for the volume every few songs. The audio pass gave music
night mode and an equaliser; levelling across a library had no plan.

Most people who care about this already have the measurement. foobar2000,
Picard, beets, Mp3tag and every serious ripper write ReplayGain tags:
`REPLAYGAIN_TRACK_GAIN`, `REPLAYGAIN_ALBUM_GAIN` and their peaks. Opus files
carry `R128_TRACK_GAIN` and `R128_ALBUM_GAIN` instead.

## Decision

**Read the tags; do not measure.** The scan's tag pass already opens every
music file through ffprobe, and reads the four ReplayGain values (and the
R128 pair) at no extra cost. Measuring loudness ourselves would mean decoding
every track in full, an hour or more of CPU on a large library, to produce a
number most taggers have already written. A track without the tags is left
alone. A measuring worker can come later if untagged libraries turn out to
be common.

**Stored as four nullable columns** on `media_item` (`rg_track_gain`,
`rg_track_peak`, `rg_album_gain`, `rg_album_peak`, schema 86). Null means not
tagged, never 0 dB. They're a measurement of the file, written as read on
every tag pass, so a gain removed from the file is removed here. No lock
applies; nobody edits a measurement.

**Reported, not applied, by the server.** `GET /api/items/{id}` carries a
`loudness` block on a tagged track. The bytes served are the file's own,
because the music deck can only join tracks gaplessly when it plays them
directly.

**Applied by the player, per device.** A playback setting, **Even out
loudness**, offers Off, By track and By album, and is **off by default**. By
album keeps an album's own dynamics; by track suits a shuffled mix. Each
falls back to the other gain when its own is missing. The factor is
`10^(gain/20)`, capped by `1/peak` so nothing clips, and capped at 1.

**Only ever down.** A media element's volume stops at 1, so a track quieter
than the reference plays as it is, and levelling brings loud records down to
meet it. Raising quiet ones would need a gain stage in Web Audio on every
music path. Night mode already routes music that way, and making levelling
depend on it would make the simple case depend on the complicated one.

**Each deck element carries its own gain.** During a gapless join or a
crossfade, two tracks sound at once, so the deck multiplies each element's
volume by that track's gain, and the next track's gain is handed over with
its URL when it is queued. The person's slider is untouched: the deck's
`volume` is still the slider, and the gain is applied beneath it.

**Backfill by dropping the grouping keys.** An unchanged library never
reopens a file (ADR 0056), so the tags of every existing track would never be
read. Revision 86 clears the stored grouping keys of every track, and the tag
pass now runs whenever any present track has no key, even if nothing changed
on disk. Each music library pays one full tag pass at its next scan (about
a minute and a half for 9,000 tracks), then the pass is free again.

## Consequences

- Levelling works for anybody whose library is tagged, from the next scan,
  with no decoding on the server.
- Switched on, most modern music gets quieter, by 6 to 10 dB. That is why it
  is off by default and says so in Settings.
- An untagged track amid levelled ones is relatively loud. A pre-amp for
  untagged tracks, or a measuring pass, would fix that, and neither is built.
- The desktop's mpv player, used for films, doesn't take part. Music plays
  in the media element on every client.
- A paired server's tracks are not levelled yet: the federation item shape
  doesn't carry the block.
