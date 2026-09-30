# ADR 0074 — A viewing is a row

Date: 2026-09-30 · Status: **accepted** 2026-09-30, and built with schema
revision 53.

Answers the roadmap's open question *"a real history, Trakt- or
Sonarr-shaped"*. Leaves [ADR 0006](0006-playback-state-keyed-by-user.md)
standing: `playback_state` stays the state table it has always been, and this
adds a log beside it.

## Context

`playback_state` holds one row per item per user, overwritten on every play. It
answers "where was I" perfectly and can never answer "when did I watch this"
for any viewing but the last. Starting a film again replaces the earlier answer.
Revision 31 added `watch_count`, so the server knows *how many* times, but not
*when*.

The roadmap parked the question on its first decision: is the ask a **log**
(every sitting, kept for its own sake) or an **exporter** (get my data out in a
format another service reads)? Only the log is heavy, because it is a second
source of truth that grows without bound.

## Decision

Answered on 2026-09-30, and all four answers are recorded here because each
one is a real choice with a real alternative.

### 1. Both: a log, and an export of it

A new table, `viewing`, holds one row per finished viewing. The export reads
that table. An export of `playback_state` alone was the cheap option, and it
was rejected because it can produce only one date per title, which is exactly
the question it cannot answer.

### 2. A row is a *finished* viewing

A row is written on the same edge that moves `watch_count`: the moment an item
goes from not finished to finished. That happens inside `SaveProgress`'s
transaction, so the tally and the log cannot disagree about what counted.
Heartbeats that repeat "watched" do not add rows. Marking something watched
that was never played counts as one, as it already does for the tally.

Logging every session, abandoned ones included, was the alternative. It would
make the table grow several times faster to answer a question ("what did I
start and give up on") nobody has asked.

### 3. Films and episodes only

Music plays far more often than video: a 9,800-track library would outnumber
every film in the log within weeks. Scrobbling is a different feature with
different users (Last.fm, ListenBrainz), and it can be added later as a kind
the INSERT allows. The insert selects nothing for other kinds, so there is no
branch to forget.

### 4. Kept forever; the existing reset clears it

A finished-viewings row is a few hundred bytes. Years of heavy watching stay
well under a megabyte, so there is no retention rule and no pruning.

"Reset watched history" already exists, per account and priced before it runs.
Its `all` and `finished` scopes now delete log rows too, narrowed by the same
`under` item tree. `unfinished` leaves them, since nothing unfinished was ever
logged. Deleting an account deletes its rows.

### What goes on a row

What was watched is **copied onto the row**: kind, title, year, series, season,
episode and imdb id, plus an episode's show year and show imdb id. `item_id` is
`ON DELETE SET NULL`. A history that forgot a film the moment its file was
deleted would fail at the one job it has. The show's ids are copied because
other services identify an episode by its show.

There is no foreign key on `user_id`, matching `playback_state`. An unsecured
loopback server keeps its history under the `local` id, which has no account
row.

### Seeding

Revision 53 writes one row per film or episode already finished, dated from the
state row's `updated_at` and marked **`estimated`**. That is the best date the
old table can give, and the row says so. When `watch_count` is above one, the
earlier viewings are *not* invented: there is no date for them, and a history
padded with guessed rows is worse than a short one.

### The export

`GET /api/profile/viewings/export` returns CSV, or JSON shaped like the body of
Trakt's history sync (`?format=trakt`). An episode without season and episode
numbers cannot be placed under a show in the Trakt shape, so it is counted in
`skipped` and kept in the CSV.

LANcast sends the file nowhere. Pushing history to Trakt directly would be the
first outbound call made on a person's behalf to a third-party account, which
is a *no phone-home* question with its own ADR's worth of answer. A download
the person imports themselves keeps the principle intact.

## Consequences

- One new table and schema revision 53. A shape change, hence this ADR.
- `SaveProgress` now runs in a transaction and reads the old flag first. That
  is one extra indexed read per progress write, on a path that writes every
  five seconds.
- `GET /api/profile`'s `history` is unchanged and still means "last play per
  item". The new `GET /api/profile/viewings` is the log. Two lists with two
  meanings, and both documents say which is which.
- The Trakt shape was written from memory of Trakt's sync API and has not been
  imported into Trakt. The CSV is the format to trust until someone has.
