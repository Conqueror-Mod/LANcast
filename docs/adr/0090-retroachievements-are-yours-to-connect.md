# ADR 0090 — RetroAchievements are yours to connect

Date: 2026-10-18 · Status: **accepted** 2026-10-18

Chris asked for a RetroAchievements tie-in and an achievement tracking module
on the profile page. The work splits into two parts of very different size:
- **reading** someone's RetroAchievements progress;
- **earning** achievements while playing in LANcast, which needs the rcheevos
  library inside the game player, RetroAchievements' own game hashes, an
  offline unlock queue, and hardcore-mode approval from RetroAchievements.

Chris chose **reading first**. This record covers that part only.

He decided on 2026-10-18:
- **Only you see it.** Your RetroAchievements progress shows on your own profile
  page and nowhere else in LANcast: not to the household, and not to friends'
  servers. It is already public on retroachievements.org; LANcast does not
  re-share it.
- **The key is encrypted, with the encryption key kept apart.** Each person's
  RetroAchievements API key is sealed in the database with a key held in its
  own file, so a database backup on its own carries no usable key.

## Context

RetroAchievements' Web API needs an account's **Web API key**, passed as `y`,
which a person copies from their RetroAchievements control panel. With it the
server can read three things:
- a profile (`API_GetUserProfile`): name, ULID, picture, points;
- recent unlocks (`API_GetUserRecentAchievements`, `m` = minutes back);
- completion progress across games (`API_GetUserCompletionProgress`, `c` up
  to 500).

Usernames can change, and the ULID cannot, so the ULID is what gets stored.

**No phone-home** is one of LANcast's four principles. A server that contacted
retroachievements.org on its own would break it. So would a page that made
every viewer's browser fetch pictures from `media.retroachievements.org`, which
would tell RetroAchievements who is looking and from where.

## Decision

**1. Opt-in, per account.**
- Nothing contacts RetroAchievements until a person connects their own
  account, by giving their RetroAchievements username and Web API key.
- After that, the server contacts RetroAchievements only for that person, and
  only when they open their profile page.
- Disconnecting deletes the stored key and username, and stops all contact.
- Deleting the account does the same.

**2. Connecting is checked.**
- The server asks RetroAchievements for the profile using the key.
- A wrong name or key is refused with RetroAchievements' answer, and nothing
  is saved.
- What's saved is the ULID, the display name, and the sealed key.

**3. The key is sealed.**
- **Cipher:** AES-256-GCM, with a random nonce per sealing.
- **Where the key lives:** `<data>/secrets.key`, 32 random bytes made on first
  use, readable only by the server.
- **Backups:** LANcast backups are the database, not the data folder. A backup
  restored on another machine has keys it can't open, and each person
  reconnects. The page says so.
- **Never shown back.** The key is never returned by any route and never
  logged. The connection status shows only the RetroAchievements name.

**4. The server fetches, and caches.**
- A profile view reads the profile, the last two weeks of unlocks, and the
  first 100 games of completion progress.
- Each answer is cached in memory per account for 10 minutes, so reloading the
  page doesn't re-ask RetroAchievements. "Refresh" asks again, at most once a
  minute.
- Requests carry the user agent `LANcast/<version>`.

**5. Pictures come through the server.**
- Avatars, badges and game icons are fetched by the server from
  `media.retroachievements.org` and cached on disk, so a viewer's browser
  never contacts RetroAchievements.
- Only paths under `/UserPic/`, `/Badge/` and `/Images/` are fetched, and
  only `.png` files. The route is not a general proxy.

**6. Your own, only.**
- Every route answers for the caller and nobody else. No route takes another
  account's id.
- Friends' servers never see it, and federation exposes nothing new.

## Schema

Revision 77 adds the table `retro_account`, keyed by `user_id`. It has no
foreign key to `user`, for the reason `session` and `passkey` have none:
`DeleteUser` removes the row in its own transaction.

| Column | Holds |
|---|---|
| `user_id` | the LANcast account |
| `ra_user` | the RetroAchievements display name |
| `ra_ulid` | the RetroAchievements ULID |
| `api_key_sealed` | the sealed API key, as a BLOB |
| `connected_at` | when it was connected |

## Not in this decision

**Earning achievements while playing** needs rcheevos inside the desktop
player: RetroAchievements' game hashes, memory reads from the libretro core,
an offline unlock queue, and, for hardcore mode, approval from
RetroAchievements (a unique user agent, six months of public release, a
privacy policy, and a list of the bundled cores' licences). That gets its own
record when it's built. Until then LANcast **shows** progress and doesn't
**make** it.
