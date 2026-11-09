# ADR 0121 — A picture of your own: uploaded, re-drawn by the server, kept in the database

Date: 2026-11-08 · Status: **accepted** 2026-11-08 · Related: [ADR 0035](0035-who-may-see-whose-viewing.md), [ADR 0045](0045-live-presence-between-paired-servers.md), [ADR 0046](0046-remote-guests.md), [ADR 0058](0058-a-backup-is-the-database.md), [docs/profile-2-plan.md](../profile-2-plan.md)

## Context

An account's picture has been a key from a fixed set since schema 52: six line
animals, drawn by the client. The server stored a word. That was a deliberate
choice, written twice in the code:

- `web/src/components/Avatar.tsx`: storing a key leaves "nothing to host and
  no way for one account to put arbitrary images in front of everyone else".
- `web/src/screens/People.tsx`: initials rather than an upload, because an
  upload is "a file store, a size limit, a format whitelist and a moderation
  question, for a household of four".

Profile 2.0 (docs/profile-2-plan.md §3, decision 1) reverses this. A picture
that can't be you is the profile's biggest gap. This ADR records the reversal
and the rules that answer each of those objections.

## Decision

**An account may upload a picture of itself. The server never keeps the
uploaded file: it decodes it, crops it square, and stores two JPEGs it drew
itself, in the database.**

### 1. Re-drawn, never stored as sent

The upload is decoded, centre-cropped to a square, scaled to 256 px and 64 px,
flattened onto the page colour (`#070b18`), and encoded as JPEG. The uploaded
bytes are discarded.

That one rule does most of the safety work:

- **EXIF goes with it**, including a phone photo's GPS position, with no
  stripping step that could miss a tag.
- **Nothing hidden survives**: trailing payloads, polyglot files and odd
  colour profiles do not make it through a decode and a fresh encode.
- **What other people's clients are given was written by `image/jpeg`** on this
  server, not by whoever uploaded it.

### 2. What is accepted

- JPEG, PNG and WebP, judged by the decoder, not by the request's
  `Content-Type`. A GIF, HEIC, SVG or anything else is refused, even when some
  other package in the binary registered a decoder for it.
- At most 10 MB.
- Dimensions are read before decoding (`image.DecodeConfig`). More than 8,000
  px on a side or 40 megapixels is refused without allocating a pixel, so a
  small file cannot expand into gigabytes.
- The whole path is fuzzed (`internal/picture`): any bytes in give either a
  refusal or two valid JPEGs out, never a panic.

The client crops before sending (docs/profile-2-plan.md §3c). The server still
applies every rule above, because the client is not a boundary.

### 3. In the database, not on disk

A `user_picture` table (schema 94) holds one row per account: the hash of the
256 px JPEG, both JPEGs as BLOBs (roughly 20–30 KB together), and when it was
set.

Files were the first plan and were dropped in step 0 (profile-2-plan §9d).
A backup (ADR 0058) is a snapshot of `lancast.db` and nothing else, so files
under the data directory would have been missing from every backup and every
restore. In the database, a picture is backed up, restored and moved with
everything else, and nothing needs cleaning up.

No foreign key, like `home_shelf`. Deleting an account removes its row by hand
(`DeleteUser`).

### 4. Who sees it

- **Any signed-in account on this server**, through
  `GET /api/pictures/{hash}`. The hash changes when the picture does, so the
  response is `private, immutable`. A request with an unknown hash gets 404.
- **Not a federated guest.** The route is not on the guest allow-list
  (ADR 0046), which closes it by default; a test says so by name.
- **Not a paired server.** Presence (ADR 0045) names exactly which fields
  leave this server, and an image is not one of them. Peers keep seeing
  initials. Changing that needs an amendment to ADR 0045, not a field added in
  passing.
- **Not the sign-in page.** The account picker there is shown to devices that
  have not signed in, so it stays names only.

### 5. Moderation, sized for a household

- An admin can clear anybody's picture (`DELETE /api/users/{id}/picture`). It is
  audited.
- A server setting, `member_pictures`, on by default. Off refuses members'
  uploads with 403 and leaves existing pictures until somebody clears them.
  Admins can always upload their own.

That is all of it, on purpose. A server with strangers on it would need more,
and LANcast is not that server.

### 6. Precedence

A picture, when set, wins over a chosen animal; an animal wins over initials.
The animals stay as a choice. Clearing a picture falls back to whatever animal
was chosen before.

## Consequences

- `/api/me`, `/api/people` and `/api/users` carry `picture`: the hash, or null.
  Clients build the URL themselves.
- The database grows by tens of kilobytes per account that sets a picture.
- Schema 94 is one-way, like every migration.
- The decode path is new attack surface on a server that reaches the internet.
  It is bounded by the size and dimension checks, the format allow-list, and
  the fuzz test, and it only accepts uploads from signed-in accounts.
