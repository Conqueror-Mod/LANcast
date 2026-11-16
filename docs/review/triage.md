# Review triage

Drafted 2026-11-16 against `main` at `dbba732f`. The ledgers are in [findings/](findings/), and the charter is in [README.md](README.md).

**State of play.** All 18 S1s and 37 of the 39 S2s are fixed and merged (#1087–#1145). The swap sitting on 2026-11-15 proved them on the installed service. This sheet covers what is left: two S2 decisions, 73 S3s, and four items found after the ledgers were written. I checked every S3 against `main`, and none was fixed in passing.

**How to read it.** Every row has a recommendation. The three policy calls were decided by Chris on 2026-11-16, all as recommended, and the rest of the sheet was accepted. "Before 1.0" means it goes into a fix wave ahead of v0.11.1. "After 1.0" means it goes to the 1.x backlog with its ledger entry as the brief. "Won't fix" gets one line of reason, written into the ledger.

The tally of recommendations: **57 before 1.0** (56 S3s and T-3, most of them a few lines each), **18 after 1.0**, and **3 decisions** (two S2s and one S3). R10-11 is split between the two lists: its quoting goes before 1.0, its `User=` line after.

---

## Decided (Chris, 2026-11-16: all three as recommended)

| ID | What | Decision |
|---|---|---|
| R01-4 (S2) | A session alone can mint an API key, and the key outlives a password change | **Ask for the password when minting a key**, as adding a passkey already does. This keeps ADR 0061's rule that a routine password change does not break the machine that copies photos. Also have an **administrator's password reset revoke that account's keys, and say so**, because that action means "this account was compromised". ADR 0061 needs an amendment. |
| R04-3 (S2) | Restoring a backup brings back API keys and passkeys that were revoked | **Clear `api_key` and `passkey` from the restored database, as sessions already are** (ADR 0058's own reasoning). *Amended 2026-11-16:* recovery codes are **kept**. Only unused hashes are stored, so a restore cannot tell which were used after the backup, and clearing them all could lock a two-factor account out on another machine where the TOTP secret cannot be unsealed. The restore result tells two-factor accounts to regenerate their codes. A restore is rare and deliberate, every account still has its password, and re-issuing a key or re-adding a passkey costs a minute. The alternative is to keep them and list them in the restore result. That leaves a leaked key working until somebody reads the list. ADR 0058 needs an amendment. |
| R04-8 (S3) | Turning "visible to peers" off leaves per-person presence grants working | **The switch wins**: `ReadersOf` requires `visible_to_peers = 1`. Somebody who flips the switch to disappear expects to disappear. The grants are kept, so turning visibility back on restores them. |

## Found after the ledgers

| ID | What | Severity | Recommendation |
|---|---|---|---|
| R05-4 | A book part someone moved went back to the folder order on the next scan: `SetBookPart` overwrote a locked `episode` | S1 (a locked field overwritten) | **Done**, fixed in #1136 (its second commit). Recorded in R05.md. |
| T-1 | A Quality or audio-track change made before playback moved reused the running HLS session | S2 | **Done**, #1144, proven on the service. |
| T-2 | Odd frame rates listed too few segments, and a 1000 fps probe set a GOP of about 83 s (a #1093 regression, never released) | S2 | **Done**, #1145, proven on the service. |
| T-3 | *Halloween II (2009)* is stored as 16 min (960000 ms), but its file is 7,145 s, so any encode lists 16 minutes and then stops | S2 | **Fixed in #1149.** A wrong first match to a 16-minute short filled its runtime while the probe ran alongside it; correcting the match left the runtime. The probe is now the only writer of `duration_ms`. **31 films** hold provider runtimes from the same race and need a re-probe (T-6). |
| T-4 | An AC3 film at Original in a browser waits about 2 min, because the server serves only a finished remux playlist | S3 | **After 1.0.** It's deliberate (WebView2 cannot play a growing playlist), but a browser could be served the growing one. That needs a client signal, so it's a 1.x item. Name it in the release notes' known issues. |

---

## S3s: before 1.0

Grouped into fix waves by the files they touch, so the sessions can run in parallel without sharing a file. "Size" is rough: **XS** means a line or two, **S** under an hour, **M** a few hours.

### Wave A: auth and accounts (`internal/auth`, `api/auth.go`, `users.go`, `account.go`, `invites.go`, `passkeys.go`, `twofactor.go`, `loginpicker.go`, `netguard`, `store/user.go`)

**Merged in #1153.**

| ID | What | Size |
|---|---|---|
| R01-5 | A password over 72 bytes gets a 500 → 400 with the limit (and `docs/api.md`) | XS |
| R01-6 | A stranger can fill the passkey challenge store → cap it per address and evict the oldest | S |
| R01-7 | Sign-in timing reveals whether an account exists → check against a dummy hash | XS |
| R01-8 | Invite redemption runs bcrypt before checking the token → throttle, then look up, then hash | XS |
| R01-9 | The throttle map grows for ever → sweep and cap | XS |
| R01-10 | Three account-creation paths skip `validName` → call it everywhere, and refuse category Cf | XS |
| R01-11 + R04-5 | Two admins deleting each other leaves none → `ErrLastAdmin` inside `DeleteUser`'s transaction. One fix for two entries | S |
| R01-12 | The two-factor password prompt has no per-account wait → one `passwordConfirmed` for all three callers | S |
| R01-13 | Behind a TLS proxy the cookie isn't marked `Secure` → trust `X-Forwarded-Proto` from a trusted proxy only | S |
| R01-14 | A machine with a public IPv4 treats the ISP's subnet as the house → private ranges only for IPv4. **Check Aither:** it has a static public address | S |
| R01-15 | `netguard` lets carrier-grade NAT (Tailscale) through → add the reserved blocks | XS |
| R01-16 | An admin edit that is half refused is half applied → validate before writing | S |
| R04-6 | A deleted account's reviews and totals remain → delete them, and add a test that walks every `user_id` table | S |
| R13-8 | The downloads list is shared across accounts on one device → key it by account (client, `downloads.ts`) | XS |
| R15-3 | Revoking an API key takes one press → two presses (client, `security.tsx`) | XS |
| R01-4 | As decided: a password to mint a key, and an admin reset revokes keys (ADR 0061 amendment) | S |

### Wave B: federation, DLNA, network (`api/guest*`, `presence.go`, `peerwatch.go`, `api/dlna.go`, `internal/dlna`, `serverapp/dlna.go`)

**Merged in #1148.**

| ID | What | Size |
|---|---|---|
| R02-4 | Accounts that opted out of peers are still named to every paired server → skip them | XS |
| R02-5 | Peer JSON is decoded with no size limit → `LimitReader` plus the roster checks | XS |
| R03-6 | DLNA answers with no limits for up to 3 s after its account is cleared → 503 | XS |
| R10-8 | SSDP answers any source → private and same-subnet sources only (Aither is public) | S |
| R04-8 | As decided: the switch outranks the grants | XS |

### Wave C: library data and settings (`api/libraries.go`, `items.go`, `playlists.go`, `activity.go`, `meta.go`, `viewings.go`, `backups.go`, `faces.go`, `store/plugin.go`, `store/browsefilter.go`, `serverapp/main.go`)

**Merged in #1152.**

| ID | What | Size |
|---|---|---|
| R06-5 | The all-libraries cast search names people from hidden libraries → filter it | S |
| R06-6 | Playlist writes don't check visibility → check against the caller's limits | S |
| R06-7 | The history export's file name is dated in UTC → local date | XS |
| R06-8 | A library edit that is half refused is half applied → check the path first | XS |
| R06-9 | The "bad kind" error names 5 of the 8 kinds → build it from `validKinds` | XS |
| R07-5 | The activity feed names hidden libraries → filter for members, drop `error` | S |
| R07-6 | An item edit and its lock are separate writes → one transaction. *This is the locked-fields rule* | S |
| R07-8 | A restore's audit record is lost in the database it replaces → write it after applying | S |
| R07-10 | A second face pass answers "started" and does nothing → 409 | XS |
| R05-3 | Removing a plugin can leave its credentials behind → one transaction | XS |
| R08-7 | The artwork prune can delete images written during the walk → skip files newer than the snapshot | XS |
| R10-10 | Shutdown closes the DB under unowned goroutines → one wait group | S |
| R04-3 | As decided: clear keys and passkeys on restore, keep recovery codes (ADR 0058 amendment) (`backup`, `serverapp`) | S |

### Wave D: metadata and logs (`internal/meta`, `nfo`, `enrich`, `report`, `reportissue.go`)

**Merged in #1149.**

| ID | What | Size |
|---|---|---|
| R08-5 | "Write NFO" puts a `tvshow.nfo` holding `<movie>` in every season folder → stop, and remove the ones LANcast wrote (they carry its marker). *It writes into the user's library* | S |
| R08-6 | A provider's API key reaches the log on a network error → strip the URL. *The built-in keys would ride out in Report an issue* | S |
| R07-9 | A failed problem report logs the webhook URL and its token → the same fix | XS |
| R08-8 | TMDB and OMDb responses have no size limit → `LimitReader` | XS |
| T-3 | Halloween II's duration → find the writer | S |

### Wave E: playback, server side (`internal/probe`, `transcode`, `subtitle`, `optimized.go`, `stream.go` and the other open-file sites, `romsaves.go`)

**Merged in #1150.**

| ID | What | Size |
|---|---|---|
| R03-3 | The optimized sweep deletes any `<digits>.mp4` in the folder → its own name pattern, and refuse a non-empty folder. *Deletes user files* | S |
| R03-4 | A file being streamed can't be deleted on Windows, and an in-app delete loses the row and keeps the file → share `openShared` everywhere | M |
| R03-5 | A game save can land on disk with no row → write the row on a detached context, and undo the rename | S |
| R09-8 | An absurd duration is accepted → cap it at parse time and in `CompletePlaylist` | XS |
| R09-11 | Subtitle extraction is cut off at 60 s, so big remuxes never get subtitles → detach it and cache | S |
| *Verify as the service:* | this wave touches `transcode`/`probe`, so it gets a swap kit | |

### Wave F: client (`web/src`, outside the files Wave A takes)

**Merged in #1151.**

| ID | What | Size |
|---|---|---|
| R13-6 | Eight-plus screens aren't refreshed when the server changes a list → one key list for both paths. *CLAUDE.md's most-repeated bug* | S |
| R13-7 | Reconnecting the event stream doesn't mark cached screens stale → invalidate everything | XS |
| R14-5 + R16-2 + R16-6 | A playlist holding the same track twice: shuffle drops a copy, the queue panel highlights both, and pressing the second copy plays on from the first → carry the position. One PR, since it's one rule | M |
| R14-6 | One failed Watch Together poll ends the room → end only on 404/403. *Needed for the Georgia evening* | XS |
| R14-7 | The last progress write on closing a tab is dropped → `keepalive` | XS |
| R15-4 | Create twice makes two playlists, and failures say nothing → busy and error props | S |
| R15-5 | Deleting a playlist can strand you on it → ignore presses while pending | XS |
| R15-6 + R16-4 + R16-5 | Ambient gold: faces, retro, a saved rating, picker folders → dormant at rest. *design.md's one rule* | S |
| R16-3 | Episode air dates show a day early west of Greenwich → `timeZone: "UTC"` | XS |

### Wave G: retro and Linux (`internal/retro/ra`, `internal/service`)

**Merged in #1147.**

| ID | What | Size |
|---|---|---|
| R12-5 | A repeated form key gets past the RetroAchievements allow-list → refuse duplicates. *It could get a user's RA account flagged* | XS |
| R10-11 | The systemd unit breaks on a path with a space → quote it. Ahead of S6's Linux run. `User=`: see after 1.0 | XS |

## S3s: after 1.0

| ID | What | Why it can wait |
|---|---|---|
| R02-3 | One paired server can fill the guest-session book | Needs a hostile paired friend, and since ADR 0071 the window no longer uses tickets |
| R02-6 | Guest CORS on the default port | Reachability uncertain for the same reason. Check it when Watch Together across servers is next tested |
| R02-7 | Unbounded remote players per account | Needs a hostile household member, whom the project trusts with more |
| R04-7 | Switching RetroAchievements accounts can mis-send queued unlocks | Needs two RA accounts and an outage at once |
| R07-7 | Settings writes can undo each other | Needs two writers within milliseconds; the fix is a refactor of every writer |
| R09-7 | An MJPEG camera clip plays as sound only | Rare in a media library |
| R09-9 | The session ceiling doesn't hold its slot | Bounded: an extra stream ends with its request |
| R09-10 | ffmpeg outlives a crashed server | Needs a Windows job object and a service-session check |
| R09-12 | Subtitles shift by the requested offset, not the real start | Needs an API change; up to 2.4 s on a resume |
| R09-13 | A `%` in the data directory breaks HLS | The data directory is fixed by the installer |
| R09-14 | `Rocky.mkv` claims `Rocky 2.srt`; `.sub` is taken as SubRip | Two films in one folder is uncommon |
| R10-7 | A local account can squat the single-instance mutex | Local denial of service only, and #1141's recovery retries |
| R10-9 | A half-failed update leaves a new file beside the old exe | No current release ships a pair that must match |
| R10-11 (`User=`) | The Linux service runs as root | Documented in security.md; costs an install step |
| R12-6 | The desktop keeps an old ROM of the same size | Needs an API change (a file hash in the listing) |
| R14-8 | Stopping a native film mid-open can leave mpv playing | A window of a few IPC round trips |
| R14-9 | Repeat-one on a converted film replays from the conversion start | Rare: repeat-one on a film |
| T-4 | AC3 at Original waits about 2 min in a browser | By design for WebView2; a known issue in the notes |

## Won't fix

None recommended. Every S3 is either cheap enough to take before 1.0 or real enough to keep in the 1.x backlog.

---

## After Chris's pass

1. ~~Write each row's outcome into its ledger's `Triage:` line, and add R05-4 to R05.md.~~ Done 2026-11-16.
2. ~~Start the waves.~~ Done: all seven merged on 2026-11-16 (#1147–#1153). Wave E was proven on the installed service the same day.
3. ~~Merge on green.~~ Done. Next: the follow-ups below that are marked before 1.0, then the run plan's S3 and S4 sessions, then cut v0.11.1.

## Follow-ups found while fixing (2026-11-16)

| ID | What | Recommendation |
|---|---|---|
| T-5 | An HLS conversion keeps encoding for up to 10 min after the viewer leaves. `IdleTimeout` assumes an abandoned session is blocked and free, which holds for a progressive stream but not for HLS, which writes segment files with no backpressure. Measured: The Fifth Element, 4K to 720p with TrueHD, reaped at `idle_seconds=614` after the viewer had switched to the window; the server felt sluggish meanwhile | **Before 1.0.** The player tells the server when it stops, and the server ends that session at once; an in-app delete also stops the title's sessions first. The idle timeout stays as the backstop for a tab that vanishes. |
| T-6 | 31 films hold a provider runtime in `duration_ms` (T-3's race), so an encode of them lists the wrong length | **Before 1.0.** A migration that clears `probed_at` for those rows, plus a startup kick so the probe worker picks them up . |
| T-7 | Deleting a file in Explorer while LANcast is *converting* it fails: ffmpeg opens its input without share-delete, and LANcast cannot change how ffmpeg opens files. R03-4's fix covers what LANcast reads itself | **Known limit.** T-5's stop-on-close removes most of it. Name it in the release notes. |
| T-8 | `useDeleteItem` invalidates lists only `onSuccess`, so a partial delete of a show (409) leaves removed episodes on screen (found by wave E) | **Before 1.0**, XS: `onSettled`. |
| T-9 | `startSemanticPass` answers "started" and then fails, the same shape as R07-10 (found by wave C) | **Before 1.0**, XS: 409 when a pass is running. |
| T-10 | The Settings backup pane could show what a restore cleared (`api_keys_cleared`, `passkeys_cleared`, `two_factor_accounts`) (found by wave C) | **After 1.0.** The restore result already says it. |
| T-11 | The scanner's own walk goroutines are not in R10-10's shutdown group (found by wave C) | **After 1.0.** The window is one statement, as R10-10 recorded. |
| T-12 | One press of Play on a converted film started two HLS sessions and a progressive one within 0.6 s (Prozac Nation, 11:31:19) | **After 1.0**, with R09-9: the same unheld-slot race. |
