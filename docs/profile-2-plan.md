# Profile 2.0: a picture of your own, and a page that has kept up

Status: accepted, 2026-11-08. Step 0 (the walk) is done, §9; nothing else is built. It was written while
Settings 3.0 ([settings-3-plan.md](settings-3-plan.md)) was in its last steps,
and it is the last of the "3.0" pieces before 1.0. Chris's decisions are in §7;
each took the recommendation.

## 1. What the page is today

[Profile.tsx](../web/src/screens/Profile.tsx), top to bottom:

1. Your name, plus an "admin" badge, or "no account" on an open loopback server.
2. **Your picture**: six drawn animals. Pressing the chosen one again clears it.
3. Five totals: Started, Finished, Viewings (only when it differs), Time
   watched, Since.
4. Year in Review.
5. Favourites shelf, Your ratings, Your tags, the RetroAchievements card.
6. Recently played, 50 a page, with a link to the full watch history at
   `/profile/viewings`.

Two things about it shape this plan.

**It is a mirror, not a profile anyone visits.** Nothing on it is shown to
anybody else (ADR 0035, 0062), and there is no `/api/profile/{id}`. This plan
keeps that.

**The picture was kept out of uploads on purpose, twice.**
[Avatar.tsx](../web/src/components/Avatar.tsx) stores a key "so there is
nothing to host and no way for one account to put arbitrary images in front of
everyone else". [People.tsx](../web/src/screens/People.tsx) chose initials over
"a file store, a size limit, a format whitelist and a moderation question, for
a household of four". Those were the right calls at the time. They are reversed
here deliberately, with an ADR (§3), and each objection gets an answer instead
of being ignored.

## 2. What is wrong now

| Area | Today | The gap |
|---|---|---|
| **Picture** | Six line animals, shown only in the rail and the phone nav. People shows initials. The Bridge roster, Watch Together and the sessions dashboard show names. | It can't be you. Where it does appear, it is 18 px. |
| **Totals** | One query over `playback_state` (`ProfileStatistics`, internal/store/history.go). | Films, episodes, songs and audiobook chapters are one number, so music listening counts towards "Time watched". Comics and retro games are not in it at all. |
| **Shape** | One long scroll of nine sections. | It has outgrown that. With a TV remote, reaching Recently played means stepping through every shelf above it. |
| **History** | "Recently played" (each title once) here, and "Full watch history" (every finish) on its own page. | Two lists of nearly the same thing, and neither can be filtered by kind. |
| **Newer kinds** | Books, comics and retro games arrived after the page did. | None of them appears except through Recently played. |
| **Settings overlap** | Settings 3.0 step 4 moves RetroAchievements to "Connected accounts". | The page loses a section it now relies on (§6). |

## 3. Picture upload (ADR 0121)

ADR 0121 records the reversal and the rules below. The schema change is one
nullable column, so it needs only a migration. The ADR is for the policy.

### 3a. Stored as our own copy, never the uploaded file

The server decodes the upload, crops it square, and writes **new** JPEGs at
256 px and 64 px. The uploaded bytes are thrown away. That one rule answers
most of the risk:

- **EXIF goes with them.** A phone photo carries GPS coordinates. Re-encoding
  drops them without a stripping step that could miss a tag.
- **Nothing hidden survives.** Polyglot files, trailing payloads and odd colour
  profiles do not make it through a decode and re-encode.
- **The client never serves an untrusted image.** Everything it shows was
  written by `image/jpeg` on the server.

JPEG output because `x/image` decodes WebP but cannot encode it. Transparency is
flattened onto the page's surface colour. The circle is a CSS mask, not baked in.

### 3b. What is accepted

- **JPEG, PNG and WebP.** WebP uses `golang.org/x/image/webp`, already in go.mod.
  Animated GIF, HEIC and SVG are refused (HEIC is parked, §8).
- **10 MB body**, through `http.MaxBytesReader`.
- **Dimensions read before decoding.** `image.DecodeConfig` comes first. Above
  8,000 px on a side, or 40 megapixels, is refused without decoding, so a small
  file cannot expand into gigabytes.
- **The decoder is fuzzed**, the way internal/comic/pdf is: arbitrary bytes in,
  a refusal or a valid 256 px JPEG out, never a panic.

### 3c. Cropping happens in the browser, checking happens on the server

The client shows a square crop with drag and zoom on a canvas. It sends only
the cropped square, at most 1,024 px. The server still runs §3a and §3b on it,
because the client is not a security boundary.

The editor must work three ways: mouse and touch, a TV remote (arrows move,
`+`/`−` or up and down in a zoom zone, Enter saves, Back cancels, through
`useFocusZone`), and a phone (`accept="image/*"` offers the camera as well).

### 3d. Storage

> **Superseded by §9d:** backups hold only the database, so the pictures are
> stored in it as BLOBs, not as files. The rest of this section is kept as it
> was first written.

- **Files:** content-addressed under the data dir (`pictures/<sha256>-256.jpg`,
  `-64.jpg`), hashed from the encoded output, next to the artwork cache rather
  than inside it, since artwork may be cleared and rebuilt and these may not.
- **Column:** `user.picture TEXT` (nullable), holding the hash. `user.avatar`
  stays. A picture, when set, wins over an avatar, and an avatar wins over
  initials.
- **Clean-up:** replacing or clearing deletes the old files once no account
  points at them.
- **Backups:** `pictures/` must be in the backup and restore set (restore
  shipped in v0.9.96). Step 0 checks what that set holds today.

### 3e. API

| Method and path | Who | What |
|---|---|---|
| `PUT /api/profile/picture` | the signed-in account | Raw image body (`Content-Type: image/jpeg|png|webp`). Answers the new `/api/me` shape. |
| `DELETE /api/profile/picture` | the signed-in account | Back to your avatar or initials. |
| `GET /api/pictures/{hash}?size=64\|256` | any **signed-in account on this server** | `Cache-Control: private, immutable`, since the hash changes when the picture does. |
| `DELETE /api/users/{id}/picture` | admin | Clears somebody else's. |

- `/api/me`, `/api/people` and the Bridge roster gain a `picture` field (hash or
  null).
- `api.md` and `openapi.json` change in the same commit, as always.
- **Federated guests cannot read `/api/pictures`.** It is not under
  `/api/federation/`, so it is outside the guest allow-list by default. A test
  asserts that, rather than relying on where the path happens to sit.
- On an open loopback server there is no account, so there is no upload. The
  page offers the animals only, as it does today.

### 3f. Moderation, for a household

- An admin can clear anyone's picture from the Users pane.
- A server setting, **"Members may upload a picture"**, is on by default. Off
  hides the upload button and makes `PUT` answer 403. Existing pictures stay
  until cleared. Admins can always upload their own.

That is the whole moderation story, on purpose. Anything more is for a server
with strangers on it, and LANcast is not that server.

### 3g. Where the picture shows

One `<Avatar user size>` component replaces `AvatarGlyph` and `initials()`.
It is used in the rail, the phone nav, the Profile header, People, the Bridge
roster, the Watch Together room, the sessions dashboard and the Users pane.

Setting or clearing a picture must invalidate every one of those queries
(`["me"]`, people, roster, sessions, users). This is the project's most-repeated
quiet bug, and the step 2 tests list each surface by name.

**Not across federation.** Paired servers keep seeing initials. Presence
(ADR 0045) names exactly which fields leave the server, and an image is a much
bigger addition than a field. It is parked (§8), not refused.

## 4. The page itself

### 4a. Header

A 96 px picture, your name, "since" (first play), your role, and **Edit** next
to the picture. Edit opens the picture editor (upload, crop, or choose an
animal, or clear). The name links to Settings › Profile, where it is saved:
ADR 0120 puts facts about you in Settings, and the page only displays them.
The six-button row goes away.

### 4b. Tabs

**Overview · History · Ratings & tags · Achievements**, as `?tab=` so a tab can
be linked and Back works. On a phone they become a horizontal strip. With a
remote they are a focus row at the top, so every section is two presses away
instead of nine.

- **Overview:** the totals (§4c), Year in Review, Favourites, and the newer
  kinds (§4d).
- **History:** §4e.
- **Ratings & tags:** the two lists as they are now.
- **Achievements:** see §6.

### 4c. Totals by kind

`ProfileStatistics` grows a split by `media_item.kind`: watched (films,
episodes), listened (tracks), and books (audiobooks). Each gets its own count and
time, and the qualifiers stay on screen as they are now. Plus **Most played**:
your five highest `watch_count` titles.

One honest gap to handle, not hide: the amounts banked by a history reset are
not split by kind today. They show as "earlier, not split by kind" until a reset
starts banking per kind. The total must not quietly shrink.

### 4d. The newer kinds on Overview

Small sections, each present only when it has something to show:

- **Books:** in progress and finished.
- **Comics:** in progress and read. Step 0 finds where reader progress is kept.
- **Retro:** recently played and time played. Step 0 finds whether play time is
  recorded at all. If not, the section shows "recently played" only, without
  inventing time.
- **Your playlists:** the ones you made.

### 4e. History, merged

One list, with a filter by kind (All · Video · Music · Books · Comics · Games)
and a switch between **Each title once** (today's Recently played) and **Every
time** (today's full history). `/profile/viewings` redirects to
`?tab=history&every=1`, so old links still work.

## 5. Steps

Each step is its own PR, in this order.

0. **Walk and measure, no code.** *Done 2026-11-08; see §9.* Read what `ProfileStatistics` counts per kind
   on the live database (copied with its -wal and -shm). Find where comic
   progress and retro play time live. Check what backup and restore include.
   Walk the current page at 375 px and with a remote. Findings go in §9 of this
   plan.

   **Then, before step 1: posters on the profile routes** (§9a). A bug fix of
   its own, with no dependencies.
1. **ADR 0121 and the server half of the picture.** *Built 2026-11-08: `internal/picture`, schema 94, the four routes, `member_pictures`.* Migration (a
   `user_picture` table, §9d), decode/crop/encode with its fuzz test, the
   four routes, the setting, the guest-denial test, a backup-and-restore test that brings a picture back, `api.md`
   and `openapi.json`. Bump the ADR count in README.md and CLAUDE.md.
2. **The picture in the client.** *Built 2026-11-08. The dashboard shows no people, so it has no picture; the Users pane and Watch Together do.* `<Avatar>`, the crop editor (mouse, touch,
   remote, phone), every surface in §3g, the admin clear in Users, and the
   setting in its pane. Invalidation tests per surface.
3. **Header and tabs.** *Built 2026-11-08. The animals moved into the picture editor; the page is now a focus zone, which it never was, so a remote reaches every control.* Move the existing sections into tabs, with no new data.
   This is where Achievements lands (§6).
4. **Totals by kind and Most played.** The server split, its tests (including
   the banked amounts), and the client.
5. **Overview's newer kinds and the merged History.** Including the
   `/profile/viewings` redirect.

Steps 1 and 3 touch different files and could run in parallel. Step 2 needs 1.
Steps 4 and 5 need 3.

**Swap the build before believing it.** Steps 1 and 2 are checked against the
installed service with a real phone photo: upload from the phone, confirm the
location is gone from the stored file, and see the picture on a second account's
People page.

## 6. Overlap with Settings 3.0

Settings 3.0 step 4 moves the RetroAchievements connection to "Your account ›
Connected accounts". Profile keeps showing what you have **earned**: the
Achievements tab is a read-only view of recent unlocks and progress, with
"Connected in Settings ›" when connected and "Connect in Settings ›" when not.
Connecting and disconnecting live only in Settings. That way neither plan has to
wait for the other, and the card's connect controls move out exactly once.

## 7. Decisions (Chris, 2026-11-08)

Each took the recommendation.

1. **Upload is allowed**, reversing Avatar.tsx and People.tsx, recorded as
   ADR 0121.
2. **The server re-encodes**; the original is never stored. JPEG, PNG and WebP;
   10 MB; dimensions checked before decoding.
3. **Crop in the browser**, checked again on the server.
4. **Admins can clear anyone's picture**, and a server setting allows member
   uploads (on by default).
5. **This server only.** Pictures do not cross to paired servers.
6. **The animals stay** as a choice, with initials as the last fallback.
7. **The picture shows everywhere a person is named on this server.**
8. **Tabs**: Overview · History · Ratings & tags · Achievements.
9. **Totals split by kind**, plus Most played.
10. **One History**, filtered by kind.
11. **The visible-to-others card is parked** until after 1.0.

## 8. Parked

- **Pictures across federation.** Needs an ADR 0045 amendment and an answer to
  "who caches a peer's face, and for how long".
- **HEIC uploads.** The photo library already makes display copies of HEIC; the
  upload path could reuse that later. Phones offer JPEG when a page asks for
  `image/*` in most cases anyway.
- **A card other people see** on People, built on the ADR 0035 opt-in.
- **Play time for retro games.** Nothing records it today (§9c); the Retro
  section shows the last save instead.
- **Per-kind banking** of history resets. §4c shows the gap instead of fixing it.

## 9. Step 0 findings

Walked 2026-11-08 against the installed v0.10.10 service, through the
`client-dev` proxy, signed in as the admin account. The database was read from a
copy of `lancast.db` with its `-wal` and `-shm`. No code was changed.

### 9a. A bug: no posters anywhere on the page

`GET /api/profile` and `GET /api/profile/ratings` never call `AttachArtwork`.
Every item comes back with no `artwork`, so all 60
rows on the page (Recently played and Your ratings) draw the first letter of the
title instead of a poster. Measured: 60 rows, 0 with an `<img>`. Favourites is
fine because it goes through the shelf path. It has been this way since the
profile route was first written (7fdfb87d). Nothing failed, so nobody noticed.

This is fixed on its own, ahead of step 1, and is not left for step 3 (#1052).
It is the "tiles need their posters" lesson already written in `rating.go`'s
`listToRate`. A test asserts a poster on both routes. `GET
/api/profile/viewings` is not part of it: its rows are a log that outlives
deleted items and carry no item, so the full watch history has no posters by
design.

### 9b. What the totals count

From `playback_state` joined to `media_item`, for the account with the history
(all rows with `position_ms > 0` or `watched = 1`):

| Kind | Started | Finished | Viewings | Hours |
|---|---|---|---|---|
| movie | 426 | 266 | 346 | 700.3 |
| episode | 79 | 72 | 123 | 56.2 |
| track | 208 | 126 | 161 | 13.7 |
| comic | 1 | 1 | 1 | 0 |

- **"Time watched" includes music.** About 14 of the 770 hours are songs. Small
  here, but it would be most of the number for someone who mainly listens.
- **"Started" counts songs.** 208 of the 714 titles "started" are tracks. Each
  song played is one more title started.
- **Comics are in it after all.** Reading progress lives in `comic_place` (by
  page, ADR 0101), but marking a comic watched writes a `playback_state` row
  with no duration. It adds one to Started and Finished and nothing to time.
- **Audiobooks: no rows yet.** 123 books and 1,300 parts in the library, none
  played. The split needs fixture tests for `book_part`, because the live data
  can't show it.
- **The split already exists.** Year in Review groups by `mi.kind`
  (`yearKinds` in internal/store/yearreview.go) and prints
  "426 movies · 208 tracks · 79 episodes · 1 comic". Step 4 reuses that query
  shape for lifetime totals instead of writing a second one.
- **Banked totals: `profile_totals` is empty on this server.** Nobody has reset
  their history, so the "earlier, not split by kind" line (§4c) has nothing to
  show here. It still needs a test.

### 9c. Where the newer kinds keep their state

- **Comics:** `comic_place (user_id, item_id, page, pages, updated_at)`, one
  row so far. "In progress" is `page < pages`. "Read" is the `playback_state`
  watched row described above.
- **Retro games:** there is **no play time anywhere**. `rom_save` has
  `updated_at` per save slot (45 rows, all `kind = 'rom'`), and that is the
  only per-person record of playing. §4d's Retro section therefore shows "last
  saved" and nothing about time. Recording play time is new work, parked (§8).
- **Audiobooks:** `playback_state` on `book_part`, as above.

### 9d. Backups hold the database and nothing else

`internal/backup` snapshots `lancast.db` (ADR 0058). No file in the data
directory is copied. §3d's files under `pictures/` would be **missing from
every backup and every restore**, and a restore would point accounts at hashes
with no file behind them.

**So §3d changes: store the two JPEGs in the database.** A `user_picture
(user_id, size, jpeg BLOB, sha256)` table instead of files. At 256 px and 64 px
that is roughly 20–30 KB per account, which is nothing beside an 89 MB database.
It also removes file clean-up and the question of a data-dir move. The hash
stays, so the URL and `immutable` caching in §3e are unchanged. That makes it a
new table rather than a nullable column, so ADR 0121 covers the schema as well.

### 9e. The page as it stands

Desktop (1,024 px wide):

- **7,480 px tall.** Recently played starts at 3,393 px, below Favourites,
  Ratings (10 rows), Tags and the RetroAchievements card.
- **32 focus stops before Recently played**, out of 84 on the page. With a
  remote, that is the case for tabs (§4b) in one number.
- **The RetroAchievements card is the largest section** (about 900 px): a
  status line, the earn-while-playing switch, recent unlocks and progress on 8
  of 14 games. As the read-only Achievements tab (§6), it moves out of the way.
- **Recently played is mostly songs.** Of the 20 most recent rows, about half
  are tracks at "0m" or "1m". This makes the case for the kind filter (§4e):
  songs push films off the first page.

Phone (375 px):

- No sideways overflow. The Favourites shelf runs past the edge, but it scrolls
  on its own, as intended.
- **The picture picker takes most of the first screen**: two rows of three
  buttons and then an empty gap before the totals. The header with Edit (§4a)
  fixes it.
- **Recently played starts at 3,980 px**, about five screens down.

### 9f. What changes in the plan

1. A separate posters fix comes first (§9a).
2. Pictures go in the database, not on disk (§9d). Step 1's migration is a new
   table and ADR 0121 says why.
3. The Retro section shows last saved only. Play time is added to §8.
4. Step 4 reuses Year in Review's per-kind query.
