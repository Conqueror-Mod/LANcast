# ADR 0099 — A pre-roll plays before a film, from the start

Date: 2026-10-26 · Status: **accepted**

## Context

Plex lets an owner set a short clip — a cinema-style intro, a family logo — to
play before a film. The roadmap entry named four questions: server-wide or per
library; never on a resume; never counted as watching; and that it has to work
in the browser and in the desktop window's own player (ADR 0067).

## Decision

**A folder, server-wide, one clip at random.** An administrator sets a folder
(`preroll_dir`); every video directly in it is a candidate. Dropping a file in is
how a new clip is added. Empty is off, which is the default. Per-library lists
were offered and not chosen.

**The server decides whether one plays** (`GET /api/preroll?item=`), so a
browser, the desktop window and a third-party client follow one rule:

- a folder is set;
- the account has not turned it off for itself (`skip_preroll`, a nullable
  column on `user`, migration 81; null is off). It is the account's own switch,
  next to "skip intros";
- the item is a **film** — not an episode, a track or anything else;
- it is starting from the beginning. **Never on a resume**: a saved place in a
  film not yet finished means no clip, because a clip before minute 47 is only
  an obstacle. Play from start is a fresh start and gets one.

**It plays as a file, before the film is handed to the player.** The player
screen asks, and plays the clip in a plain video element over the screen with a
Skip button; when it ends, is skipped or cannot play, the film starts exactly as
it would have. Putting it inside the playback engine would have meant teaching
both engines — the browser element and the desktop's mpv — about a second
kind of thing to play; at the entrance, neither changes.

**Nothing is recorded.** A clip has no progress, is never watched, is not in
history, and does not count as viewing the film.

**A clip is named by an opaque key** looked up in the folder's listing, never
decoded into a path, and a link leading out of the folder is not a clip
(ADR 0094).

## Consequences

- Starting a film asks the server one small question first. With pre-roll off
  it answers at once with nothing, and the film starts as before.
- The clip plays in the web view, not in mpv: an MP4 or WebM clip plays
  everywhere. A clip in a format the engine cannot decode is skipped rather
  than shown as an error, because it stands between somebody and their film.
- Pre-roll before the next film of a collection, or after Up Next, is not done:
  it plays when somebody presses Play on a film, not between films of a run.
