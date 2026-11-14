# Whole-app review before 1.0

Started 2026-11-14. This review comes before the run in [v1-run-plan.md](../v1-run-plan.md). It is a read of the whole codebase, done in slices, and every finding goes into one ledger format. Scope stays frozen at v0.11.0. The review finds faults, and fixes for those faults are allowed under the freeze. New features are not, and anything that reads like one goes to the 1.1 backlog.

The codebase is about 234k lines of non-test code: around 145k of Go and 91k of TypeScript, 22k of it the generated `web/src/api/schema.ts`. Nobody can review that in one pass. A useful review depends on three things: slices small enough to read completely, a clear bar for what counts as a finding, and findings that make sense on their own. The model doing the reviewing (Fable) is available for a few days only. A ledger entry has to let any model or person carry out the fix without the session that found it.

## Stages

| Stage | What | Who | Output |
|---|---|---|---|
| 0 | This charter and the slice map | done | this file |
| 1 | Read-only review, one session per slice, in waves | Fable | `findings/RNN.md` per slice, one PR each |
| 2 | Adversarial verify: try to disprove every finding | Fable where possible | a verdict on each entry |
| 3 | Triage: decide 1.0, 1.1 or won't fix for each finding | Chris, one sitting | the triage line on each entry |
| 4 | Fix, one PR per cluster of findings | any model | PRs under the [run-plan rules](../v1-run-plan.md#rules-for-every-unattended-session) |

Stages 1 and 2 can overlap. A slice's verify pass starts as soon as its review PR is up.

## What counts as a finding

A finding is a fault that a user, an operator or a third-party client could actually run into. Each finding needs a concrete failure: *given this input or state, this wrong thing happens.*

**Severity**

- **S1**: a security boundary is crossed, data is lost or corrupted, a locked field or locked decision is overwritten, or the server, the window or the installer crashes or hangs.
- **S2**: a feature is broken on a path real people take, the API contract (`docs/api.md`, `docs/openapi.json`) disagrees with the handler, or the screen stays stale after a write.
- **S3**: wrong but survivable. Examples are a misleading message, a bounded leak, or an edge case few people will reach.

**Not findings.** Leave these out of the ledger:
- style, naming, or refactors that change no behaviour;
- "could be faster" with no measured problem;
- missing features;
- anything inside live TV, which is parked. There is one exception: if `live_tv` being off does not actually close a route, that is an S2 finding.

A reviewer who is unsure whether something is real records it with **Confidence: low**, not as a certainty. The verify stage exists for exactly that case.

## The checklist

These are the faults this project has already shipped at least once, so every reviewer checks the ones that apply to their slice on purpose. In a ledger entry, the **Class** field names the checklist item it falls under, or says `other`.

| Id | Check |
|---|---|
| C1 | **Containment.** Every handler that turns a database row into a filesystem path goes through `internal/contain` (`containedPath`), with links followed (ADR 0094). |
| C2 | **The network boundary.** With no password, the server binds loopback only. CSRF is checked twice, by `SameSite=Strict` *and* by an `Origin`/`Referer` check on every state-changing method. Changing the password deletes every session. |
| C3 | **Bounded input.** The 1 MiB body cap applies before auth. Look for `io.ReadAll` and decoders on network input with no limit elsewhere, and for parsers of untrusted files (m3u, NFO, MilkDrop, cbz/cb7/pdf, ROM headers, EXIF) with no bound on size or depth. |
| C4 | **Locked means locked.** No refresh, rescan or merge touches a locked field or a locked playlist's members. Scanning marks files missing and never deletes them. |
| C5 | **Federation.** A new `/api/federation/` route is on the guest gate list. Guest tickets are scoped to what they were issued for. |
| C6 | **Invalidation.** A mutation invalidates every list it changes. A query key must not be a sibling of the key its callers invalidate. |
| C7 | **The API contract.** Handlers, `docs/api.md`, `docs/openapi.json` and the client types agree on paths, methods, parameters and response shapes. |
| C8 | **The store.** No `*sql.DB` leaves `internal/store`. A write made of several statements runs in a transaction. A one-way migration is marked as one. |
| C9 | **ffmpeg.** Never let ffmpeg pick (`auto`). No code assumes a desktop session (the service runs in session 0). Process execution stays separate from parsing. Child processes are killed and reaped. |
| C10 | **Windows handles.** A file held open for a long time is opened with `FILE_SHARE_DELETE`. |
| C11 | **Dates.** A calendar date the user sees is built from local components, never from `toISOString().slice(0,10)` or the Go equivalent. |
| C12 | **React effects.** No cleanup depends on an object that is rebuilt every render, such as `useMutation()`'s result. |
| C13 | **The window.** No native `confirm()` or `alert()` in the WebView2 window. Use the in-DOM modal. |
| C14 | **One opinion.** Filename guessing lives only in `internal/media`, and title normalization uses only `clean` and `SortTitle`. |
| C15 | **Leaks.** Contexts are cancelled, timers are stopped, response bodies are closed, and every goroutine has an owner that stops it. |
| C16 | **Swallowed errors.** An ignored error on a path where data integrity depends on it. |
| C17 | **Secrets.** No secret appears in a log, URL, error response or crash report. Secret comparisons are constant-time. |
| C18 | **Gold.** Gold appears only for *where you are* ([design.md](../design.md)). This one applies to the client slices only. |

## Slice map

Each slice is sized so that one session can read every line of it. A file belongs to exactly one slice. For `internal/api`, the slice lists the files it owns, and R07 takes everything left over. A reviewer may read outside their slice to follow a call, but findings outside the slice go into a short **Seen in passing** list at the end of their file. They do not become full entries there, so that two ledgers never claim the same fault.

**Wave 1** covers security, data loss and the bug pattern that has shipped most often. **Wave 2** is everything else.

| Slice | Wave | Scope | ~Lines |
|---|---|---|---|
| **R01 Auth and the boundary** | 1 | `internal/auth`, `secret`, `netguard`, `servermode`; api: `auth`, `sessioncookie`, `passkeys`, `twofactor`, `sublogin`, `loginpicker`, `account`, `users`, `apikeys`, `bodylimit`, `invites`, `server` (middleware, CSRF), `network`, `port` | ~5k |
| **R02 Federation** | 1 | `peer`, `identity`, `knownserver`, `presence`, `together`, `remote`, `guestticket`, `certpin`, `tlscert`, `publiccert`; api: `guest*`, `peer*`, `federation*`, `together*`, `rosterpush`, `presence`, `remote`, `identity`, `certfingerprint`, `customcert`, `publiccert` | ~9k |
| **R03 File serving and containment** | 1 | `internal/contain`; api: `stream`, `mediabytes`, `download`, `artwork`, `comics`, `photo`, `book`, `extras`, `lyrics`, `previews`, `optimized`, `preroll`, `romsaves`, `retroachievements`, `raplay`, `webcores`, `served`, `browse`, `backups`, `facethumb`, `dlna`, `transcode`, `streamticket` | ~6k |
| **R04 Store: identity and persistence** | 1 | `store.go`, `migrate.go`, `schema.sql`, `session`, `user`, `access`, `sharing`, `libraryshare`, `invite`, `passkey`, `twofactor`, `apikey`, `retroaccount`, `presencegrant`, `peer`, `restore`, `backup`, `verifycopy`, `retention` | ~6k |
| **R09 Playback on the server** | 1 | `transcode`, `probe`, `subtitle`, `preview`, `optimize`, `livebuf`, `mediatools`, `childproc`, `history` | ~10k |
| **R13 Client data layer** | 1 | `web/src/api` (read `schema.ts` only to check it against the spec), `web/src/lib`, `web/src/focus` | ~11k hand-written |
| R05 Store: everything else | 2 | every `internal/store` file not in R04 | ~19k |
| R06 API: library and playback | 2 | api: `items`, `libraries`, `libraryshare`, `libaccess`, `roots`, `playback`, `playbackpref`, `resume`, `viewings`, `watchlist`, `playlists`, `homeshelves`, `homeprefs`, `showplay`, `afterwards`, `suggestion`, `timeline`, `chapters`, `markers`, `tags`, `rating`, `people`, `memories`, `yearreview`, `history`, `dashhistory` | ~10k |
| R07 API: everything left | 2 | every `internal/api` file not claimed above | ~9k |
| R08 Library pipeline | 2 | `scan`, `media`, `meta` (+`nfo`, `tmdb`), `enrich`, `artwork`, `artistart`, `coverart`, `playlist`, `rating`, `marker`, `lyrics`, `comic` (+`pdf`), `photo`, `faces`, `picture`, `geo` | ~25k |
| R10 Process and lifecycle | 2 | `serverapp`, `service`, `singleton`, `update`, `selfupdate`, `backup`, `plugin`, `config`, `release`, `autostart`, `crashlog`, `applog`, `report`, `webhook`, `tasks`, `events`, `dlna`, `diskuse`, `faceinstall`, `cmd/lancastd` | ~12k |
| R11 Desktop window | 2 | `cmd/lancast`, `clientwindow`, `webview2`, `mpv`, `displaymode`, `vsync`, `desktop`, `desktopprefs`, `raise`, `branding` | ~17k |
| R12 Retro, games and presets | 2 | `retro/*`, `games`, `milkdrop` (+`eel`), `cmd/lancast-faces` | ~17k |
| R14 Client playback | 2 | `web/src/playback` | ~14k |
| R15 Client screens | 2 | `web/src/screens`, `App.tsx`, `main.tsx` | ~20k |
| R16 Client components | 2 | `web/src/components` | ~22k |

Any package not named above goes to the slice nearest to it in purpose. The reviewer says so in their file's header, and the next one to touch this map adds it here.

**A starting lead for R03.** Twenty api files open or serve files (`os.Open`, `http.ServeFile`, `http.ServeContent`), and seven call `containedPath`. Some of the rest serve paths that come from configuration rather than from a library row, such as backups, web cores and pre-roll, and those may be fine. Each of the twenty needs a written answer to one question: *where does this path come from, and what stops it from escaping?*

## The ledger

Each slice writes `docs/review/findings/RNN.md` on its own branch (`review/rNN`) and opens a docs-only PR. No two slices share a file, so the PRs never conflict. A review session changes **no code**. A reviewer may write a test that proves a finding, run it in their worktree, and paste it into the entry, but it is not committed. Committing it belongs to the fix PR.

```markdown
# R03 File serving and containment

Reviewed: <commit sha> · <date> · <model>
Read: <every file read in full; anything skipped, and why>

## R03-1 <one-line title>

- **Severity:** S1 | S2 | S3
- **Class:** C1 … C18 | other
- **Confidence:** high | low
- **Where:** `internal/api/extras.go:88`
- **Failure:** <given this input or state, this wrong thing happens>
- **Evidence:** <the code, a proving test, or a command and its output>
- **Fix sketch:** <a sentence or two; not a patch>
- **Verdict:** <stage 2: CONFIRMED | PLAUSIBLE | REJECTED, and why>
- **Triage:** <stage 3: 1.0 | 1.1 | won't fix>

## Seen in passing

- `internal/store/x.go:12`: <one line> (belongs to R05)

## Checked and clean

- C1: every path in the slice traced to containedPath or to config; see the list below.
```

The **Checked and clean** section matters. It records which checks were done and came up empty, so nobody has to repeat them, and it shows when a check was skipped.

## Running a slice

This is the brief for a slice session. Fill in the slice id.

> Review slice **RNN** from `docs/review/README.md` on a fresh worktree from `main`. Read the charter first, then every file in the slice. Change no code. Write `docs/review/findings/RNN.md` in the ledger format, covering every checklist item that applies to the slice, plus anything else that meets the bar for a finding. Commit only that file on branch `review/rNN` and open a docs-only PR. Then do a verify pass on your own entries: try to disprove each one by reading its callers, guards and tests, and fill in the Verdict line. Leave the Triage line empty.

The verify pass is better done by a second session than by the reviewer. When Fable's budget allows, run one, with this brief: *"Try to disprove every finding in `findings/RNN.md`. A finding survives only if you can name the input that triggers it."*

## How this changes the run to 1.0

- **S1, S5 and S6** in the run plan do not overlap with this review, and run as planned.
- **S3 (security.md) and S4 (architecture.md) wait** until wave 1 is triaged, because the findings will change what those documents need to say.
- **Stage 4 fixes** follow the run-plan rules: one worktree per PR, all three suites green, no `--auto` merges, and anything touching ffmpeg is verified as the service. The first candidate, v0.11.1, is cut after the 1.0 fixes from triage are merged, not before.
