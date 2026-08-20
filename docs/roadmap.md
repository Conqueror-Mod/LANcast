# Roadmap

Last updated: 2026-08-19 · **v0.7.4 released · M0–M4 built.** The React client executes the design
system and the client-UX backlog is closed. Observability (match, review, scan
diagnostics), an audit log and CI are in place. Transport security (TLS) and
multi-user accounts (admin/member roles) are built, and branding & splash shipped.

All of it is released: the repository is **public under MIT**, releases are
**signed**, the client **opens its own window by default**, and the server can
**check for, download, verify and stage an update** that swaps itself in on the
way down. Nothing sits unreleased on `main`. Details in the areas below; what
the pass taught is at the end.

**Music libraries shipped in v0.5.0** ([ADR 0024](adr/0024-music-libraries.md)),
which is the first media type past video and therefore the first real test of
the claim ADR 0002 made: that a new kind needs no new tables. It holds — music
is three new `kind` values on `media_item` related by `parent_id`, exactly as
show → season → episode already was. Metadata inverts the video rule: for a film
the filename is a guess and a provider corrects it, but a music file already
carries the answer in its tags, so tags win and the filename is the fallback.
The release was **server-side only**. Both gaps closed in v0.6.0: album
artwork is extracted, and the client has an album view, a track list, an audio
mode and a docked mini-player. What ADR 0024 scoped is done; **artist images
from a provider are on the back burner** (see below), not because they are hard
but because music has had a long run and the rest of the map has waited.

**Plugin architecture (M4) is built** — the last milestone. A WebAssembly runtime
([ADR 0020](adr/0020-plugin-isolation-boundary.md)) sandboxes third-party code
behind a deny-by-default capability model, plugins register into the same
interfaces the native sources use ([ADR 0007](adr/0007-provider-and-localsource-split.md)),
and a signed-bundle install flow with a two-layer trust model — provenance
(Ed25519 signing) and authority (an explicit capability grant) — surfaces on a
Settings → Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md),
[plan](plugin-distribution-plan.md)). The contract is validated by OMDb
reimplemented as a first-party plugin that produces ratings byte-identical to the
native source. All four founding principles now hold in shipped code.

The **browse-experience backlog shipped** in three PRs
([plan](browse-experience-plan.md)): media-type-aware library views, Plex-style
multi-select filters (genre, decade, content rating) with per-library counts and
an unwatched toggle, and a ratings display with a rating sort. What remains of it
was **external ratings** (Rotten Tomatoes / Metacritic / IMDb via OMDb), specced
in [ADR 0019](adr/0019-external-ratings.md) and now **built** — leaving plugin
architecture (M4) as the last milestone.

The two early-lock Foundation decisions are now **built, not just decided**: the
data model past revision 1 ([ADR 0017](adr/0017-collections-and-multi-part-works.md),
schema at **revision 13**) and the API contract ([ADR 0018](adr/0018-api-contract-and-versioning.md)).
On top of them, **media organisation shipped end to end** — collections, the
show → season → episode hierarchy, multi-part works and serials/miniseries, a
library-kind that drives movie-vs-TV matching, Fix match that reaches TV,
retroactive re-parse on rescan, Play-all queues, and Remove (ignore or delete,
with a sidecar sweep). Theme music (blocked on OST identification) is the
remaining M3 depth. Packaging & distribution is **built** — two branded
executables, a goreleaser matrix and a Windows installer
([ADR 0016](adr/0016-packaging-and-distribution.md), [ADR 0022](adr/0022-client-and-server-executables.md)) —
and has been since v0.3.2.

A **feature backlog is captured below.** With M4 built, what remains is breadth
(finishing music, more plugin kinds, more client surfaces)
rather than foundational milestones.

## Releases

| Version | Date | What shipped |
|---|---|---|
| **v0.7.4** | 2026-08-19 | **The sharing toggle shows the state the server holds.** Reported as the option that lets others see what you have watched not saving when ticked. **It saved.** The reporting machine's database had held `share_activity = 1` throughout; what it could never do was read the value *back*, so the checkbox rendered unticked on every mount however long ago somebody had opted in. `Store.People` excludes the caller by design -- a row for yourself in a list of other people is noise you read past every time -- and the toggle looked for itself in that list anyway, so its own row was always `undefined` and the value fell through to `false`. Local state hid it until you left the pane and came back, and **no route anywhere reported the caller's own setting**: the store has `SharesActivity` and nothing in the API had ever called it. For a privacy control that is the worst direction to be wrong in -- it says you are private while you are sharing, and invites you to switch on something already on. Auth status carries `user.sharing` now, which is where `useSetSharing` was **already invalidating**: the wire was chosen correctly and no value was ever sent down it. **Why it shipped is the part worth keeping**: the server test asserted the *database* after the PUT, so it passed the entire time the only thing a person could see said the opposite -- a round trip is not proven by checking the end you wrote to. Both new tests were demonstrated to fail without the fix, reproducing the reported symptom. The client one then caught a flaw in itself: it flushed microtasks a fixed number of times, enough alone and not enough under a loaded suite, and **only the ticked case could fail that way** because `false` is also the pre-resolution value -- so the unticked assertion had been agreeing for the wrong reason. It waits on the condition now |
| **v0.7.3** | 2026-08-19 | **The home screen fits the screen it is on, and the grid does what you tell it.** The hero was sized against height alone -- `clamp(280px, 31vh, 380px)` -- which on a wide window makes it roughly a **5:1 letterbox**, and a 16:9 backdrop dropped into that with `cover` keeps about 40% of its own height. That is why every hero read as a close-up of the middle of something. It is `max(31vh, 23vw)` now, so a tall window keeps exactly the height it had and a wide one gets a box the artwork can survive; the crop moved off the middle to the top third, where a frame puts its subject. Everything downstream had been sized for the old box: the type column was **52ch**, which on a 1920 window fits every word inside the left third and leaves the other 60% as empty scrim, and the synopsis clamped at **two lines**, which cut essentially every synopsis mid-sentence. The poster scales with the hero rather than staying at 150px -- left fixed inside a taller hero it would have looked *smaller* than before the fix, which is the failure mode of correcting one number in a composition. **Tile size became a control**: a stepped slider in the library header, six steps from 96px to 306px. Discrete rather than continuous, because the grid is `repeat(auto-fill, minmax(N, 1fr))` and most pixel values between two useful column counts render identically -- a continuous slider would spend two thirds of its travel doing nothing visible, which reads as a broken control. The default is **160px, exactly what the grid has always been**, so nobody's library resizes on upgrade; the setting is per device on the bigscreen reasoning, since how big a poster wants to be is a fact about the screen rather than about the person. The size is written on the **grid element, not the document root** -- playlists, search and detail all read the same token, and a root write would mean a size chosen in a movie library silently followed you into them. Fixed: **a container whose children have all gone no longer goes missing with them**. And the design for **Watch Together across two servers** is written down -- a plan and four ADRs, no behaviour change yet. [ADR 0044](adr/0044-server-identity-and-peering.md) gives a server an identity, closing the half of ADR 0014 that ADR 0014 itself named: it encrypts the wire, it does not authenticate the server -- tolerable on a LAN where you walked past the machine, and the whole security property between two households. [ADR 0045](adr/0045-live-presence-between-paired-servers.md) amends ADR 0035, and is written as two changes because only one was invited: sharing with *a named person rather than everybody* is the granularity 0035 parked explicitly and listed first under Revisit when, while disclosing what somebody is watching **right now** is stronger than the resume position 0035 excluded by name, and the ADR says 0035 would not have permitted it rather than dressing it as something left open. What makes it acceptable comes from 0035's own reasoning -- the harm it names is a record that *accumulates*, and presence is never written down -- which is why there is no history and deliberately no "last seen watching", the request that will arrive and would end the ADR without anybody deciding to. [ADR 0046](adr/0046-remote-guests.md) makes a remote person a **principal rather than an account**, admitted by a ticket their own server signs, default-deny in middleware because `requireAuth` grants by default and withdraws by exception, and permitted to stream **only the item the room is playing** -- allow-listing the route is not enough, since that handler streams whatever id it is handed. A guest writes no state at all. [ADR 0047](adr/0047-remote-streaming-is-capped-by-the-host.md) turned out small, because ADR 0031 had already built the mechanism and given the control to the wrong person for this case: capability is the guest's, the ceiling is the host's |
| **v0.7.2** | 2026-08-19 | **The season page is finished, and its episodes have pictures.** Three steps of [season-page-plan.md](season-page-plan.md) at once. **The tick on a row is a control now**: a season page is where somebody corrects watched state — an episode watched on the television, or one the player marked finished while walking a queue — and until now there was no way back from a wrong verdict anywhere in the app. Marking unwatched sends a **position of zero** along with the flag, because leaving the position behind would put the episode straight back on the Continue shelf and the server's own rule (`watched := asked || past the threshold`) would not clear it either. The row had to stop being a button to gain one — a button inside a button is invalid and the browser recovers by dropping one — so it is a container with two focusable siblings, and the mark control **stays visible when off**, since a control that only exists under the mouse is one the focus model cannot find. The season header gains **Continue** and demotes Play all to secondary, matching the show page, with no server change: `NextEpisodeFor` matches episodes by their parent, so a season id answers with that season's next episode. **Episode stills appear**, and the story there is a correction. The plan had claimed they were fetched and thrown away; the running library said otherwise — `item_artwork` held **993 rows of kind thumb**, every Futurama episode among them, and the whole path had worked all along. The tiles were blank because the poster grid asked for `artwork.poster`, which an episode does not have. Which meant v0.7.1 shipped a bug of its own: the new row put the content-addressed **hash** straight into `src` where every other screen passes it through `artworkURL`, and a hash is truthy — so the row took the image branch and rendered a **broken image on all 993 episodes that had a still**, strictly worse than the number it was meant to fall back to. The test had asserted an `<img>` existed rather than what its `src` was; it asserts the exact URL now. And **spoilers became a decision rather than a default**: three settings, hiding the synopsis by default, because an overview is written as a summary and not a tease — the next row down a season list gives away what you were about to watch. The still stays at the default, since a frame rarely gives a plot away and it is what makes a row identifiable at a glance; the strongest setting withholds it too, reusing the typographic state that already existed for missing artwork, so the option cost no new design. One refinement came out of writing the rule: protection applies only to an episode with **no progress at all**, because two minutes in you have already met whatever the first scene gives away, and hiding it then is how a spoiler setting ends up switched off. A withheld synopsis **says so** rather than leaving the line blank — silence reads as missing metadata, which is the exact failure this screen was built to stop looking like |
| **v0.7.1** | 2026-08-19 | **A season reads like a season, and a 10-bit film stops stuttering.** The season page rendered episodes with the movie grid — 2:3 tiles, a title underneath, nothing else — which is the same mistake the music arc corrected for albums, and the reason it looked unfinished was never the missing artwork. Episodes are now **wide rows**: the still on the left, number and title, runtime and air date and rating, the synopsis clamped to two lines, and a progress bar **only when there is progress**. Two absences are the design rather than omissions: an untouched season is not a wall of empty bars, a finished one is not a wall of full ones — watched is a tick and a receding title, because a bar pinned at 100% is a fact nobody needs stated eleven times — and where a still is missing the row draws the **episode number, large, in the space the still will take**. That is why the screen shipped before any artwork work: every row takes that path today and none of them look broken. Pressing a row plays that episode and queues the rest of the season behind it. Alongside it, **a film that stuttered while its audio played perfectly**: HEVC **Main 10**, direct-played, with no ffmpeg running anywhere — the WebView was decoding it and coping badly. The client probes HEVC with `hvc1.1.6.L93.B0`, which is Main profile at **8 bits**, and the server read the resulting claim as covering Main 10 as well. The worst shape of that bug is that nothing fails: a direct play that *errors* records the claim and stops making it, but this played, and no code anywhere can tell a smooth picture from a glitching one. So the client probes `hvc1.2.4.L120.B0` separately and sends **`hevc10`**, which the server treats as permission for a bit depth rather than a codec — exactly as 10-bit H.264 was already handled one codec along. Scoped to *claims*: `tv` and `safari` list HEVC natively because they are device classes known to decode Main 10 in hardware, and demanding a claim from them would re-encode HDR for the clients that handle it best. Also recorded: **[season-page-plan.md](season-page-plan.md)**, which decides that specials sort after every numbered season and — the half that matters — that **Continue must never land on one**, since a special sitting unwatched between two seasons is not what anybody means by "next"; and which sets out five arrangements for whether a season stays a screen at all, proposing the Netflix-style selector with the season in the URL, because this project already holds every browse control in the URL so a filtered view is linkable and survives reload |
| **v0.7.0** | 2026-08-19 | **A minor bump, because browsing and shows both became different things this week.** **Nine filter categories where there were three** — Genre, Decade, Year, Actor, Director, Collection, Content rating, Rating and Status — as buttons that open a panel, with chips where the set is small and a type-ahead where it cannot be listed, and **every applied filter repeated as a removable pill** so a grid narrowed three ways no longer looks like an unfiltered one showing suspiciously little. None of it needed new metadata: `person` and `credit` have carried TMDB cast since M2 and nothing had ever read them for browsing. **Shows became first-class**: Continue watching, Play from start and Randomize episodes, where a show previously offered nothing at all because its children are seasons. Continue is computed from `playback_state` on every press with `no-store` at both ends, and its rule is the first unwatched episode **after the furthest one watched** rather than the earliest unwatched — the difference between the two is the backtracking that makes other players untrustworthy. **ffmpeg installs itself** from Settings ([ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md)), pinned and checksummed, because an install without it appears to work and then does not play things — and produced a report of "AC-3 is not supported" for a codec that had shipped four releases earlier. Alongside those, the fixes that came out of using it: **a finished item starts again instead of resuming past its own end**, which is why pressing play on episode one played episode three — a watched episode keeps a position *after* its final frame, so the queue walked forward through everything already seen; **pressing Back returns you to where you were** rather than the top of the library, the restore having previously given up after 200ms against a grid that takes longer than that to exist; **the library count is the library's size** rather than the page size leaking into the UI; **the subtitle button appears only when there is a track to cycle to**; and **the two faults v0.7.0's predecessor shipped** — a blank detail page from a hook declared below an early return, and an ffmpeg progress bar frozen at 0% by a poll the browser was allowed to cache. Both are now guarded rather than remembered: **eslint runs first in CI** with the react-hooks rules that catch the first in a second, and a **detail page is rendered in a test** with a cold cache, which is the transition that crashed |
| **v0.6.49** | 2026-08-18 | **Two faults shipped in v0.6.48, both mine, both fixed same day.** **Opening any item blanked the app with no way back** — a black window that only a restart recovered. The show buttons declared their two `useState` calls beside the handlers that use them, which sits *below* `if (isLoading) return`, so the first render registered two fewer hooks than the second, React refused to reconcile, and the whole tree unmounted with no error boundary above it to offer a way out. Reported first as a TV-show bug and it looked like one: a film opened from the grid is usually already cached, so `isLoading` is false on its first render and the hook counts never differ, while a show fetches on arrival and gets the loading render. A second household on a cold cache saw it on **every** page, which is what it always was — the fault was latent on every detail screen and only the timing differed. TypeScript cannot see hook ordering and no test renders a detail page; **there is no eslint in this project**, and `react-hooks/rules-of-hooks` catches it in a second, which is the gap worth closing rather than the mistake worth apologising for. **The ffmpeg download's progress bar stayed at 0% while the install succeeded underneath it** — worse than a hang, because the sane response to a frozen bar is to cancel something that was working. The status endpoint sent no cache headers, and every poll is a GET of the same URL with no cache-buster, so the browser reused the first "0 bytes" response for the whole download. The same mistake as a stale continue-watching read, one release after writing that rule down. `no-store` now, asserted by tests, and set **before** the lookup rather than after it, since the response most likely to be kept is the one that went out without a header. Three pieces of hardening that were *not* the cause: `http.DefaultClient` has no timeout of any kind, so a genuine stall would have waited for ever — there is deliberately no total timeout, because 160MB over a slow line is legitimately minutes, so what is detected is **silence**, a watchdog reset on every chunk that abandons a download delivering nothing for a minute and says so in those words; progress is reported before the first byte and then every megabyte rather than every four, which is what tells a slow download from a stuck one at a glance; and **the install logs when it starts**, with its size and URL, because there was no line until it finished and a stall was indistinguishable from never having begun |
| **v0.6.48** | 2026-08-18 | **A show can be put on, and Continue never sends you backwards.** A show was the one container offering nothing — its children are seasons, so the play-all rule found nothing playable and you had to drill into a season to watch anything. It now has three actions, because "put this programme on" is three different questions: **Continue watching**, **Play from start** and **Randomize episodes**. Continue is the one with a rule worth defending, and it is written against a specific failure lived with daily in Plex — press continue on a long-running show and land about **three episodes back**, on something already watched, whether the show has one season or seventeen. That is a stale *read*: the server knows episode 14 was watched and answers 11 because something between the truth and the button holds an older picture of it. So there is **no cache anywhere on the path** — the answer is computed from `playback_state` at the moment it is asked, the handler sends `no-store`, and the client fetches on the press rather than through react-query, whose `staleTime` would reintroduce exactly this intermittently. The rule: an episode **in progress** wins, most recently touched first; otherwise the first unwatched episode **after the furthest one watched** — deliberately not the earliest unwatched, which is the backtracking bug written as a query, since skipping episode 5 and watching through 13 would send you back to 5 on every press. Progress only moves forward. A finished show reports **exhausted** and says so rather than silently replaying the finale, and progress is per user, so one person finishing a season does not move anybody else's place in it. **Play all and Randomize all reach every library that is a queue** — music had them, films and shows never did, and a show library queues *episodes* rather than shows, since a queue of containers is not something a player can advance through. Pictures are deliberately excluded until a slideshow exists to put there. **ffmpeg can be installed from inside the app** ([ADR 0043](adr/0043-media-tools-are-fetched-not-bundled.md)), which is the fix for an install that appears to work and then does not play things: with no ffmpeg nothing is probed, so every file direct-plays by fallback and anything the browser cannot decode is handed to it anyway, Live TV is unavailable entirely, and a household reported **"AC-3 is not supported"** when AC-3 shipped in v0.6.45 and its browser path is an audio re-encode that needs ffmpeg — a dependency that degrades into wrong conclusions about what the software does rather than into a missing button. Pinned to an exact release asset with a **SHA-256 verified before anything is unpacked**, no caller-supplied URL, into the data directory rather than Program Files, with the version, size and licence shown before the download starts. Measured: **160MB compressed, and `ffmpeg.exe` alone is 144MB unpacked**, which is the retroactive argument against bundling it. A partial install reports as **absent** rather than present-and-broken — `ffprobe` moves into place last and `ffprobe` is what detection keys on — and the running server picks the tools up **without a restart**. `mediatools` also now looks **beside the server's own executable** before consulting PATH, which retires the LocalSystem trap rather than documenting it again: a path resolved from `os.Executable()` needs no PATH at all. Two browse fixes: **pressing Back returns you to where you were**, which was reported as the paging resetting — click a film in the Z's, press Back, land in the A's. The restore loop gave up after twelve animation frames, about **200ms**, which is plenty for a detail page and nowhere near enough for a grid of 1,198 posters: the document is one page tall for far longer, `scrollTo` is clamped, and the grid is left at the top. It got worse the larger the library, which is the opposite of what an expiring cache would do. And **the library count is the library's size** rather than how much of it has loaded: "120 of 1,198" was the honest v0.3.2 label for a grid that genuinely stopped at one page, and it outlived the bug it described — 120 is now the page size leaking into the UI, while "Loading more" already says what is arriving, at the bottom edge where somebody waiting is looking |
| **v0.6.47** | 2026-08-18 | **Browsing gets nine filter categories where it had three, and says what it is filtering by.** The old bar laid every value out flat — a dozen genres and eleven decades as chips — which works until the filters worth adding are ones that cannot be listed: a library has thousands of credited people and a century of years. So the bar is now **categories that open a panel**, chips inside where the set is small and a **type-ahead where it is not**, and the new categories are **Actor**, **Director**, **Year**, **Collection**, **Rating** and **Format**, alongside a **Status** group that finally gives *In progress* and *Unmatched* somewhere to live beside the lone *Unwatched* toggle. **None of it needed new metadata**: `person` and `credit` have carried TMDB cast since M2 and nothing had ever read them for browsing, and resolution is a bucket over the width the probe already stored. Under the bar, **every applied filter is repeated as a removable pill** — the half Plex leaves out, where the active filters live inside the dropdown that set them, so a grid narrowed three ways looks like an unfiltered one showing suspiciously little. Three decisions came out of writing the tests rather than the code. Year search matches by **prefix, not substring**, because `99` as a substring also returns 1994 — so typing *more* would widen the list, which is not what typing means. **Actor and Director are separate categories**, because "who is in this" and "who made this" are different questions and an any-role filter answers both without saying which was meant; somebody who does both appears under each, once. And **unrated is not zero** — a rating floor excludes unrated items rather than sinking them, since sweeping them to the bottom would quietly hide the unmatched half of a library behind a control that says nothing about matching. Resolution buckets read **width rather than height**, because a 2.39:1 film at 4K is 3840×1608 against a 16:9 one at 3840×2160 — same format, heights 550px apart — and a height rule demotes every scope film a tier; the boundaries sit below the nominal widths because real 1080p is often 1912 after cropping, and a file with **no width has not been probed** and belongs to no tier rather than to SD. Every category that cannot change the grid is not drawn, and a rating step above what the library holds is not offered. Also: **the CC button stops appearing on files with no subtitles**, where it rendered on every video and did nothing at all — `cycleSub` walks `[null, ...available]`, so with nothing available the click landed and the cycle returned to where it started, and a control that never responds cannot be told apart from a broken one |
| **v0.6.46** | 2026-08-17 | **A live channel that stutters once no longer stutters for ever.** v0.6.41 gave live channels a head start before playing, measured against a real IPTV source that delivers bytes in bursts separated by silences up to **5,071 ms** — HLS segment pacing arriving verbatim, because the server copies video through untouched. That cushion was correct and it was built exactly once: the effect cleared its own timer on the first successful `play()`, and the `<video>` element had no `waiting` handler at all. So the first drought deep enough to empty the buffer spent it permanently — Chromium resumed by itself at `HAVE_FUTURE_DATA`, the very "first burst arrived" condition the original measurement rejected as too little, and since playback runs at the rate the source arrives, **nothing rebuilt the head start**. Every later gap then reached the decoder. It reads as judder rather than as buffering because the spinner was dismissed at startup and never came back, which is why it gets reported as a framerate problem. The hold is now re-armed on `waiting`, and it **pauses while it refills** — an element left playing eats the buffer as fast as it arrives and simply stalls again. It is re-entrant on purpose: `waiting` fires repeatedly on a stalling channel, and restarting the clock each time would push the 12-second deadline out for ever, which is the failure the deadline exists to prevent. The buffering policy itself is untouched — `shouldStartPlayback` and `bufferedAhead` are reused exactly as they were, so there is one rule rather than two that can drift. Three of the four new tests fail against the unfixed client. A channel whose gaps consistently exceed the 8-second cushion will now show **Buffering…** again rather than juddering, which is the honest failure: the droughts are upstream and no client can invent bytes the provider has not published. Also: **pressing Scan updates the counts it changes.** The sidebar could read "TV Shows 15" while the grid beside it read 12 and stay wrong indefinitely, and the button looked dead for up to eight seconds. One cause behind both — starting a scan touched no cache, and the activity poll only refreshes counts when it *observes* work go from active to idle, so a small library that finished between two polls was never seen running at all. The mutation now claims the work itself, which is sound because `Scanner.Start` sets the state to running before the 202 is written |
| **v0.6.45** | 2026-08-17 | **Choosing an audio track on a dual-audio file no longer leaves the player spinning for ever** — and it was never a dual-audio bug, only the thing that exposed one. `empty_moov` writes the moov atom up front so a stream can start before the file is finished, which is the whole point of progressive fMP4; but the MP4 muxer builds the `dac3`/`dec3` box from a *parsed packet*, so it cannot describe AC-3 or E-AC-3 yet, and ffmpeg refuses rather than degrading: **"Cannot write moov atom before EAC3 packets parsed. Could not write header (incorrect codec parameters ?): Invalid argument"**. ffmpeg exits before the first byte while the client is already committed to a `200` and showing a spinner — the same shape as the dead-channel bug of v0.6.39, where a response promises media that never arrives. `delay_moov` holds the moov back until packets have been parsed and satisfies both constraints; ffmpeg names it in its own error text for the AC-3 case. Ten seconds copied out of real files: **E-AC-3 5.1 946 bytes and dead → 8,029,116 bytes; AC-3 946 bytes and dead → 3,343,714 bytes; AAC unchanged either way**. Choosing a non-default track is what triggers it, because that forces a **remux** where the default track would have direct-played — so the failure appears the moment a track is chosen and hides whenever one is not, which is exactly why single-audio files always looked fine. It reaches the `tv` and `safari` profiles, which claim `ac3`/`eac3` and therefore copy; a `browser` profile re-encodes the audio to AAC and was never affected, which is how it survived so much testing. **The same root cause is why a Deep Space Nine season would not play** earlier in this work — h264 + AC-3 in mkv, remuxing for a client that could not direct-play it — so one fix covers both. Verified end to end through the real code path rather than the argument string alone (`DecideTrack` → `Args` → ffmpeg to a pipe, as the server runs it): **42 MB piped, first byte after 39ms**, output carrying the `eac3` stream tagged `eng` — the track that was actually chosen. `probe.mp4CarriesAudio` was right all along that MP4 carries these codecs, so nothing changed in `probe`; the muxer flags were what made the claim false. HLS is untouched and deliberately so — that muxer writes its own init segment after parsing and copying AC-3 through it already worked, now asserted by a test so it cannot grow MP4 flags it does not use |
| **v0.6.44** | 2026-08-17 | **HDR stops being ignored, and stops lying about itself.** LANcast handled HDR by ending the command line in `-pix_fmt yuv420p`, which on a BT.2020 PQ source reduces bit depth and nothing else — no transfer conversion, no gamut mapping, no tone mapping. Worse, ffmpeg copies the source's colour metadata to the output by default, so the delivered file was 8-bit H.264 **claiming to be HDR10**: a client that ignores the tags renders it flat, one that honours them applies a PQ curve to values that were never PQ-encoded, and the result is differently wrong on every display — the shape of bug nobody can reproduce. Not an edge case: HDR content is HEVC Main10, the browser profile excludes HEVC, so every HDR file transcodes for a browser, and a real library holds **35 of them**. Now `Decision.TonemapHDR` rides on the decision (set only on `video_action: "encode"` — a copy delivers the source's own HDR bytes, correctly described as HDR), the filter chain tone maps on the CPU via `zscale`/`hable`, and the output is tagged BT.709 throughout. Three findings on implementation changed **[ADR 0033](adr/0033-hdr-tonemapping.md)**, all measured rather than assumed. **Tagging is not independent of the conversion**, as the ADR had claimed: x264 writes its VUI from the *frame properties* it is handed, and `-color_trc` does not override them, so the flags alone produced `bt709 / smpte2084 / bt2020` — a file whose matrix and transfer disagree, a third and more incoherent wrong state than the one being fixed. `setparams` (core libavfilter, no libzimg) rewrites the properties, giving three states: convert and tag; relabel and tag; or leave the output exactly as before — the last deliberate, because where coherence is unreachable the least wrong action is none. **The tension with [ADR 0032](adr/0032-hardware-decode.md) did not arise**: nothing decodes on the GPU today, so frames are already in system memory and the tone map is a plain filter insertion rather than the download that ADR feared. **The chain had to become a single `-vf`**, since ffmpeg keeps only the last one and a tone map added as a second flag would have replaced the quality-ceiling scale instead of composing with it. Verified on real HDR content per the ADR's own step 5 — a 2160p HDR10 clip through LANcast's own generated arguments, `SATAVG` 2.6 → 4.0 and `YAVG` 40.6 → 35.0, with colour restored and highlights controlled in a side-by-side frame; the magnitudes differ from the ADR's original sample and are not the same measurement repeated. Alongside it, **three filename-parsing defects**, all found by asking why one file scored 0% against TMDB. A parenthetical *before* the year lost its closing bracket, because `clean`'s Trim carries `)` in its cutset for the `[2018]`-style leftovers it was written for and took the inner group's bracket with the trailing space; the surviving `Film (Alternate Cut` is not merely wrong on a tile, because the title *is* the search query — measured from that library's provider cache, `query=…%28Alternate+Cut` returned **0 results** where the same query without the fragment returned the right film first. The bracket is restored rather than the group dropped: `Birdman or (The Unexpected Virtue of Ignorance)` is a real title arriving in exactly the same shape, and a first attempt at the fix turned it into "Birdman or". A **bracketed edition marker** slipped past the end-anchored suffix rule twice — the vocabulary did not carry `alternate cut`, and the closing bracket meant the marker was not last even when it did. And a title *ending* with a vocabulary word was truncated: `The Final Cut (2004)` reduced to **"The"**, matching nothing and sorting into the Ts, which the existing empty-string guard could not catch because "The" is not empty; the new guard is on what survives rather than on the vocabulary, since an article with no noun behind it is not a title anybody has. Also recorded, not built: **[ADR 0041](adr/0041-a-misplaced-file-is-corrected-on-disk.md)** — a misplaced file is corrected on disk, rejecting reparenting because `shapecheck.go` already holds that a show library legitimately contains a few loose files, and because neither motivating case needed it (one needed its `EP1` marker restored, one belonged in the film library); it keeps one small accepted change, that a confirmed match must not lock a row whose shape is still wrong. And **[ADR 0042](adr/0042-two-files-one-work.md)** — two files, one work: a file named `(Alternate Cut)` proved to be a **byte-for-byte copy** of the theatrical file (identical size and identical hashes of its first 1MB, middle 4MB and last 1MB), so the filename asserted an edition that does not exist, which no parser could discover because parsing a name is the thing that believes the name. Thirteen pairs in a 1,209-film library already share a provider id and are five different situations — seven redundant copies, three same-cut-different-encode, one genuine second edition, one film split across two discs, and one outright misfile where a stale `.nfo` put a 1989 film under a 2022 film's identity — so a shared provider id is not evidence of duplication but evidence that something wants a human. `duration_ms` cannot discriminate, being the *provider's* runtime overwritten on match: every such pair reports identical durations whatever the files hold, including the misfile where one film is 126 minutes and the other 177. The decision is to report the collision and not resolve it, keep the edition marker as a label rather than a grouping key, and never merge, rank or delete |
| **v0.6.43** | 2026-08-17 | **A season is no longer searched for by name, which is why nine unrelated shows shared one Thai drama's poster.** Found by driving the app and then reading the live database: season 2 of *The League*, *Black Books*, *Silicon Valley*, *Deep Space Nine*, *The Next Generation*, *Voyager*, *Blue Mountain State* and *It's Always Sunny* all carried the same two artwork hashes, and every episode under them inherited it in the grid. Nothing was wrong with the artwork cache, the merge engine or the scanner — the season rows had been matched, at high confidence, to the wrong shows. A season was enriched like any other item, so its own title went out as the query, and a season's title is a *position* rather than a name: `/search/tv?query=Season+2` is in the provider cache verbatim. TMDB answers that with real shows whose names contain the phrase; the scorer strips non-ASCII and punctuation before comparing, so a Thai title normalizes down to the "season 2" it ends with, the year agreed exactly, and the result scored **0.905** — above the 0.85 auto-apply threshold — so title, year, overview, poster and fanart were written over the season. The decisive property is that **the query depended only on the season number**, so the same wrong show won every time: season 1 → 如果古建筑会说话S01 (0.903), season 2 → รักหลับ กับ ออฟ - กัน season 2 (0.905, **nine times**), season 10 → MTV's 10 on Top, season 12 → Big Zuu's 12 Dishes in 12 Hours, season 13 → Club Friday Season 13, season 15 → Jamie's 15-Minute Meals. The reasoning was already written down — `store.notReviewable` says a season's name "can only ever fail" a name search — but the remedy stopped at hiding seasons from the review queue, the place the cost lands on a person. The failure that mattered was not the search failing; it was the search **succeeding**. Two things kept it invisible: `EnsureSeason` stamps a season resolved at birth so seasons never queue during a scan (the trigger is **Refresh metadata**, which clears every stamp in a library, seasons included), and once poisoned a season is stamped `matched` so it is not pending *and* excluded from review so no human is offered it — the wrong answer was self-sealing. A season is now resolved from the show that owns it or not at all: `tmdb.Search` returns nothing for `KindSeason` without issuing a request, `enrich.fetchSeason` fetches `/tv/{show}/season/{n}` from the parent's id with no search, no scoring and no threshold, `tmdb.Fetch` stops aliasing a season to `fetchShow` so it gets its own name and poster instead of claiming the show's, and `ReparseTargets` excludes seasons for the reason `notReviewable` gives — re-parsing one clears the stamp keeping it out of the queue. A season under an **unmatched** show is left *pending* rather than stamped unmatched, so enriching the show later brings its seasons along instead of stranding them. **Migration 26** strips the identity and artwork from every season a name search matched and clears its stamp; locked seasons are untouched, because a cleanup is a rescan-class event and a rescan does not re-litigate decisions. Verified against a copy of the live database: **20 poisoned seasons and 36 artwork links cleared, 0 remaining**, all 20 requeued. Season sort keys become zero-padded (`season 002`) because the default listing order leads with `sort_title`, where "Season 10" sorts before "Season 2" as text. **[ADR 0040](adr/0040-a-season-is-not-a-searchable-work.md)** records the general rule: an item whose name is a position rather than a title must be resolved by structure, never by search — scoring cannot save a query the scorer is being asked to rank answers to when the question has none, and a confident wrong answer is worse than the failure the threshold was built to prevent. Not fixed here and still open: Deep Space Nine's mixed folder naming produces **four `show` rows for one series**, and duplicate synthetic shows sit beside real ones (Blue Mountain State, Silicon Valley) — ADR 0037 territory. Also unrelated to matching: DS9 S02 is h264 + **ac3** in mkv, which remuxes for video but needs audio transcode to play in a browser |
| **v0.6.42** | 2026-08-17 | **An HLS channel no longer plays at 1.5× speed, and a progress bar stops overrunning its own total.** A channel on a second playlist played visibly fast with a *duration* on the scrubber — `0:16 / 0:28` — which a live stream should not have. The source is an HLS master playlist with three ABR variants, and the HLS demuxer defaults to `live_start_index -3`: three segments back from the edge. Those segments **already exist**, so ffmpeg fetches them as fast as the server serves them and everything downstream receives media faster than real time until the backlog drains. Reproduced outside the app by running LANcast's own arguments for twenty seconds of wall clock: **29.97s of media produced by default (1.50×), 19.97s with `-live_start_index -1` (1.00×)**. Conditional deliberately — the option belongs to the HLS demuxer, and against a plain transport stream ffmpeg refuses the input outright with *"Option live_start_index not found"* rather than ignoring it, so applying it everywhere would turn every tuner and raw stream into a dead channel; an unprobed channel keeps the old path. Separately, the activity panel read **"682 of 449"**: the total was measured once when the run began and never revised while completions kept climbing, and requeueing mid-run is ordinary rather than exceptional — a scan adds rows while enrichment is going, Refresh metadata clears a library, Re-read filenames requeues what it corrected. The total is now done-plus-outstanding and never shrinks, since a bar that jumps backwards reads as a fault in the thing it measures; failures are not added on top because a failed item stays queued and is already counted there. Also recorded, not built: **[ADR 0039](adr/0039-organising-a-large-channel-list.md)** on organising a 1,862-channel list — filtering by playlist, groups that open rather than filter, per-device hidden and favourite channels, and a guide-first grid deferred while no provider in use publishes XMLTV, with virtualising the tile list named and rejected because the ask is not to be *shown* 1,862 tiles |
| **v0.6.41** | 2026-08-17 | **Live channels build a head start before playing**, and the diagnosis is the point. Timing every chunk of the live response body for twenty seconds against a real channel: 376 chunks, gap median **0ms**, p90 **3ms**, max **5,071ms** — bytes arrive in tight bursts separated by multi-second silences, which is HLS segment pacing seen from the far end (ffmpeg pulls a segment as fast as the network allows, then waits for the next to be published). The server copies video through unchanged so it relays that pacing verbatim, and the root cause is upstream: the provider decides when a segment exists. What *is* ours is when playback starts — `canplay` fires at `HAVE_FUTURE_DATA`, which on a bursty source means only "the first burst arrived", so `play()` began with under a second in hand and ran dry at the next silence. Playback now waits for **8 seconds** of buffer, covering the measured drought with margin, with a **12-second deadline** so a channel that trickles still starts; starting late with a short buffer is exactly what the old code did *immediately*, so the fallback is never worse than before. Polled on a timer rather than driven by `progress` events, because those fire on the source's schedule and the source goes silent for seconds at a time, which is precisely when the deadline must fire. **Three explanations this replaces, each falsified by measurement rather than argument:** the muxer's interleave delta (muxing a synthetic source with and without `-max_interleave_delta 0` produced **byte-identical output**, longest single-stream packet run of 2 either way — shipping that as a fix would have been a confident no-op); a WebView2-specific decode fault (Chrome stutters too, 25.4s of media over 41.3s of wall clock with zero dropped frames, and an earlier "Chrome plays it perfectly at 28fps" reading was one lucky sample generalised too far); and per-frame fragment overhead (fragmenting is fine, the stream simply stops arriving). Does not make an upstream deliver evenly — a channel whose silences exceed eight seconds will still stutter |
| **v0.6.40** | 2026-08-17 | **Five fixes, every one found by driving the running app and querying the live library rather than by reading code.** **Blank tiles**, two bugs with one symptom: `libraryTrending` never called `AttachArtwork`, so "Recently Played" rendered **10 items / 0 images** for films whose posters were on screen a few hundred pixels above in a shelf that had attached them — the omission is a line that is *not there*, which is why it survived review, and `TrendingItem` holds its `Item` by value so attaching to the slice directly would have compiled and changed nothing. And **8,443 tracks** had no cover though their album did, which is every music tile on the home page, in Continue Listening and in search, blank beside film posters that worked; `inheritParentPosters` mirrors the existing `inheritArtistPosters` that already walks the other way, and only the poster is inherited since a backdrop belongs to a page *about* an item. **A miniseries was three films**: `Storm.Of.The.Century.[1999].DVDRip.XviD.EP2-BLiTZKRiEG.avi` parsed as a movie because `markerOf` strips noise *before* looking for the ordinal, so `DVDRip` took `EP2` with it — three identically named films in a television library, each searched against film data and each landing in review with nothing to fix. The raw name is now searched only when the tidied one yields nothing, ordered as a fallback so every name that resolves today resolves identically. **One band, one artist**: `Blut Engel`/`Blutengel`, `Box Car Racer`/`Boxcar Racer`, `t.A.T.u`/`t.A.T.u.` and — the pair that settles the argument — `alt-J`/`alt‐J`, differing only by U+002D against U+2010 and *visually identical on screen*, so no amount of care while tagging catches it. Keys fold on letters and digits alone via `media.MergeKey`, deliberately not `SortTitle`, which drops articles and would key "The The" as "the". And **the blank gap on the Downloads page**, measured at 52px: the shared empty state carries padding meant for a grid where it stands alone, and the list container rendered with no rows. Two tests failed on first run and both were right to — the Downloads guard caught a wrongly assumed storage key and surfaced that `readDevice` memoises, so seeding storage after a read is invisible. **The artist and episode fixes need a rescan**; the artwork fixes work immediately |
| **v0.6.39** | 2026-08-17 | **A dead channel says so instead of looking like a broken app** — found by v0.6.38's logging within a second of it shipping: `Server returned 404 Not Found` for a channel whose source had simply gone from the provider's list. The viewer saw none of that, because the response committed to `200 OK` **before any bytes existed**; by the time ffmpeg failed it was already a successful video stream, so an empty body reached the browser and Chromium reported `DEMUXER_ERROR_COULD_NOT_OPEN`. A list of **1,862 channels** will contain dead ones — that is ordinary, and every one of them read as LANcast being broken. The header is now written only once the first byte of video exists, and a source producing nothing answers **`502 channel_unavailable`** with a reason: 404 as a stale list, 401/403 as credentials that may have expired, refused, timed out, unreachable, or unreadable. Anything unrecognised falls back to "could not be opened" rather than inventing a cause, since a confidently wrong one sends somebody to fix the wrong thing and the full text is in the log regardless. Once a byte has been sent there *is* a stream, and a later failure ends the connection rather than changing a status that can no longer be sent. **The upstream URL never reaches a client**: ffmpeg writes it into stderr and provider URLs are routinely credentialed, so only the classification is returned — guarded by two tests from different directions, the classifier against real ffmpeg output and the whole response body scanned end to end, because a single guard on that rule decays the first time a branch is added. Does not explain the stuttering playback still under investigation, but removes from it a channel that was never playing at all |
| **v0.6.38** | 2026-08-17 | **ffmpeg's errors reach the log instead of being discarded.** A live channel that would not play produced exactly one line — `live transcode started` — and nothing else, while the browser reported only `DEMUXER_ERROR_COULD_NOT_OPEN`, which says that what arrived could not be opened and nothing about why. ffmpeg knew: whether the source refused the connection, sent a codec the mux rejected, or died three seconds in. Every session already captured its stderr into a bounded ring buffer and `Session.Stderr()` already existed to read it — **nothing ever called it for a stream**, so the explanation sat in memory until the process exited. Now reported from both paths a session ends by: `Stop` (the client went away, or ffmpeg died and the reader hit EOF) and `reap` (idle, which a session can become *because* ffmpeg stopped producing). Quiet on a healthy stream, since ffmpeg runs at `-loglevel error` and being killed when a viewer closes a tab is not an error it reports. Found while diagnosing a stuttering channel: three theories, two disproved by measurement — Chrome played the same channel at 28fps with **zero dropped frames** and a steady buffer, which ruled out both the muxer's interleave delta and `frag_every_frame` as a general cause — and the one measurement that would have settled any of them was being thrown away on every session. The tests compile a small Go program as the ffmpeg stand-in rather than a shell script, because the existing helper skips on Windows and a test that only runs in CI is one whose failure is found late and by somebody else. Nothing here changes playback; it changes whether the next attempt is evidence or inference |
| **v0.6.37** | 2026-08-17 | **Four filename-parsing defects, found by running a real TV library through the parser rather than by reading it — and three of them meant files that needed no renaming at all.** **`ds9` was read as season 9**: the marker matched the `s9` *inside* the abbreviation, took `.e099` as the episode and truncated the series to `"star trek d"`, filing 78 episodes under a season that does not exist. No error, a plausible answer, and it hits any show abbreviated to letters ending in `s` plus a digit — the worst shape a parse bug takes, and one no fixture would have caught unless it happened to be named after such a show. **A season marker at the *end* of a folder is now a season**: a folder counted only when the marker led, so `Blue Mountain State/BMS S01` became a show of its own while the same series grouped correctly from its other seasons — one show listed twice under two names. That library uses **two conventions across its own seasons** (season 1 carries episode titles and no show name; seasons 2–3 carry the show name and no titles), so the test asserts the two *converge on one series*, since checking them separately would pass while the grid still showed two shows. **A trailing marker no longer survives in a series name** — `Spider-Noir.Season.1.S01E01…` left `"Spider Noir Season 1"`, which matches no show anywhere. And **double episodes keep their title**: `S01E01-E02 - Emissary` was titled "E02 Emissary". The near-miss worth recording: recognising trailing markers initially made a `Show S01` folder sitting *directly under the library root* skip to the root and return no show directory, turning ADR 0037's twenty-shows bug into **zero** shows — there is nothing above such a folder to name the series, so it names itself and the marker comes off the name instead. Four existing scan tests caught that before it shipped. **Needs a rescan** of any affected TV library |
| **v0.6.36** | 2026-08-16 | **Four fixes, three found by driving the app against the real library rather than by reading the source.** **Re-read filenames has a button** — v0.6.34 shipped `/reparse` as an endpoint only, and Settings → Libraries now exposes it, reporting *examined* and *changed* rather than one number, because "0 changed" and "nothing left to examine" mean opposite things and a success state that reads identically to a no-op is not feedback. **A season is no longer offered for review**: the queue listed season rows as "Season 1 · NO MATCH · best 0%" with a Fix button, and a season has no identity of its own — its name is a position within a show rather than the name of a work, so the search cannot succeed, on every season, permanently. `meta.Caps.Supports` routes seasons to the show providers, which is right for fetching a season whose show is known and wrong for searching one by name; shows stay listed, since a show's title is real and a wrong match on one is worth correcting. **The metadata progress figure was mostly photos** — the activity readout showed the same total whatever was scanned, and the number was real rather than stale: **4,238 of 5,492 pending items were photos**, which can never be enriched because `Supports` answers false for `photo` and `gallery`. The queue already excluded music for precisely this reason; the picture kinds were missed when that fix was made, so the general test is now recorded where the list lives — not *did we forget a kind* but *can a provider ever answer for this kind*. (The worker is server-wide, so the figure is the whole backlog rather than the library scanned.) And **correcting a match now changes the picture**: `item_artwork`'s primary key includes the artwork id, so `INSERT OR REPLACE` only replaces when the image is the *same* image — a corrected match downloads a different one, inserting a second row and leaving both selected for one kind. Both readers assign in row order with no `ORDER BY`, so the winner was whatever SQLite returned last and the grid and detail page could disagree about the same item; with the fix reverted the hash assertions still passed and only the count caught it, which is exactly why the symptom appeared in one place and not another on identical data. Existing duplicate rows are not rewritten by upgrading — a Refresh metadata on the affected library re-stores the artwork and the corrected image sticks |
| **v0.6.35** | 2026-08-16 | **Re-parsing a library twice is now actually free.** v0.6.34 shipped `/reparse` claiming a second run would be a no-op, and driving it against the real 1,216-film library disproved that within minutes: 160/98 on the first press, then **99/32 on the second and third**, settling to 0 only when no enrichment had run in between. Enrichment writes the provider's answer back over the guess for any row that stays uncertain, so *"never re-parsed"* and *"re-parsed a minute ago"* were indistinguishable by the only test the code had — the stored title disagrees with the filename either way — and every press rewrote the same 32 rows and re-asked the same provider question. Nothing was corrupted and each cycle needed an operator press, so it was never a runaway; it was a repeating write and a repeating provider call the documentation promised would not happen. A row is re-parsed **once** now: schema revision 25 adds a nullable `media_item.reparsed_at`, targets skip rows carrying it, and the stamp is written whether or not the row changed, since a row the parser already agrees with has been re-parsed just as truly as one that moved. **`?force=true`** re-offers stamped rows, for the one thing the stamp cannot see — the heuristics themselves improving, which is precisely what v0.6.34's folder-year fix was, so it will be wanted again. The claim in `docs/api.md` is corrected rather than quietly dropped. Rows re-parsed under v0.6.34 carry no stamp, so the first run after upgrading examines them once more and then settles |
| **v0.6.34** | 2026-08-16 | **A film's year can live on its folder, and rows guessed by an older parser can be re-parsed.** Found by driving a real 1,216-film library and reading the scores *behind* the Review queue rather than the queue itself. The parser read a movie's year from the filename alone, so the very common `Title (Year)/Title.ext` layout discarded it — `Spiderman (2002)\Spiderman.mp4` parsed as year 0. That is not a weak signal but a **cap**: an absent year scores half credit, so the best reachable total is `0.60 + 0.15 + <0.10`, strictly under the 0.85 auto-accept threshold because the popularity term is asymptotic and never reaches 1. A film in such a library was *arithmetically incapable* of auto-matching with a perfect title and the correct year one directory up — **111 of 140** movies in review were this and nothing else. It also let a wrong identity be **applied**: `Aliens SE (1986)` matched "Alien Sexting" (2020) at 0.683, above the review threshold and therefore merged, because with no year nothing could veto a candidate 34 years off. Only the immediate parent is read; the filename keeps precedence, the library root supplies nothing, and a year *range* names a collection rather than a release so `Alien(1986-2024)` cannot stamp 1986 onto everything beneath it. Because that fix only reaches files added after it, **`POST /api/libraries/{id}/reparse`** re-runs the heuristics over a library's uncertain rows and requeues them — neither a rescan nor a refresh could, since a rescan reconciles *files* and refresh asks the provider the same question when the *question* was broken, and enrichment builds its search from the **stored** title and year. Scope carries the safety: review and unmatched only (a matched row's provider title outranks any filename), locked and local excluded, field locks honoured individually, empty guesses never clearing, and idempotent. Measured on the same library, 130 of 140 recover a year and 114 agree with the provider. No client button yet. Also: a displayed score **floors** rather than rounding, since 0.848 printed as "85%" — the threshold itself — on a row badged *Uncertain*, making a correct decision read as a matcher bug |
| **v0.6.33** | 2026-08-16 | **A music track is not an episode.** Found by driving the profile page on the real library: Pearl Jam's *Black* read **S00E33** and Garbage's *#1 Crush* **S00E14** — disc zero, track thirty-three. A track reuses the show columns (`ApplyTrackTags` writes the album into `series`, the disc into `season`, the track number into `episode`, ADR 0024), so *"has a season and an episode"* is true of every tagged song in the library, and three places formatted an episode code from precisely that test: the profile history rows, the poster tile, and the download filename, which would have named a downloaded song `Album - S00E14 - Title`. The kind is the only thing separating them, so `episodeCode` checks it once and the callers stop deciding for themselves. Profile's own comment already said *"the difference is the library's kind"* and the line beneath it did not act on it — the reasoning was written down and unenforced. Also fixed: **v0.6.32 shipped with CI's gofmt check red.** The `parse.go` edit in v0.6.32 was formatted by `gofmt -w` run from Windows on a CRLF working copy, which computes alignment across the trailing carriage return and produces padding that gofmt on Linux disagrees with; the tag went out anyway. The repo blob was never CRLF — only the working copy is — which is why it is invisible locally: `gofmt -l .` on Windows lists every file in the project, and `git archive` re-applies CRLF on the way out, so both obvious checks lie in the same direction. `git -c core.autocrlf=false archive HEAD` piped into `tar -x` reproduces exactly what CI sees, and names one file |
| **v0.6.32** | 2026-08-16 | **Season folders are recognized even when the number leads the show name.** Found investigating an oversized Review queue on a running server: `reSeasonDir` required an exact "Season N" folder with nothing else, so a scene-release layout like `Star Trek Deep Space Nine/Season 1 - Star Trek Deep Space Nine/...mkv` never matched — the walk stopped one folder early and read the season folder as the show itself, producing one phantom show per season and leaving the real episodes' `series` field garbled, so their metadata search had nothing sane to query (4 phantom shows and 117 unmatched episodes from this series alone). Trailing text after the number is now allowed, but only behind a separator, so a show whose name happens to start the way a season marker does ("S3rvant") still isn't misread as a season. Needs a rescan of any affected TV library to regroup already-scanned episodes under the corrected show — this only fixes how the folder shape is identified going forward. Separately, not a code change: most of the same Review queue's Movies entries (137 of 164 sampled) turned out to be stale — matched before their year was parsed, capped below the auto-match threshold, and never re-scored since nothing revisits a `review`/`unmatched` item once `metadata_updated_at` is set. `Settings → Libraries → Refresh` already clears that stamp and requeues them
| **v0.6.31** | 2026-08-16 | **The home screen catches up to the nav bar's count fix.** v0.6.30 taught the nav bar to count music and picture libraries by what's in them — songs and photos — instead of the artists and galleries that group them; `HomeMasthead` never got the same fix and kept summing the old tile counts, so right below a nav reading the new totals, the "things to play" line and each library's count on the home page still read the old numbers (2,467 against a nav already on 1,209/9,276/4,238/20). Both now share the one `navCount()` rule rather than growing a second one that disagreed with it. Found by running v0.6.30 against a real server
` and produces padding that gofmt on Linux disagrees with. The repo blob was never CRLF; only the working copy is, which is why it is invisible locally — `gofmt -l .` on Windows lists every file and `git archive` re-applies CRLF on the way out, so both obvious checks lie. `git -c core.autocrlf=false archive HEAD \| tar -x` reproduces what CI sees |
| **v0.6.30** | 2026-08-16 | **The nav counts refresh, and each library is counted in the thing it is of.** Removing three files left the nav reading 1,212 beside a grid already on 1,209: `useDeleteItem` invalidated six query keys and claimed to refresh "every list that could be showing the item", and it was three short — invalidating `items` **does not match the grid**, whose key is `["items-infinite", …]`, because invalidation matches by key *prefix* and `items-infinite` is a different string rather than a child of `items`. `libraries` (the nav count) and `facets` were missing outright. And a library's number now answers the question the nav appears to be asking: Music showed 1,171 artists and Photos 67 galleries in the same column as Movies 1,209 films, as though the three were comparable. The server reports `media_count` — the files — alongside `item_count`, which stays exactly the tiles the grid shows, so v0.6.29's count-matches-grid promise is untouched and neither number is redefined; the client reads `media_count` for music and picture libraries. Movies and TV keep the tile count deliberately: a film *is* a tile, and "20 shows" is what somebody means by a TV library. Also closed out this pass, after a spike: **LANcast cannot be renamed in SteelSeries Sonar.** The audio session belongs to Microsoft's WebView2 process, and while `IAudioSessionControl2::SetDisplayName` does rename it — S_OK, reads back, and Plex uses the same mechanism — Sonar keys on the process *image name* and ignored it entirely. The remaining routes are a 250 MB Fixed Version runtime for an undocumented rename, or producing audio outside the web client. Recorded rather than retried |
| **v0.6.29** | 2026-08-16 | **The count counts entities, and v0.6.28's regrouping stops leaving shells behind.** Both found by running the previous release against a real library rather than by reading it. **The library count and the grid were answering different questions**: the sidebar read Movies 1,381 beside a grid of 1,211 — the difference exactly its 170 collections — and Music 1,177 against 1,171, exactly its 6 imported `.m3u` playlists. `libraryItemCount` honoured the top-level rules but never excluded the kinds that *group* items rather than being them, which the grid excludes through `exclude_kind`; so all three of v0.6.28's fixes changed what the grid **showed** and none changed what the count **counted**. `store.GroupingKinds` is the one list now, with a test that fails if it drifts from the SQL predicate beside it. And **pruning empty containers has to reach a fixed point**: v0.6.28's show regrouping was correct, but the sweep after it ran once and a regrouping needs two passes — when the statement evaluates, an old show still holds its old *season* rows, and those seasons are the empty ones, having just lost their episodes to the new show. It deletes the seasons and in the same pass judges the shows as still having children. The real library showed it plainly: TV went **up**, 60 rows to 64, one correct "It's Always Sunny · 8 seasons" beside twenty empty shells of the same name with full metadata and no children. No test caught it because the regrouping test *re-parents* seasons rather than replacing them, so its old shows went childless directly and one pass sufficed |
| **v0.6.28** | 2026-08-16 | **The library counts were wrong, and this is the release that explains why.** Five fixes, all found by reading a real 1,381-film library against the same collection in Plex, and every one of them the same shape: something counted or grouped that should not have been. **A show is a series title, not a directory** ([ADR 0037](adr/0037-show-identity-is-the-series-title.md)) — a series stored as `Show S01`, `Show S02` became one show *per season*, twenty tiles each reading "1 season", because a folder counted as a season folder only when its entire name was a season marker. **Extras are not works** ([ADR 0038](adr/0038-extras-are-not-works.md)): every playable file in a movie library became a movie, `sample.mkv` and `Trailers/` included, with the one condition that a `Shorts` folder *directly* under a library root is a category somebody keeps rather than a film's extras — getting that backwards would discard whole collections. **The collections page only ever showed 120**: it never wired `fetchNextPage`, so every collection after roughly "H" was unreachable with nothing on screen to say so, and the count read the number *loaded*, which is how a truncation reported itself as a total. A **collection of one is no longer a collection** — the ≥2-members rule existed but lived inside the grid's predicate, so the one page dedicated to collections was the one that ignored it. **The music grid shows artists**: tiles named like `00-health-rat_wars-16bit-web-flac-2023` were the `.m3u` files scene releases ship, imported correctly and listed in the wrong place, and `9VoltRevolt`/`9voltRevolt` were two artists because the grouping key was the raw tag. Plus **titles**: quotes that wrap a title come off (only a matching pair, so `'71` keeps its apostrophe), a trailing edition marker comes off so `Alien DC` matches Alien (anchored to the end, because at the front those letters are `DC League of Super-Pets`), and an episode tile names its series. Found by **piloting the running app** rather than by reading the source — none of the paging or listing bugs are visible in code, because each renders as a page that looks finished. **Needs a rescan of Movies, TV and Music**; rows that no longer correspond to anything are marked missing rather than deleted |
| **v0.6.27** | 2026-08-16 | **Live TV works end to end, and four passes of feature work reach a tagged build.** Channels come from an M3U — a provider's or a tuner's — and the upstream URL is never serialised, because those lists are routinely credentialed and publishing one hands out the subscription. **Playback works in any browser** through the ffmpeg pipeline files already use: usually a *remux*, since nearly every channel is H.264 that fMP4 takes as-is, with audio treated the opposite way on purpose — a video encode costs a core per viewer and an audio encode costs a few percent, so audio is copied only when it is *known* to be AAC. The bug that found: **AAC inside MPEG-TS carries ADTS framing MP4 refuses**, so without `-bsf:a aac_adtstoasc` ffmpeg wrote a valid header, rejected the first audio packet and exited — 16 KB where the fix produces 1.05 MB from the same source. **The EPG** ([ADR 0036](adr/0036-epg.md)) adds an optional XMLTV URL per source, now/next on every tile, a schedule under the player, and a search that reads programme titles. Listings attach by **`tvg-id` and nothing else** — name matching attaches "BBC One" listings to "BBC One HD" with total confidence, and that failure is *invisible from the guide*, since every title and time looks plausible and only watching the channel reveals it. Guides refresh themselves every twelve hours where library scans are opt-in, because an unrefreshed guide does not go blank, it goes **wrong**. Also carried here, none of it previously tagged: **watch together**, where the server owns the position and followers converge rather than every client broadcasting and the last writer winning; **private ratings**; the **sharing decision** ([ADR 0035](adr/0035-who-may-see-whose-viewing.md)) — viewing is private until an explicit per-account opt-in, defaulting off, because you cannot un-show a history; **downloads**, **profiles**, **people**, **user management**, a **pop-out player** with the controls browser PiP takes away, **per-library trending** that counts accounts rather than plays, **bigscreen mode**, **rebindable shortcuts**, and **crash reports**. Fixed: an unknown `/api` path answered **200 with an HTML page**, falling through to the SPA fallback — invisible to a browser, which never asks for a route that does not exist, and the least debuggable possible answer for a third-party client |
| **v0.6.26** | 2026-08-15 | **The settings screen stopped hiding half of what it says, and a library location can be browsed for.** Both reported from a running v0.6.25. Every explanatory line under a setting was truncated with an ellipsis — written for the one case where that is right, a long absolute path under a library name, but the class is used about thirty times across five files and almost all of those are prose. Truncating them did not shorten them, it deleted the half carrying the point: "Folders cannot overlap — one inside another would be scanned twice, and which l…" stops exactly where it starts being useful. **Wrapping is the default now and truncation is opt-in**, because that is the direction that fails safe — the old default meant every description silently lost its ending with nothing on the rendered page to say so, and it was losing text on Metadata and Playback as surely as on Libraries. And **adding a location was type-only**: an absolute server path from memory is the one field on that screen a person cannot check as they go, since a typo is accepted, stored, and surfaces later as a location that scans nothing. The add field and each location's move field now open the same folder browser adding a library already used, with move starting at that location's current path. Verified through the built bundle rather than a screenshot — the in-app browser could not render the dev server — which is the honest limit of what was checked before shipping |
| **v0.6.25** | 2026-08-15 | **The release that carries v0.6.23 and v0.6.24**, neither of which was ever tagged — so multi-root libraries, the playback settings panel with its quality ceiling, and the scan that stopped emptying libraries all reach a running server here for the first time. New in this row: a **release group no longer survives into the title**. A file named `veto-beavis.and.butthead.do.america...mkv` was titled "veto beavis and butthead do america", because a scene group is a *trailing* marker on the folder and a *leading* one on the file and only the trailing form was stripped. The naive fix is a catastrophe — stripping any leading `word-` turns `Spider-Man.2002.mkv` into "Man" — and nothing in the filename separates the two cases, so the folder does: the prefix goes only when the containing folder actually ends with it. **HDR files are now identified as HDR**, which they could not be before: `pix_fmt` is the only colour the server recorded and `yuv420p10le` is what HDR10 reports *and* what 10-bit SDR reports, so schema revision 19 records the transfer function, primaries and colour space instead. Three columns rather than one `is_hdr` flag, because the transfer function is the fact and "is this HDR" is a rule applied to it — storing the verdict would mean re-probing a library to change the rule. Nothing reads it yet; it is what a real HDR-to-SDR conversion needs ([ADR 0033](adr/0033-hdr-tonemapping.md)). And the **Libraries pane moved out of Settings.tsx**, which had reached 1,899 lines and 29 components across 12 panes — not for the size but because this screen's own tests record that "a control rendered into a pane nobody can reach is the same failure this project has now made three times", and the pane had just grown again to hold locations. A pure move: the built bundles are byte-identical either side |
| **v0.6.24** | 2026-08-13 | **A library can live in more than one place.** Family films on one drive and the main collection on another were one library by every meaning that mattered — same kind, same rules, wanted in one A–Z — and the schema could only express them as two, which split the rail, split "play all", made collections spanning both impossible and doubled every setting. `library.path` became rows in `library_root`, and **every item records which location it came from** ([ADR 0034](adr/0034-multi-root-libraries.md)). That second half is the design, not an implementation detail: the obvious version asks "does *any* of this library's locations contain this path", which is a **weaker** property than the check it replaces — a row pointing under location B while belonging to A passes on the strength of something matching, and a boundary quietly becomes a search. With the location on the item there is never a search: one location, one check, and only the lookup moves. **Partial availability is ordinary rather than a fault.** A location that cannot be read is skipped and reported — "1 of 2 locations scanned — could not read E:/Family. Items there were left alone, not marked missing" — and only a library with *no* reachable location fails, which is the previous release's unmounted-drive fix generalised rather than replaced. Reconciliation is per location, and that is load-bearing: comparing a partial walk against the *library* finds the other location's files unseen, so walking the first marked the second's missing and then walking the second marked the first's missing — each location sweeping the other, on a healthy server, every scan. **Removing a location deletes its rows where an unreachable drive only marks them missing,** and the distinction is the rule stated properly: "scanning marks missing, never deletes" governs what the server may *infer* from an absent file, not what a person may ask for; a scan deduces and can be wrong, an administrator states. The UI says the count in the question — "Remove 32 items?" — and the last location cannot go at all, disabled with the reason on the control rather than hidden. **Locations may not overlap**, in any library: nesting has no good answer at scan time, since the inner files are walked twice, `media_item.path` is unique so the second write fights the first, and the item's recorded location ends up decided by scan ordering — which is what containment resolves against. Compared by path *component* rather than string prefix, so `films` and `films2` are unrelated, and case-insensitively on Windows where `D:\Media` and `d:\media` are one directory. Three quiet failures were found and fixed on the way, all the same shape and none of which error: the migration would have **cascade-deleted every item, watch position and lock** during the upgrade, because `library.path` carries a UNIQUE constraint, SQLite cannot drop a column through one, and dropping the rebuilt parent table is a *data* operation with foreign keys on; and the hierarchy, music and picture passes each derived structure from the library's *first* location, so an episode, track or photo sitting loose in a second one invented a show, an album or a gallery **named after that drive's own folder**. Moving a location is per location, carrying its contents — the drive-letter case, precisely, where the library-level path edit would have moved the wrong folder on the first library that had two |
| **v0.6.23** | 2026-08-13 | **One playback settings panel, a quality ceiling, and a scan that stopped emptying libraries.** The control strip had grown five separate popovers — speed, audio track, subtitles, and the two that quality and subtitle appearance were about to add — which is not a settings surface but a row of unlabelled glyphs you have to open to find out what they are. They are one panel now, with the transport left outside it and **subtitles keeping a button of their own**: turning them on mid-scene is a transport action, you missed a line and you want them now, not a settings change. Rows are *absent* rather than disabled when the content cannot offer them, because a control that is present and does nothing reads as broken where an absent one reads as unavailable. **Quality was half-built and had been for a while**: `probe.Profile` already carried `MaxHeight` and `MaxVideoBitRate` and `videoCompatible` already enforced them, but nothing could *ask* — `clientProfile` never populated them — and nothing *honoured* it once asked, because `Args` emitted no scale filter and no rate control, so a cap would have changed the delivery decision and then produced an uncapped encode anyway. Both links are closed, and two rules keep it honest: a ceiling **only ever narrows** (the mirror of `?can=`, and why they are separate parameters rather than one knob that could be argued in either direction), and a ceiling **is not a target** — a file already under it is untouched, so no upscale and no rate control that could only ever be slack ([ADR 0031](adr/0031-quality-selection.md)). Preferences are per *device*, not per user: ADR 0006 keys playback state to you because where you are in a film should follow you, while bandwidth on this link, this machine's speakers and subtitle size at this viewing distance are all facts about the screen you are sitting at and would be wrong when roamed. The subtitle offset shifts parsed cues client-side rather than refetching — instant, and it works in *both* directions where the server's `ShiftVTT` only moves cues earlier, which is right for lining up a transcode and wrong as a user control — applied as a delta against what is already applied, or it compounds silently while the readout still says +2.00s. Fixed, and worse than it sounds: **scanning a library whose drive was unmounted marked every item in it missing, and reported the scan as successful.** `filepath.WalkDir` on a root that does not exist calls the walk function once with a nil `DirEntry` and then returns *nil* — a complete success having seen zero files — and reconciliation cannot tell that apart from every file having been deleted. Nothing was deleted, so the letter of "marks missing, never deletes" held while the outcome that rule exists to prevent happened to every row at once, and the periodic scan meant it needed no user action to fire. Two guards now, one before anything is read and one immediately before the only destructive step, the second covering a drive pulled *during* a scan |
| **v0.6.22** | 2026-08-13 | **Search across every library**, the keys where you are, and two player gestures that were each two handlers fighting over one input. Search was per-library, which asks you to know which library a thing is in before you can look for it — the opposite of what a search is for, on a server whose whole job is that you do not have to remember where anything lives. The API could always answer it (`ItemFilter` constrains `library_id` only when it is non-zero) and nothing had ever asked; results group **by library** rather than merging, because "Terminator" matching a film and a soundtrack is two answers and one sorted list would interleave them. `?` shows the keyboard map over whatever is on screen — they were only in Settings, which is where you read *about* the keys and not where you are when you want one — and Settings now renders the same array so the two cannot drift. The **A–Z rail became one component** shared by three grids, with the library still asking the server for its letters (it pages in, so the S titles may not be loaded) and the collections and playlists pages filtering in memory. Fixed: **double-clicking out of fullscreen paused the film** — a double-click fires two clicks *and* dblclick, so play toggled twice and the compensating toggle made it odd again; a click now waits 220ms and a second cancels it. And **Escape closed the player from fullscreen**, leaving a borderless window over the library with the film going in the corner: the case was in the player's own `window` listener while this app resolves Escape centrally on `document`, which fires first, so it could never win — Back knows about fullscreen now instead |
| **v0.6.21** | 2026-08-13 | **The app says when it is older than its server**, plus the loose ends the host-side fullscreen left. The in-app updater replaces the server and the assets it serves and *cannot* replace a running client — a process cannot overwrite the executable it is executing and keep going — so after any release touching the desktop shell the window on screen is the previous version. That was found the hard way: the fullscreen fix shipped, the server updated itself, and the button went on doing nothing because the running window was twenty-six minutes older than the binary on disk, with nothing anywhere saying so. The client carries a version now, injected at release time exactly as the server's is, and the shell says plainly when they differ. The silence is the part with judgement in it and is what the tests cover: a browser has no client version, a `dev` build differs by definition and would nag whoever is working on the thing, and a client too old to report one predates the check — all quiet, because a banner that interrupts everybody has to be right. **Escape leaves fullscreen** (host-side fullscreen is a borderless window, so nothing exited it for us, in the one state where the controls are hidden), the **fullscreen control shows when it is on**, and **double-clicking the picture toggles it**. And the wrong-library-kind warning gets its missing half: a shows library created as a movie library imports everything and *looks fine* — every episode becomes a film with no series and no seasons — so those files are counted and named, never corrected, because the parse is right and the library is wrong |
| **v0.6.20** | 2026-08-13 | **Fullscreen exists.** The previous release fixed *which element* went fullscreen; the controls stayed and the window still never changed size, because WebView2 hands "this page wants fullscreen" to its host and does nothing itself — the host has to resize, drop the frame and cover the taskbar, and nothing here listened. So the page believed it was fullscreen inside a window that had not moved, which is indistinguishable from a dead button. The window does it now through a binding the page calls when it is running in the LANcast window: the ordinary Win32 borderless-fullscreen dance, **on the monitor the window is on** rather than the primary one, restoring the exact placement on the way back — a binding rather than handling WebView2's fullscreen event, which would need a COM interface and an event-handler vtable written by hand to be told about an HWND we already own. In a browser there is no binding and the Fullscreen API is still right. Also: the **A–Z rail stopped disappearing** — it stuck at top 0, which is *behind* the shell's own bar, so the letters slid under it while the filters below stayed; the bar's height is a token now and the strip sits beneath it, with the facet labels gone (GENRE, DECADE and STATUS said what the chips under them already said and cost a line each) since a permanently visible strip is a permanent tax on the grid. And the **page reloads once an update is confirmed**: the server restarts and the page does not, so every script in front of the user was still the old version's and "updated" was true of the server and false of what they were looking at |
| **v0.6.19** | 2026-08-12 | **Fullscreen keeps its controls, and the browse controls stay reachable.** Pressing fullscreen showed the picture and nothing else — not a fault, but fullscreen doing exactly what it was told: `requestFullscreen` was called on the *media surface*, which holds the media element and the cover art, while the player's controls are a sibling of it in the screen the provider renders as children. Fullscreening it therefore hid every control by construction *and* put the mousemove that wakes them outside the fullscreened subtree, so they could not have come back either. It takes the document element now, which contains both; the player screen is already a fixed overlay filling the viewport, so this changes what the viewport is and nothing about the layout. Reported as a second-monitor bug and reproducible on one. The **A–Z rail and the filter row now stick to the top**: a jump list you have to scroll back up to reach costs more than it saves, and they stick as one wrapper because two sticky siblings would overlap each other as they caught. And the **install banner stops outliving the install** — the staged update is gone the moment the server applies it, but `/api/update` is cached for a minute, so the banner kept offering to install what was already installed; the panel re-asks on confirmation and the banner independently hides itself when the staged version is the version already running |
| **v0.6.18** | 2026-08-12 | **Diagnostics, and a floor under the install panel.** The panel that reports an update now has an end it always reaches: 45 seconds after Install is pressed it stops waiting and says it could not confirm which version came back, and to reload. Three releases running had shipped a confident version comparison that was wrong in a way nobody could see until an update ran — and each fix could only take effect on the update *after* the one that shipped it, because the client watching an install is always the client from the version being left. The deadline is a timer rather than a check inside the health effect, which is the detail that matters: that effect is keyed on the version, React Query preserves object identity when data has not changed, and the case the floor exists for is exactly the case where nothing changes. Alongside it, three backlog items the roadmap had named as open. **Debug logging** from the Logs pane, taking effect on the next line logged rather than the next start, and persisted because the faults worth it are the intermittent ones. **Clear cache** and **reset settings**, under one rule that is the whole feature — every action must be recoverable *by the server itself* — so a reset keeps the password hash, the provider keys, the certificate paths and the ffmpeg location, none of which a reset could restore and the first of which would lock the operator out. And two gaps the playlist work left: **rename from the playlist's own page**, which had Delete but not Rename, and tiles that say "3 tracks" rather than "3 items". Also: the SQLITE_BUSY race test stops flaking, fixed rather than annotated — it ran forty reconcile rounds against three writers that never pause, which is a benchmark of the lock rather than the invariant it claims to test |
| **v0.6.17** | 2026-08-12 | **The update panel notices the server came back.** The first real end-to-end update through the new panel worked — downloaded, verified, staged, applied on the way down, service restarted, server back on the new version — and the panel sat on *"Installing…"* for ever on top of it. `staged` is the release tag as GitHub published it (`v0.6.16`); `/api/health` reports the string the build injected (`0.6.16`); the exact comparison between them could never match. The same class of failure the panel was rewritten to remove — the application knowing something and not saying it — reintroduced by the code doing the saying, and missed by a test that used the same string on both sides of the comparison, which is the shape of assumption a test is meant to catch rather than share. Two signals now: versions compared with any leading `v` removed, **and** a version that simply differs from the one running before the restart, because the tag-to-ldflag relationship is a build convention rather than a guarantee and "it came back as something else" is the fact that actually matters |
| **v0.6.16** | 2026-08-12 | **Four things a real library made obvious.** An **A–Z rail** on the browse grid — a jump list rather than a scrollbar, because the grid pages in as you scroll and "jump to S" cannot mean "scroll to a row that has not loaded"; it asks the server for the S titles instead, which behaves the same on a library of nine hundred as on one of nine. Only the letters actually present are offered (the facets endpoint reports them), because a strip of twenty-six where nineteen do nothing is a control that lies about the library — and since the facet query and the filter are two pieces of SQL that could drift, a test walks every offered letter and insists it selects something. **A library can be renamed and moved**: everything is editable except the kind, which is refused rather than ignored, since a kind decides which scanner runs and what the top level of the browse is. The path is editable because the drive-letter case is real and the alternative was deleting the library — throwing away every match, every piece of artwork, every watch position and every playlist that referenced those files to record a fact about a drive letter; repointing rewrites each item path under the old root, and the ignore list with it, in one transaction, marking nothing missing. **Collections got their own page**: they sat in the movie grid among the films they group, which made a curated shelf read as an unsorted one — the grid now excludes them and a control beside Play all leads to a page that asks for exactly those, through one new parameter on the same endpoint rather than a second listing that could disagree. And **Add-ons moved into the rail**, at the foot of the library list and present even with no libraries, because it is a place with contents rather than a setting, and a fresh install is exactly when somebody is looking for what else this thing can do |
| **v0.6.15** | 2026-08-12 | **The update panel stops claiming to be downloading.** Reported from a real install one release after the update work landed: Settings → Updates sat on *"Downloading…"* while the activity indicator three inches away already said the new version was ready, and the workaround was to close the server — the exact experience v0.6.14 existed to remove. Nothing was wrong on the server: `POST /api/update/download` returns immediately and downloads in the background, deliberately, and the panel's status query was cached for a minute with nothing to tell it to look again, so it kept rendering the snapshot taken before the download finished. The activity panel polls, which is why it knew and the panel did not — two surfaces reading the same server and disagreeing, which reads as a hang. The panel now watches while a download is in flight and **stops when it is staged or failed**, because a panel polling an admin endpoint every two seconds for ever is its own bug, and the button shows a **percentage** when the server reports one: a ~16MB download behind a button that says only "Downloading…" for a minute is indistinguishable from one that is stuck, which is how this was reported |
| **v0.6.14** | 2026-08-12 | **An update finishes itself, and a double-click starts the server** ([plan](one-click-plan.md)) — two complaints with one shape: the server did work the user could not see and then waited for them to guess. Clicking Download staged an update and went quiet; on anything but a service install `Restart now` answered *"close LANcast and open it again"*, and nothing afterwards said whether the swap had happened — the only way to find out was to start the server and read the version. A non-service install now **restarts itself**, using the trick the service path already used one level down: a detached `lancastd relaunch <pid> [args…]` waits for the server to exit (which is when staged files are applied, on the way down) and starts it again **with the arguments it had**, so a tray launch comes back as a tray. It waits on the *process*, never a timer — a fixed sleep races a shutdown whose length depends on how many workers must stop — and **gives up rather than starting a second server** over one that will not stop. The panel gained the four states it was collapsing into one, ending in **"Updated to LANcast x.y.z"** confirmed by polling health until the server answers as the new version, and a staged update became a line at the top of the page rather than an entry in the activity list: an activity list is things the server is *doing*, and an update waiting on a decision is not that. Second: with LANcast installed as a service and stopped, **double-clicking the server started a second copy** as the logged-in user against the data directory the service account owns, failing with `attempt to write a readonly database (8)` — a sentence about SQLite with no action in it, whose only route through was an elevated `Start-Service`. A launch now finds the service first and starts *it*, unelevated where possible and behind **one UAC prompt** where not, waiting for the port rather than the service state because "running" means the process started and not that it is listening. That is the only thing LANcast will ever elevate for. Startup failures now carry an action and keep the underlying error, an unfamiliar one passes through untouched, and the tray gained Check for updates |
| **v0.6.13** | 2026-08-12 | **Five server-side rules in Settings** — the first settings that decide what a client *shows* and what it may *do*, rather than which key to call an API with. **Counts as watched at** (90%) is applied on every progress write, so a client that never sends `watched` still gets correct state; it is OR-ed with the client's flag and never overrides it downward, because a player that fired `ended` knows something the server cannot see while one claiming "not watched" at 98% has an out-of-date idea of finished — and an item with no duration is never marked, since a percentage of an unknown length is not a fact about anything. **Weeks to keep in Continue Watching** (16, 0 = forever) and **Items in Continue Watching** (40) decide what that shelf holds; the window filters in SQL so the limit applies to what survives it, because trimming afterwards reproduces the exact bug the window fixes — forty things you abandoned in March with last night's pause pushed off the end. **Allow deleting media files from disk** (on) is the switch that decides whether this server can destroy media at all: off makes `?mode=delete` a 403 before anything is even looked up, while `?mode=ignore` is untouched because it writes no file. Every install could delete from disk through the API until now, and "no" was not a thing an operator could say. **Rescan libraries automatically** (off) runs a timer that re-reads its interval each tick, so a change takes effect without a restart, and skips a library already scanning rather than queueing behind it. Ranges are **rejected with 400 rather than clamped** — a client sending 200 has a bug and silently storing 90 hides it — and the config file is separately repaired on load, because it is hand-editable and a hand-edited `0` threshold would mark an entire library watched the moment each item started playing. The client tests assert the controls sit on panes a person can *reach*, which is the failure this project shipped three times running |
| **v0.6.12** | 2026-08-12 | **A playlists page** ([plan](playlists-page-plan.md)) — playlists could be made, edited and played, and were nearly impossible to find: a playlist is filed in a library whose top level is artists, so nothing listed one and you reached it by a search that happened to match its name. That is the same failure as a button on a page nothing navigates to, one level up, and the third time in three releases this shape of bug has surfaced. The page is **per-library**, at `/library/{id}/playlists`, entered beside Play all and Shuffle and gated on a config flag so it appears where playlists are actually kept — a playlist belongs to the library its tracks and its `.m3u` live in, and a global page would have to invent a grouping across libraries or silently mix them. **New playlist** and **rename** land here; renaming has been a `PATCH` the API accepted since M2 that no client had ever called, and until now a playlist could only be born as a side effect of adding a track to it. **`child_count` for a playlist is now its entry count**, read from `playlist_entry` — it was 0 by design, which was true about the implementation and useless to a client that reads the field as "how many things are in this"; repeats count, and so do entries whose file is missing, because a playlist that shortens itself when a drive is unplugged is the failure that marks-missing-never-deletes exists to prevent. Tiles compose their own artwork from the first few entries' covers, client-side, since no provider will ever have an opinion about what a list called "The Gym One" looks like. Also three things the screens said wrongly, all found by looking at them: **Fix match was offered on a playlist**, which has no identity to correct; the meta line said **"1 tracks"** under every single-track album; and a **deliberate repeat was annotated with its filename**, a hint that belongs to the mis-tagged album and claims something is wrong with the one case where two identical rows are correct |
| **v0.6.11** | 2026-08-12 | **Add to playlist, where a track can be reached.** v0.6.10 put the control on the item detail page, which for music is a page nothing navigates to: tracks are rows in a track list and never poster tiles, so `/item/{id}` for a track is unreachable. The capability shipped and no one with a music library could press it — the same failure the admin remove control had, described in a comment three lines above the change that repeated it, which is the second time this exact mistake has been made and the reason the new test asserts *reachability* rather than behaviour. The control now sits on the **track row**, on every track list; in the **full player**, outside the queue-length gate, because a single track playing with nothing queued behind it is the most ordinary thing to want on a list and is the one case that gate excluded; and in the **mini-player** for audio, since "put this on a list" is a thought you have while a song is playing, which is exactly when the track is not on screen anywhere else. Also plans the **playlists page** ([plan](playlists-page-plan.md)) — a playlist is filed in a library whose top level is artists, so nothing lists them and they are findable only by a search that happens to match; that is the same bug one level up and it needs a screen |
| **v0.6.10** | 2026-08-12 | **Playlist editing** — v0.6.9 shipped playlists that could be imported and played and not changed, which is half a feature: a list you can look at is not a list you keep. Five writes close it — create, add, reorder, remove an entry, delete the list — and every one of them **locks `members` before writing**, because that lock is the only thing standing between an edit and the next scan re-importing the `.m3u` over it. Verified against the real test libraries: a rescan re-imported the untouched playlist beside it, left the edited one exactly as edited, and did not write a byte to either file on disk. Entries are addressed by **position**, not by item id, for the same reason the queue cursor is — an id does not name a row in the one listing that may hold the same id twice — and a removal resequences so a position stays the index the client rendered. Deleting a playlist is its own route rather than `DELETE /api/items/{id}`, whose modes are about *files*: one would delete the seeding `.m3u`, the other ignore-list it. The writes need a session but no particular role, since editing a list touches no file and no library and the audit log records who did it. In the client, playlist rows gained reorder and remove-from-list controls (buttons, not a drag: a d-pad cannot drag), numbered by **position** rather than by the track numbers they carried on their own records — a playlist drawn from six albums was numbering itself 1, 4, 1, 9 — and no longer split into disc headings that described the records rather than the list. Any playable item gained **Add to playlist**, which appends and can create the list on the way. Also: **docs/api.md is now checked against the router on every build**, in both directions — the document third-party clients build against, which had drifted three ways at once before anything watched it |
| **v0.6.9** | 2026-08-12 | **Playlists** ([ADR 0030](adr/0030-playlists-and-m3u.md)) — the `.m3u` files already sitting in a music library become playlists on scan, browsable and playable like anything else. A playlist is `kind = 'playlist'` on `media_item`, no new *item* table — but membership needed one, and the reason is the whole design: `item_collection` is keyed `(item_id, collection_id)` and physically cannot hold the same track twice, which is ordinary in a playlist and impossible in a collection. `playlist_entry` keys on **position** instead (schema 17), and that repeat case forced the play queue's cursor to become a position too — `indexOf` always finds the first occurrence, so a track appearing twice used to send playback backwards and strand everything after it. An `.m3u` is an **import, not a mirror**: the database is the truth, editing a playlist locks its membership so a rescan cannot undo the edit, unresolvable lines are counted and reported rather than dropped in silence, and LANcast's own HLS output is refused rather than imported as somebody's mixtape. Also: **a settings page with categories** down the left instead of one long scroll, grouped by whose setting it is — server versus this device — because that is the distinction that matters once two people share a server; it exposed a General pane with the server and API version, and controls for the provider rate limit and update check, both of which the API had accepted and validated since the beginning with no way to reach them. Fixed: shuffle stranded most of the queue (starting mid-order meant the 690 tracks in front of you never played), the queue panel described the unshuffled order and never scrolled to what was playing, and a trip through the mini-player collapsed any queue to a single track — taking every skip, shuffle and repeat control with it. The client gained its first test suite, in CI |
| **v0.6.8** | 2026-08-11 | A testing pass over the player and music, and three faults that shared a shape: present, plausible, and doing nothing. **The audio track picker had never rendered** in the project's history — nothing in the library carried two audio tracks — and behind that gate sat three bugs, one of which killed playback outright: the client never sent `?audio=` to the delivery decision, so the server answered about the default track and the raw byte serve could not honour the choice; `DecideTrack` would have direct-played a chosen alternate track it cannot select; and seeking dropped the index, refusing with a silent 409 mid-seek. **Music sorting** looked broken because a music library's top level is artists and an artist row has no year, so "Year" ordered by a column that is NULL for every row and returned the same list as "Title". **Deleting a track** was permitted, endpoint-backed and dialog-served with no path to it — a capability you cannot invoke is one you do not have; there is a control on the row now, and colliding titles show their filename, because two identically tagged rows each with a delete button is a coin flip whose losing side takes the correctly tagged file. **The whole music library can be put on**, in order or shuffled from a random start, with the queue handed over in history state because every track of a library is far too much URL. Also: `setPositionState` gives picture-in-picture and the Windows media overlay a true clock on a transcode — the overlay had never worked at all; one text track may be showing, ending cues from two subtitle files stacking up the screen; Back sticks and restores its scroll position; the account group moves to the foot of the rail, which now auto-hides after a choice; an artist gets Play all and a square sleeve; and the client has its first test suite, in CI, written to answer whether the media element survives a cross-document move ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md)) — it does not, unless it is the only child of a slot, which it now is |
| **v0.6.7** | 2026-08-10 | The navigation stands up. Library names ran horizontally across the top, competing with each other and with the account controls for one strip of pixels, so a fourth library made the third one shorter and a long name was truncated by the existence of its neighbours. They move to a rail down the left, one per line, counts in their own column. **It collapses to icons and expands on hover or focus** — the backlog's Plex behaviour — and expands *over* the content rather than pushing it, because a page that slides whenever the pointer crosses the left edge is harder to use than a narrow rail. Labels fade rather than being removed, so a screen reader and in-page search still find them; the glyphs are drawn in twenty lines rather than pulled from an icon font, for the reason hls.js is not vendored. Verified collapsed and expanded, with the expanded state driven by **focus** rather than hover — the keyboard path is the one that would have broken silently, since `:focus-within` is what makes the rail reachable without a mouse |
| **v0.6.6** | 2026-08-09 | A staged update finally has somewhere to go. LANcast applies an update as the server shuts down, and when it runs as a Windows service nothing ever shuts it down — so "it takes effect the next time the server starts" described a moment that never arrives, and the only route through was an elevated `Stop-Service`, which applied the update correctly and left the machine with LANcast not running at all. The swap was never at fault: on the reporting machine the new binaries were in place, the old ones set aside, the staging directory consumed. What was missing was the step from *installed* to *running*. `POST /api/update/restart` now spawns a detached helper — the same binary, `service restart` — which stops the service, **waits for the stop to complete**, and starts it again; the wait is load-bearing, because `Start` on a service still stopping fails, and it would fail after the old version had already gone. Scoped as "finish the update" rather than "restart the server", and a non-service install refuses and says to close and reopen instead, because killing a process nothing will bring back is the failure being fixed |
| **v0.6.5** | 2026-08-09 | **Pictures** ([ADR 0028](adr/0028-pictures-library.md), [plan](pictures-plan.md)) — the third media type, and the second test of ADR 0002's no-new-tables claim, which holds again: gallery → photo on `media_item`, one nullable column (`taken_at`) at schema 16. The design follows from one fact — a photo *is* its own artwork, where every other media type points at an image representing it — so thumbnails are generated into the existing content-addressed cache by their own worker, and the cache is handed a 1600px copy rather than the original, because storing what it is given would put a second copy of the library on disk. Folders become galleries because in a picture library the folder is the only grouping that means anything: the filenames are UUIDs and there is no provider to ask, so titles are stored verbatim — a name that means nothing beats a tidied version of one. The library opens on a banner cycling the library, a gallery on a banner over its photos; pressing a photo selects it into the banner rather than navigating, since a photograph has no detail page worth visiting. Expand opens a viewer that owns the keyboard, restores focus on escape, and never auto-starts its slideshow. EXIF gives orientation and capture date; **GPS is never read**, because the surest way not to leak location data is not to load it. Also fixed: every rescan of a music library had been re-recording every track and re-queueing the whole library for enrichment since v0.5.0 — found by a picture test asking a new kind an old question |
| **v0.6.4** | 2026-08-09 | The in-app updater could find a new version and never install one, found by pressing the button on the first release it could have installed. Two faults. The release lookup asked GitHub's JSON endpoint for `application/octet-stream` and got 415 — the check path had it right, so checking worked, finding the release worked, and only fetching it was impossible. And the failure had nowhere to go: the download runs detached from the request that starts it, so the error reached the log and nothing else, leaving the panel on "Downloading…" indefinitely. A download that died half an hour ago was indistinguishable from a slow one. `download_error` is now reported and rendered separately from a failed check. **The tests could not have caught the first fault** — the fake releases server accepted any `Accept` header, so a downloader that asked wrongly passed every test and failed against the only server that matters. The fake is now as strict as GitHub on that dimension, verified by reintroducing the bug and watching the suite fail with the same 415. Installed by hand, necessarily: a broken updater cannot deliver its own fix |
| **v0.6.3** | 2026-08-09 | A home page worth opening, and two libraries that stop failing quietly. Home now opens on a **spotlight** — the thing you are part-way through, full-bleed artwork behind a floating poster, with a Resume button; failing that, the newest addition. The screen gained **depth built from everything except colour** ([ADR 0027](adr/0027-depth-in-the-canonical-look.md)): shadows cast in the void colour so a raised object reads as further from the same field, artwork tinted into the nebula rather than pasted on top of it, and a backdrop that parallaxes behind the shelves. Gold is untouched, because the ring is the focus indicator and diluting it costs an accessibility affordance rather than a look. **Listening separated from watching** — Continue Listening and New Music are their own rows, since a square sleeve beside a 2:3 poster is a row with no shared baseline, and a half-played track among films reads as broken films. On the library side, a **kind mismatch stops being silent**: a music folder added as a Movies library scanned 1,592 tracks, imported none and reported "0 items · scanned", which reads as an empty folder; it now names what it ignored and why, and the library-type field no longer has a default, because the choice is permanent and anything selectable by inattention eventually will be. Fixed: the Start menu's server shortcut carried a data-directory argument that never expanded — NSIS resolves `$%VAR%` at compile time, on a Linux runner — so starting the server that way opened a second, empty database beside the install |
| **v0.6.2** | 2026-08-09 | LANcast updates itself, opens in its own window by default, and the source is public under MIT. **The first signed release** — a signature over the checksum list, verified against a key compiled into the binary, which is what makes automatic installation defensible rather than merely convenient: installing an update is a system-level process executing a downloaded binary, and without proof of origin that is a hole. Three outcomes stay distinct — signed installs automatically, unsigned is offered for a manual install, present-and-wrong is refused before anything is downloaded. The install is staged and swapped on the way down, so it is one restart with no elevation prompt and no second process. The check is on by default and is not a phone-home exception: a plain GET with no install identifier, statistics or history, switchable off, with a manual check that still works. The `-window` flip landed here too, with `-browser` as the opt-out and the installer offering both. Fixes: a database handle held open after a stop that raced startup (which is what makes an installer's file replacement fail), and an NFO edit that claimed the whole file rather than the field that changed |
| **v0.6.1** | 2026-08-09 | A day of fixes, an audit log, and desktop lifecycle controls — two of the fixes for problems that never produced an error. Scans stopped dying with `database is locked` when enrichment committed mid-scan; new films and shows are enriched again on any server with a music library, where un-enrichable rows had blocked the queue permanently (a "remaining" count of 2,198 that never moved became 7); the service stops when told rather than being judged a hang and restarted by its own recovery policy; and a sidecar is written only for an identity actually established, so a wrong title can no longer outlive the database that produced it. The **audit log** ([ADR 0026](adr/0026-audit-log.md)) records who changed what where the mutation is authorised — libraries, titles, matches, accounts, add-on trust — readable from Settings, with browsing and playback deliberately excluded. **Desktop lifecycle** shipped: close to tray and open when Windows starts, both off by default, both shown only in the LANcast window. Close-to-tray had shipped disabled on the belief that the tray and the web view fight over the message loop; they do not — Windows message queues are per thread, and the conflict existed only because both were run from one goroutine |
| **v0.6.0** | 2026-08-08 | Music becomes a client experience, LANcast gets its own window, and the server says what it is doing. The music UI v0.5.0 left unbuilt — artist and album tiles, a numbered track list in playing order, an audio mode, a docked mini-player — plus album artwork off the disk (369 of 398 albums, 10.7s, no network) and album artist/year derived from tracks. `-window` opens a WebView2 window that pins the server's own certificate, which matters because a web view does not warn on a bad certificate, it refuses to load. Clients now declare what they can decode, ending the full re-encode of every HEVC file. An activity indicator and a log viewer make background work visible. Two fixes found by using it: scans aborting with `database is locked` when enrichment committed mid-scan (SQLITE_BUSY_SNAPSHOT, which `busy_timeout` does not cover), and new items never being enriched at all on any server with a music library, because un-enrichable rows sat at the head of the queue and the worker stopped at the first unproductive batch |
| **v0.5.0** | 2026-08-03 | Music libraries — the first media type past video, and the first test of ADR 0002's claim that a new kind needs no new tables (it holds: three `kind` values on `media_item`, schema 13). Scans eleven audio formats, reads embedded tags as the authority rather than guessing from filenames, and groups tracks into artists and albums by *album artist* so compilations stay whole. Playback profiles gained audio containers, which is what stopped a `.flac` being re-encoded to AAC to deliver a format every browser plays natively. Server-side only: no music player in the client, no album artwork yet. |
| **v0.4.3** | 2026-08-02 | The two guards that keep the server honest, both broken in ways only a real install showed. The cross-session single-instance check failed open: Windows returns the same "access denied" whether an object exists and may not be opened or the caller cannot create it, so a desktop launch never saw a server running as a service and started a second one — the mechanism behind the two-servers and, before v0.4.1, two-databases problems. And v0.4.2's service log wrote nothing, because it was paired with a console that does not exist under the service manager using a writer that gives up on the first failure. |
| **v0.4.2** | 2026-08-02 | Makes a service-run server diagnosable. It had no console and no inherited stderr, so everything it logged was discarded by the operating system — when it exited, the only record anywhere was Windows' own "terminated unexpectedly", which cannot tell a crash apart from a kill. It now writes `lancastd.log` beside the database, one rolled generation capped at 4 MB, and every exit goes through it including a refusal to start. New installs also restart three times after an unexpected exit and then stop, rather than staying down unnoticed or looping on a server that genuinely cannot start. |
| **v0.4.1** | 2026-08-02 | Windows run environment, all of it found by installing v0.4.0 and using it. Child processes no longer open console windows — the app flashed three or four on launch and one per file on a scan, because a windowsgui parent gives every ffmpeg child a visible console. The HTTPS redirect is temporary rather than permanent: browsers cached the 301 forever, so a server that later dropped back to plain HTTP was unreachable with `ERR_SSL_PROTOCOL_ERROR`. The client no longer starts a server on a second, per-user database while the service uses the machine-wide one. Separate **LANcast Client** and **LANcast Server** Start menu entries. Add-library focuses its first field. |
| **v0.4.0** | 2026-08-02 | Playback decisions rewritten after a second real-library test: the chosen audio track now drives the decision (picking one produced `-c:a copy` on undecodable audio and silent playback), named client profiles so HEVC stops forcing a re-encode, copy gated on what MP4 can actually carry, `pix_fmt`-based 10-bit detection (schema 12), and audio no longer re-encoded alongside video for free. Adds **Re-read media files** for libraries probed by an older build, and `lancastd reset-auth` for lockout recovery. Fixes: the app opening to a tray icon and no window, NFO sidecars growing on every write, shows libraries counting seasons and episodes as items, a certificate warning on a loopback-only server, and a restart prompt that could not deliver. |
| **v0.3.2** | 2026-08-02 | First published release. M0–M4 plus packaging: two executables, Windows installer, service install. Fixes from the first real-library test — ffprobe unreachable under a service (which had left every file direct-played), a grid that stopped at 120 of 1,226, volume, filenames for Fix match, and two upgrade-path bugs. |

**What real-library testing taught, worth remembering:** every serious bug in
this release was invisible rather than loud. Nothing was probed and playback
silently degraded; the grid truncated under a count claiming the full total; the
launcher read a TLS error as "server down"; an old process survived an upgrade
and held a lock. The fixes each added a way to *see* the failure — a media-tools
row in Settings, an honest "120 of 1,226", a message box instead of a silent
exit. Prefer that to a quiet fallback.

**v0.4.0 repeated it, and added one.** The audio-track bug played films with no
sound and logged nothing; an impossible mux made ffmpeg refuse to start and the
player just died; NFO sidecars grew on every write and nobody would ever have
noticed. Same shape: the failure had no voice.

The addition is about *how the bugs were found*. Two claims made from reading
the code were wrong — a re-probe described as taking "hours" turned out to take
15 seconds per 225 files, and a shows library's item count was explained as
correct arithmetic when it was plainly wrong on screen. Both were caught by
running the thing and looking at the output. Four of the release's fixes were
found while verifying something else. **Reasoning about the code predicts; only
running it against real files reports.**

**v0.4.1 is the same lesson at the next layer down.** Every bug in it was found
by installing v0.4.0 and using it as a user would — not by reading code, not by
tests, all of which passed. Console windows flashing on launch, a browser that
would not connect, an empty second database, a Start menu entry that hid which
program it ran: none of these are visible from inside the repository, and none
of them would ever fail a test. The unit of verification that catches this
class is *the installed artifact on a real desktop*, and it deserves a pass of
its own before a release is called good.

**v0.4.2 and v0.4.3 close the loop by turning it on the diagnostics.** The
service log added in v0.4.2 wrote nothing under the service manager — it was
tested in a terminal, which is the one environment it is not for. The
single-instance guard had been failing open since it was written, and was found
only because a real service happened to be running during an unrelated test.
Both are the same mistake in a new place: **the environment a check runs in is
part of the check.** A guard verified somewhere easier than production has not
been verified.

**v0.5.0 adds a different shape: a fix with two halves, one of them invisible.**
ADR 0024 named the audio-container problem precisely and pointed at
`probe.Profile`. Adding the containers there was correct and, on its own, would
have done nothing for `.m4a` or `.opus` — because decisions are made from the
*stored* extension mapped into ffprobe's vocabulary by `containerFromExtension`,
and that mapping knew only video. Profiles would have listed `ogg` while every
`.opus` file still arrived as `opus` and matched nothing. The tests would have
passed, the ADR would have been satisfied, and those files would have transcoded
forever with no stated reason. **An ADR names the decision, not the full set of
places the decision touches.** The second half was found by asking what actually
produces the value being compared, rather than trusting that the obvious place
was the only place.

## Ordering principle

**Plan an area immediately before building it, not long before.** Specifying the
plugin contract today would mean designing an extension API with no extensions
to validate it against — which is exactly how these projects calcify around the
wrong abstractions.

The exception is anything that constrains the **schema** or the **API
contract**. Those get decided early because they are expensive to retrofit. Two
are already handled in schema revision 1: `playback_state` carries a `user_id`
before multi-user exists, and `media_item` does not hardcode a media taxonomy.

## Milestones

| | Milestone | Definition of done | |
|---|---|---|---|
| M0 | Library scan | Point at a folder, get rows in a database | **done** |
| M1 | **Watch something** | Browse in a browser, click, play, seek, resume | **done** |
| M2 | Metadata | Real titles, artwork, seasons, OST identification | **done** |
| M3 | Transcoding + real client | Plays anywhere; React client executes the design | **done** |
| M4 | Extensibility | Plugin runtime with first-party plugins proving the contract | **done** |

M1 is the milestone that matters. Everything before it is scaffolding and
everything after it is depth.

## Areas

Status: **planned** · **next** · *unplanned*

### Foundation · M0–M1

| Area | Status | Note |
|---|---|---|
| Server core architecture | **built** | Go, SQLite, scan → browse → play |
| UI/UX design system | **built** | Nebula field, gold rule, keyboard model — executed by the React client, not just the tokens |
| Data model evolution and migrations | **built** | Forward-only migrations (rev 1→13); collections, hierarchy, multi-part & serial works ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| API contract and versioning | **built** | URL-path versioning, `/api` ≡ v1, additive-safe rule ([ADR 0018](adr/0018-api-contract-and-versioning.md)); `child_count`, `collection_id`, cross-type match |

### Metadata and artwork · M2

| Area | Status | Note |
|---|---|---|
| Provider interface | **built** | Scraper contract; first real extension point |
| Matching and confidence | **built** | Wrong-match correction; library-kind biases movie-vs-TV; Fix match reaches TV, not just film |
| Media organisation | **built** | Collections, show→season→episode, multi-part works, serials/miniseries; retroactive re-parse; Remove (ignore/delete) ([ADR 0017](adr/0017-collections-and-multi-part-works.md)) |
| Artwork pipeline | **built** | Fetch, cache, resize; fanart for detail pages; art-less children inherit the parent poster |
| External ratings | **built** | RT / Metacritic / IMDb via OMDb; `RatingSource` + `item_rating` side table + `imdb_id` ([ADR 0019](adr/0019-external-ratings.md)) |
| OST identification | *unplanned* | Feeds theme music; MusicBrainz / TheAudioDB |
| Library types beyond video | **built** | **Music** ([ADR 0024](adr/0024-music-libraries.md)) and **pictures** ([ADR 0028](adr/0028-pictures-library.md)) both on `media_item` with no new tables — the taxonomy claim from ADR 0002 has now survived two media types that work nothing like video. Pictures added the case nobody predicted: the file *is* its own artwork, where everything else points at an image representing it |
| Embedded tags as a source | **built** | ID3v2 / Vorbis / MP4 atoms via the probe that already runs. Authority order for a track: locked fields, tags, folder, filename — the inverse of video, because the file carries the answer |
| Album artwork | **built** | `internal/coverart`: embedded picture first, then `cover.jpg`/`folder.jpg` beside the tracks, in its own worker. Measured on the real library — 369 of 398 albums, 10.7s, no network. A directory's image is refused when the directory also holds audio that is not the album's, which is what stops a letter-bucket `folder.jpg` being worn by five unrelated records |
| Artist images | **back burner** | The placeholder is good enough to wait behind: artists **borrow** their most-substantial album's cover, flagged `inherited`, and a real image supersedes it automatically with nothing to clean up. TheAudioDB, name-keyed and opt-in, is the decided source ([ADR 0025](adr/0025-artist-images.md), accepted, unbuilt) — it was sequenced after the client UI, which is now built, so nothing blocks it except priority. Deferred deliberately: music has had a long run and this is the first item where the gap is cosmetic rather than functional |
| NFO sidecar authority | **built** | A sidecar is written only for an identity actually established — writing one for a failed match committed a guess to disk under LANcast's own name, where it outlived the database and was inherited by the next one. The marker that recognises LANcast's own sidecars is version-tagged, so a future release hashing a different field set cannot silently reclassify every sidecar as hand-edited and start trusting stale contents over live metadata. On `main`, an edit authors only the field that changed, per field, rather than claiming the whole file |
| Album artist and year | **built** | Album rows carried a title and nothing else — 398 albums with no artist and no year, which read as three separate faults: a bare detail page, a Year sort with nothing to sort, and a track list that showed every performer because it had no album artist to compare against. Both are now derived from the tracks on every scan, and locks are respected |

### Playback and client · M3

| Area | Status | Note |
|---|---|---|
| Media probing | **built** | ffprobe; codecs, duration, tracks |
| Transcode decision tree | **built** | Direct play / remux / transcode, with reasons |
| Client capability negotiation | **built** | Clients report what they decode (`?can=`) and the server widens the profile ([plan](client-capabilities-plan.md)). `?profile=` had existed for a release and no client ever used it, so a browser that decodes HEVC in hardware was still served a full re-encode of every HEVC file — the whole of the "slow between films" complaint. Additive and widen-only, resolved once for both decision endpoints so they cannot disagree, and a claim that proves false is dropped, remembered, and retried as a conversion |
| ffmpeg pipeline and HLS | **built** | Progressive fMP4 + HLS, session lifecycle |
| Hardware acceleration | **built** | NVENC, QSV, AMF, VideoToolbox — verified by test encode |
| Subtitles | **built** | Sidecar, embedded, WebVTT, OpenSubtitles hash matching |
| React client build | **built** | React + TS + Vite; Home shelves, Browse, Detail, Player, Settings; subtitles local + online; central spatial focus controller (ADR 0004) |
| Theme music subsystem | specced | Behavior in design.md; blocked on M2 |
| Music player UI | **built** | Album view with a numbered track list, square sleeves, an audio mode in the player, and a docked mini-player so leaving the player no longer stops the record ([plan](music-client-plan.md)). Playback moved above the router to make that possible — the media element used to be a child of the `/watch` route, and a route owns its DOM |
| Branding & splash | **built** | App icons + favicon from the emblem, web manifest, and a once-per-session animated splash. Source art in `/assets` |

### Extensibility · M4

| Area | Status | Note |
|---|---|---|
| Plugin runtime and sandbox | **built** | WebAssembly via wazero, deny-by-default capabilities ([ADR 0020](adr/0020-plugin-isolation-boundary.md)); validated by OMDb-as-plugin |
| Extension point catalog | **built** | `rating_source` first; new source for an existing capability. Widening to new kinds waits for a plugin that needs it |
| Plugin distribution and trust | **built** | Signed `.lcplugin` bundles, two-layer trust (Ed25519 + capability grant), two-step install, Add-ons page ([ADR 0021](adr/0021-plugin-distribution-and-trust.md)) |
| Client surfaces: TV, mobile | *unplanned* | A restyle, if the focus model held |

### Native desktop client · ADR 0023

| Area | Status | Note |
|---|---|---|
| Stage 1 — own the window | **built, default** | `LANcast-Client.exe` opens a WebView2 window instead of handing a URL to a browser ([plan](native-client-plan.md)). **Pure Go, `CGO_ENABLED=0`** — the ADR's assumed CGO cost was wrong, tested rather than argued, so the single-runner release matrix survives. The binding is a trimmed vendored copy with the embedded DLL and its from-memory loader removed ([provenance](../internal/webview2/PROVENANCE.md)); Microsoft's signed loader ships beside the executable |
| Certificate trust | **built** | The point of owning the window, and worse than the ADR assumed: against a LAN-bound server the web view does not warn, it fails the handshake and retries, so the app never loads. The client pins the server's public key, read from its own `cert.pem` on local disk; every other certificate is still validated |
| Flip `-window` to default | **built** | Done after living with it, not after arguing about it. The browser lost on three things a tab cannot fix: LANcast cannot say what its close button means, cannot pin the server's certificate, and gets a warning against a LAN-bound self-signed server that the window does not need. `-browser` is the opt-out, `-window` is kept as a no-op alias so existing shortcuts and the autostart run key keep working, a machine with no WebView2 runtime falls back on its own, and the installer's finish page offers both |
| Stage 2 — own playback (libmpv) | *unplanned* | Deliberately not started. Its case is narrower than the ADR first made it — see the 2026-08-08 amendment: HEVC left the list |

### Cross-cutting

| Area | Status | Note |
|---|---|---|
| Users, auth, sessions | **built** | Multi-user accounts with admin/member roles (ADR 0015); per-user watch state |
| Remote access | documented | VPN or reverse proxy; see security.md |
| Security model | **built** | Auth, CSRF, throttling, loopback-until-secured |
| Transport security (TLS) | **built** | HTTPS beyond loopback; bring-your-own or self-signed cert, http→https redirect (ADR 0014) |
| Performance targets | *unplanned* | Budgets for a 40k-item library |
| Packaging and distribution | **built** | Two branded executables, goreleaser matrix, in-binary service install, signed-tag releases with a Windows installer ([ADR 0016](adr/0016-packaging-and-distribution.md), [ADR 0022](adr/0022-client-and-server-executables.md)) |
| Backup and restore | *unplanned* | Rebuild a library without a full rescan |
| Activity view | **built** | `GET /api/activity` in one shape for every worker; a nav indicator and task popover. Indeterminate where the worker genuinely cannot know its total — a scan discovers its own size, so it shows a count rather than a lying percentage |
| Observability | **built, one known gap** | Match score breakdown, review queue, scan skip diagnostics — including `skipped_kind`, the count of media a library's own kind discards — and a single-instance guard that names what is holding it: the service, its pid and its data directory, read at a privilege an unelevated caller actually has. **The gap:** a show library created as a movie library still scans silently, because both kinds take the same files. See the feature backlog |
| Desktop lifecycle | **built** | *Open on Windows start* and *Close to tray* ([plan](desktop-lifecycle-plan.md)), both off by default and both shown only in the LANcast window, because a tab has no tray to reduce to and no close button LANcast owns. The Settings section states which of the three meanings of "closed" you are looking at — stop the server, or leave it running because the service owns it. Autostart records the mode you chose, so picking the browser does not silently become the window at login |
| Update checking and self-update | **built; proven as far as staging, unproven past it** | Check, download, verify, stage, and swap on the way down. **The download was broken from the day it shipped until v0.6.4**: the release lookup asked a JSON endpoint for octet-stream and GitHub answered 415, so v0.6.2 and v0.6.3 could find an update and never fetch one. Found by pressing the button, not by reading — the first release that could have been installed was the first test the code had ever had. **v0.6.5 was the first release the updater could fetch, and it got further than
before and still not all the way**: it downloaded, verified and staged
correctly — the swap applied, the binaries were replaced — and then stopped
dead, because a service never shuts down on its own and the update is applied
on the way down. v0.6.6 adds the restart. What remains untested is the whole
path in one go on an installed artifact: download, stage, restart, come back on
the new version. Two releases have now been spent finding one more link in that
chain each time, which is what a path that only executes during a release costs. What *is* proven, on the published artifacts: the signature verifies against the key compiled into the shipping binaries, and the Windows archive's digest matches the one in the signed list — signature → checksums → bytes, all three links checked rather than assumed. Signed releases first, because auto-install is a LocalSystem process executing a downloaded binary and without authenticity that is remote code execution as SYSTEM — an Ed25519 signature over `checksums.txt`, offline verification against a key compiled into the binary, and a **separate key from the plugin project key** so one compromise is not both. Three distinct states: unsigned is installable by hand only, a wrong signature is refused outright, and a build with no key refuses everything. The swap works because a running process on Windows can rename *itself*: the binary moves aside to `.old`, the staged one takes its place, and the next start is the new version — one restart, no UAC prompt, no second process. Nothing is written into the install directory before the swap; staging lives in the data directory. The check is on by default and is not a phone-home exception — a plain GET with no install identifier, no statistics, no history, and switchable off entirely |
| Audit log | **built** | Who changed what, and when, recorded server-side where the mutation is authorised ([ADR 0026](adr/0026-audit-log.md)) — libraries, deleted titles, overridden matches, accounts, add-on trust. An audit trail a client writes is forgeable by the client it is auditing. Readable from Settings, newest first, filterable by action. Browsing and playback are deliberately excluded: burying a handful of deliberate acts under a million routine ones is how a log becomes unreadable. Each entry freezes the actor's name and a sentence at the time it happened, so "who deleted this library" still answers after both the library and the account are gone |
| Testing strategy | **built** | CI runs go test + client build + bundle-drift check; fixture libraries, no real media |
| Licensing and open-sourcing | **done** | **MIT** (`LICENSE`) and **the repository is public**, which is what the update check needed: the releases API returns 404 for a private repo and cannot distinguish "no such repository" from "not yours to see", so the checker was correct and inert until the switch was flipped. Vendored code keeps its own notices and the README points at them. **The history cleanup did not happen before the repo went public and is now a judgement call rather than a scheduled one** — three `lancastd.exe~` blobs from the M3 era still sit in history (~25 MB packed, about a third of `.git`). Rewriting 352 commits and re-pointing every release tag was worth doing *once, before* the first public clone; after it, a rewrite also breaks every clone and every commit link that already exists. The honest options are to accept the weight or to pay it deliberately and announce it; `*.exe~` is gitignored, so it cannot grow either way |

## Client UX backlog

Noted from use of the old single-file client, and largely resolved by the React
rebuild, which split the player dialog into distinct screens.

1. ~~**Separate the information screen from the player.**~~ **Done** — clicking a
   poster opens the full-bleed detail page (synopsis, cast, artwork) with a
   **Play** button; playback is its own screen.
2. ~~**Play the official trailer on the information screen.**~~ **Done** — a
   Trailer button opens a lightbox that embeds and autoplays the provider's
   trailer, on the detail page.
3. ~~**Subtitles belong to the player, not the preview.**~~ **Done** — the picker
   lives in the player, with local tracks, online search, and removal.
4. ~~**Reposition "fix match".**~~ **Done** — metadata correction is a modal on
   the detail page, with a score breakdown that explains each candidate, and a
   dedicated Review screen queues everything the matcher was unsure about.

All four are resolved; the backlog is closed.

## Feature backlog

Captured, not yet designed. Each of these is planned immediately before it is
built, not here — this section is the running list of *what*, so nothing is
lost, deliberately ahead of the *how*. Grouped for legibility; order within a
group is not priority.

### Pages and navigation

- **Organising a large channel list** — designed, not built:
  [ADR 0039](adr/0039-organising-a-large-channel-list.md). A real server now
  carries **1,862 channels from one provider** with a second playlist beside it,
  merged onto one page with no way to ask for one and not the other; the ~60
  group chips wrap to five rows before a single channel is visible, so the
  filter row is taller than what it filters. Four changes in cost order: a
  `source_id` filter on `/api/channels` with a selector that appears only when a
  second playlist exists, groups that open rather than filter, per-device hidden
  and favourite channels, and a guide-first grid **deferred** — ADR 0036 already
  refused the grid for the schedule strip, and it would render 1,862 rows of "no
  listings" while no provider in use publishes XMLTV. Virtualising the tiles is
  named and rejected: the ask is not to be shown 1,862 tiles, not to scroll them
  faster.
- ~~**More branded, thematic home page** — beyond functional shelves.~~ —
  **built**: a masthead that greets by name and by local hour, states the size
  of the collection, and puts the libraries in reach as destinations with their
  counts. It compresses when there is a hero below it, because two full-height
  openings stacked is one too many. **No gold anywhere on it except the focus
  ring** — the brief invites decoration and decoration is exactly what would
  kill the focus signal, so the theme comes from the nebula palette and the
  letterspaced caps instead. It also gives a fresh install something to say:
  before this, a server with no watch history and no recent additions opened on
  a blank page.
- ~~**Auto-expanding / collapsing navigation bar**~~ — **built** in v0.6.7,
  alongside the move from a horizontal nav to a vertical rail. It expands over
  the content rather than displacing it, and on focus as well as hover.
- ~~**Movie library page** and **TV-show library page**~~ — **done**
  (Phase 1): media-type-aware browse views selected by library kind.
- ~~**Add-ons page**~~ — **built** as a real page at `/addons`. The rail has
  called Add-ons a destination since the shell was built and pointed it at
  `/settings?pane=addons`, which taught that Add-ons is a setting. It lists what
  is installed and says plainly why there is nothing to install yet — a plugin
  contract is a promise about the shape of the core, and making that promise
  before the core is finished is how a plugin API becomes the thing that stops
  the core changing. Installation stays in the settings pane that already does
  it properly rather than growing a second uploader to keep in step. Still admin
  only, for a narrower reason than before: `/api/plugins` is admin-gated, so a
  member would reach a page that could not tell them whether anything was
  installed — and it says *that* instead of showing them an empty list.
- ~~**More defined settings page** — a real structure, not a flat list.~~ —
  **built** in v0.6.9. Categories down the left, one pane at a time, the pane
  in the URL. Grouped by whose setting it is (server versus this device) rather
  than by subject, because that is the distinction that matters once two people
  share a server. Also added: a General pane showing the server and API version
  from `/api/health`, which no client had ever asked for, and controls for
  `rate_per_sec` and `update_check` — both accepted and validated by the API for
  as long as it has existed, and reachable until now only by hand-editing
  `config.json`.
- ~~**Downloads page** and a **download handler**~~ — **built.**
  `GET /api/items/{id}/download` serves the original as an attachment, named
  from the item's metadata rather than its path (`Arrival (2016).mkv`, and
  `Show - S02E07 - Title.mkv` for an episode, because `Pilot.mkv` collides with
  every other pilot ever made). **Never transcoded**: the transcoder exists so a
  device that cannot play a file can still watch it, and a download that quietly
  returned a re-encoded copy would be a lie about what you have. Range requests
  are honoured, so an interrupted nine-gigabyte transfer resumes. The page
  itself is deliberately **a receipt list, not a transfer manager** — once a
  download starts the browser owns it, and a progress bar over an unobservable
  transfer is a guess that reads as fact. Per device, because the phone that
  downloaded something is the phone that has the file. What is still open: an
  add-on's content, which has no route to serve until add-ons do.
- **Profile page** (details under Social and profiles below).
- ~~**Bigscreen (10-foot) mode** — with a settings option to enable it at
  startup.~~ — **built**, as one attribute on the document root and a `zoom` on
  `body`. Not a second client: a separate television UI is a second set of
  screens to keep in step, and every product that has built one has spent the
  years afterwards explaining why a feature exists in one and not the other. It
  is a zoom rather than a set of larger tokens because every size in this client
  is in px, so a token-by-token enlargement would mean auditing every stylesheet
  and would still miss the ones written after the audit. Applied **before the
  first paint** by four lines in `index.html`, since React cannot read
  localStorage until it has mounted and a television that flashes the desk
  layout on every load reads as an app breaking and correcting itself. The
  setting is per device — "I am ten feet away" is a fact about the room, not
  about the account — and `Ctrl+Shift+B` toggles it from anywhere, because the
  way people find it is by turning it on and wanting out. It works at all only
  because the keyboard model came first (ADR 0004): a pointer-only client would
  have needed a rewrite, and this one needed a stylesheet.

### Libraries and media types

- ~~**Playlists, and importing `.m3u`**~~ — **built** in v0.6.9
  ([ADR 0030](adr/0030-playlists-and-m3u.md), accepted). A playlist is
  `kind = 'playlist'` on `media_item` — no new *item* table, the third media
  concept to manage that — but membership needed one: `item_collection` is keyed
  `(item_id, collection_id)` and so cannot hold the same track twice, which is
  ordinary in a playlist and impossible in a collection. `playlist_entry` is
  keyed on **position** instead, which is that difference written in SQL
  (schema 17). An `.m3u` is an **import, not a mirror**: the database is the
  truth, and editing a playlist locks its membership so a rescan cannot undo the
  edit — the locked-fields rule applied to membership. Unresolvable lines are
  counted and reported rather than silently dropped, our own HLS output is
  refused, and Windows paths resolve on Linux. The repeat case forced the queue
  cursor to become a *position* rather than an id: `indexOf` always finds the
  first occurrence, so a track appearing twice used to send playback backwards
  and strand everything after it.
- ~~**Editing a playlist**~~ — **built** in v0.6.10, after v0.6.9 shipped
  playlists that could be imported and played and not changed. Five writes:
  create, add, reorder, remove an entry, delete the list. Entries are addressed
  by **position**, not by item id, for the same reason the queue cursor is — an
  id does not name a row in the one listing that may hold the same id twice —
  and a removal resequences so a position stays the index the client rendered.
  Every membership write **locks `members` before writing**, which is what stops
  the next scan re-importing the `.m3u` over the edit; a rescan against the real
  test libraries confirms it, re-importing the untouched playlist beside it and
  leaving the edited one alone, with the `.m3u` on disk byte-identical.
  Deletion is its own route rather than `DELETE /api/items/{id}`, whose modes
  are about *files*: one would delete the seeding `.m3u`, the other ignore-list
  it. The writes need a session but **no particular role** — the admin gate is
  for filesystem access and account control, and a playlist edit is neither —
  which stands until ADR 0030's open question about per-user playlists is
  decided. In the client, playlist rows gain reorder and remove-from-list
  controls (buttons, not a drag: a d-pad cannot drag), numbered by position
  rather than by the track numbers they carried on their own records, and any
  playable item gains **Add to playlist**, which appends and can create the list
  on the way.
- ~~**Wide-scope audio codec support** — MP3, FLAC, WAV.~~ — **done**: eleven
  audio formats scanned, and audio containers are first class in the playback
  profile so a FLAC is not re-encoded to deliver a format every browser plays.
- ~~**Music library.**~~ — **done**, end to end: server-side in v0.5.0, client
  UI and mini-player in v0.6.0.
- ~~**Photo library** with a built-in **image viewer**~~ — **built** in v0.6.5 ([ADR 0028](adr/0028-pictures-library.md), [plan](pictures-plan.md)). Folders become galleries, because a filename like `openart-f81b76…_raw.jpg` says nothing and there is no provider to ask. Thumbnails run in their own worker through the existing content-addressed cache; HEIC decodes through the ffmpeg already required, because a phone backup is mostly HEIC and a wall of placeholders would be a feature that looks finished and is useless. EXIF orientation and date-taken only — **GPS deliberately unread**, since the safest way never to leak location data is never to load it.
- **Live TV** — a tuner page and function. **Channels, playback and the EPG
  are built; what is not right is how the picture is paced.** A channel
  source is an M3U — from an IPTV provider or from a tuner on the network —
  and a channel is deliberately **not
  a `media_item`**: that table describes works, and a channel has no duration,
  no file, no position and no identity a provider could match, so it would have
  meant six nullable columns and a `kind` every listing must exclude
  ([ADR 0002](adr/0002-one-wide-media-item-table.md)). Channel lists are
  routinely credentialed, so **the upstream URL never reaches a client**: clients
  play through a proxy that takes a channel id rather than a URL, and for HLS the
  playlist is rewritten so segments resolve *relative to that channel's own
  base* — nothing a caller sends can change the host, which is what keeps it
  from being an open relay inside somebody's network. Refreshing **replaces**
  rather than merges, because the file carries no id worth trusting across
  versions and merging duplicates every channel each time the guess is wrong.

  **Playback now works in any browser.** `GET /api/channels/{id}/live` routes a
  channel through the ffmpeg pipeline that already produces progressive fMP4 for
  files, which is what makes an MPEG-TS channel playable in Chromium at all —
  `canPlayType("video/mp2t")` is empty, and [ADR 0013](adr/0013-transcode-pipeline.md)
  refuses to vendor hls.js. Verified end to end: a real transport stream
  decoding at 640×360 in the browser, buffered and advancing.

  Usually a **remux**, not a transcode — nearly every channel is H.264, which
  fMP4 takes as-is. Audio is treated the opposite way, and the asymmetry is the
  design: a video encode costs a core per viewer and an audio encode costs a few
  percent, so video gets the benefit of the doubt and audio does not. Audio is
  copied only when it is *known* to be AAC and re-encoded otherwise, because
  guessing wrong about audio produces a working picture with silence — the
  failure that looks like success.

  The bug that found: **AAC inside MPEG-TS carries ADTS framing MP4 refuses.**
  Without `-bsf:a aac_adtstoasc`, ffmpeg emits a valid `ftyp` box, rejects the
  first audio packet and exits — 16 KB where the fix produces 1.05 MB on the
  same source, and a browser showing one frame before stopping. The most common
  live format in existence, failing in the way hardest to attribute.

  ffmpeg's lifetime is the request's, tested against a real process: a live
  source never ends, so nothing else would ever stop it and a leak pulls a
  stream at full rate for ever.

  **The EPG is built** ([ADR 0036](adr/0036-epg.md)). A source can carry a
  second URL — an XMLTV guide, plain or gzipped — and the Live TV page shows
  what is on now and next on every tile, with a schedule under the player and a
  search that reads programme titles as well as channel names.

  Listings attach to channels by **`tvg-id` and nothing else**, and that is a
  decision to refuse a feature rather than a gap. Matching on display name is
  the obvious fallback and it attaches "BBC One" listings to "BBC One HD" with
  total confidence — a failure that is *invisible from the guide*, because the
  titles and times are all plausible and the only way to find it is to watch the
  channel. A channel with no `tvg-id` says it has no listings instead.

  The ordering constraint that fell out of it: replacing a source's channels
  cascades their listings away, so a refresh imports the channel list first and
  the guide second. The other order yields an empty guide and no error anywhere
  to explain it. Tested from both sides.

  Guides refresh themselves every twelve hours, unlike library scans, which are
  opt-in. The asymmetry is the point: an unrefreshed guide does not go blank, it
  goes *wrong* — it is the only thing here that decays into a falsehood by being
  left alone.

  **Still not built:** hardware tuners (HDHomeRun and friends — no device to
  build against), and recording, which needs somewhere to put the files and a
  decision about what a recording *is* once it lands in a library.

### Metadata, ratings and discovery

- ~~**Ratings system**~~ — **done**: TMDB rating display + rating sort (Phase 3),
  and the **Metacritic / Rotten Tomatoes / IMDb** tie-in via OMDb
  ([ADR 0019](adr/0019-external-ratings.md)).
- ~~**Plex-style filter settings**, with a **total movie/show count per library
  page**~~ — **done** (Phase 2): multi-select genre/decade/content-rating
  filters, unwatched toggle, per-library counts in the nav.

### Social and profiles

- **Profile page** — **the history half is built**, and the rest is deliberately
  not. `GET /api/profile` answers identity, history and totals in one request,
  derived from `playback_state`, which has held those answers since v0.4 and had
  never been asked. The stated cost of no new table: one row per item per user
  means the *last* time each thing was played, not every time — a history, not a
  log of sittings, and the page says so. `watched_ms` counts time **spent**, not
  runtime owned, because summing the duration of everything opened reports
  eleven hours for eleven films abandoned in their first minute. Missing items
  stay listed: "what happened to the film I watched last week" is a question
  about history, and a lost drive should not lose the answer. **Find Friends,
  Trending, ratings/reviews and viewer stats are not stubbed** — two of them need
  a decision about who may see whose viewing that nobody has made, and a page of
  scaffolding promising four features is worse than three true numbers, because
  the scaffolding is what people plan around. **Find Friends and viewer stats
  are now built too, because the decision they waited on has been made:**
  [ADR 0035](adr/0035-who-may-see-whose-viewing.md) settles that viewing is
  private by default and shared only by an explicit per-account opt-in. Four
  deferrals across three passes was enough — each feature built around the
  question had encoded an assumption about the answer, and the assumptions were
  not obviously compatible. "Find Friends" became a **People** page, which is
  the honest name on a household server: there is no directory to search and no
  second server to federate with, so it lists the accounts already here. It says
  who has chosen *not* to share rather than showing an empty list, because "has
  not shared" and "watches nothing" are different statements. The switch lives
  in the account's own settings and there is **no administrator route that could
  set it** — a switch somebody else can flip is not consent. **Ratings and
  reviews are also built, and only the private half**: your rating is yours, stored per user,
  shown to you, and aggregated for nobody. The routes carry no user id at all,
  so a leak cannot be introduced by forgetting a filter. Turning private
  verdicts into visible ones changes what people are willing to write, which
  makes it a decision about the product rather than a flag — and it stays
  unmade. Scores are out of ten so a half-star interface needs no migration and
  so the scale matches the provider ratings it sits beside; withdrawing a rating
  is deliberately distinct from scoring something 1.
- ~~**Trending (trends computed per library)**~~ — **built**, from
  `playback_state` and no new table. It counts **accounts, not plays**, because
  that table holds one row per item per user — so the API also reports how many
  accounts contributed, and the client names the shelf from that: "Trending in
  Films" with several, "Recently Played in Films" with one. Calling one
  person's history a trend would be a small lie told on the home page every
  day, which is the kind that survives longest because nobody bothers to argue
  with it. `finishers` is reported beside `viewers` because a title many people
  start and nobody finishes is a different fact from one everybody finished.
  Not admin-gated and it names no accounts: which titles are popular is a fact
  about a shared library, who watched them is a fact about a person.
- ~~**Watch Together** — synchronised playback across viewers.~~ — **built.**
  The design question is where the truth lives, and principle one answers it:
  the server owns it. A room holds what is playing, the position, whether it is
  paused and who is in it; clients converge on that. The alternative — every
  client broadcasting its own position — makes the last writer win, and on a
  lossy connection that is whoever lagged worst.

  **In memory, no schema.** A room means nothing after a restart; persisting one
  would resurrect a film nobody is watching. **Polling, not sockets**: nothing
  else in this stack streams, and a socket layer for one feature is the
  dependency argument [ADR 0013](adr/0013-transcode-pipeline.md) settled. A
  second of drift is fine for "we are watching this together"; frame accuracy
  was never the goal.

  **One host drives**, and the host leaving *ends* the room rather than promoting
  somebody — promotion sounds generous and is worse, because the film keeps
  playing in three houses under a driver nobody chose. Rooms drop members who
  stop polling, because **nobody presses leave, they close the laptop**.

  Two things the build taught. The sweep that drops absent members has to record
  the caller *before* it runs, or a host polling exactly on the interval is
  judged absent and takes down their own room for being on time. And a
  follower's correction has to allow for the time since the host reported —
  otherwise every seek lands one poll-interval behind and never catches up,
  including the case where two machines' clocks disagree and the naive sum seeks
  backwards forever.
- ~~**Better profile manager.**~~ — **built**, as two surfaces separated by
  authority: your own display name lives in Account, while renaming anyone else
  and changing roles lives in Users. A rename **keeps the account id**, so watch
  history, ratings and playlist membership follow silently — that is what makes
  it a rename rather than a replacement. Promotion and demotion are one button
  because they are one decision with two directions, offered for yourself as
  well: the rule that protects the install is "not the last admin", not "not
  you". That refusal lives in the store, inside a transaction with the count,
  because two admins demoting each other at the same moment is a race a
  handler check loses — and the prize for losing it is a server nobody can
  administer without `reset-auth` on the machine itself.

### System, operations and diagnostics

- ~~**Activity status in the UI**~~ — **built.** `GET /api/activity` answers
  "what is the server doing right now?" in one request, normalizing scan,
  enrich, probe, coverart and transcode into one shape, and the shell shows a
  pulsing indicator with a popover listing each task. The per-worker endpoints
  each answered for one worker, which meant a client wanting to show *anything*
  had to know the whole roster and poll `/api/libraries/{id}/scan` once per
  library — the capability existed and no caller could reasonably use it. A
  failed scan stays listed, because the recurring bug shape in this project is a
  failure with nowhere to appear.
- ~~**Audit log — who changed what, and when.**~~ — **built** in v0.6.1
  ([ADR 0026](adr/0026-audit-log.md)), server-side where the mutation is
  authorised, readable from Settings. The absence of one is why "what emptied
  this library" was unanswerable during v0.4.x testing. Still open beside it:
  whether identity should live in its own store rather than beside the library,
  so losing a password never opens the file holding the media.
- **A wrong library kind is only half-visible.** Kind is chosen once and is
  immutable by design — it decides which files are scanned at all and biases
  movie-vs-TV matching, so changing it later would mean a rescan re-litigating
  identity for a whole library, which is the thing the locked-fields rule exists
  to forbid. Plex takes the same position. The consequence is that choosing
  wrongly is unrecoverable except by removing and re-adding, so the mistake has
  to be **loud at the moment it happens**, and today only one of the two cases
  is. A music library created as a movie library now reports how many audio
  files its own kind discarded (`skipped_kind`), because the audio-vs-video gate
  makes that case obvious: zero items imported. **The show-vs-movie case has no
  signal at all**, because both kinds scan exactly the same files. Nothing is
  skipped, the count stays zero, the scan succeeds, and the library is quietly
  wrong in its *shape* rather than its size. Measured on the test library, the
  same folder scanned as `movie` instead of `show`:

  | | `kind=show` | `kind=movie` |
  |---|---|---|
  | shows | 3 | 2 |
  | seasons | 3 | 2 |
  | episodes | 15 | 12 |
  | stray movie / parts | — | 1 movie + 3 parts |

  One show stopped being a show: its episodes were read as a film in three
  parts — almost certainly the miniseries [ADR 0017](adr/0017-collections-and-multi-part-works.md)
  exists for. That is worse than the music case, not better. Music fails loudly
  enough to be reported within minutes; this produces a library that looks
  finished and is wrong, and would be found weeks later by someone wondering why
  a miniseries is a film. The fix is a different signal from a skip count —
  candidates are a post-scan sanity check (a `show`-kind library that produced no
  shows, or a `movie`-kind library where most files parsed as episodes) surfaced
  the same way the review queue surfaces uncertain matches.

  **Built**, as both of those candidates plus a third. It is a *verdict* rather
  than a count, because "1 movie, 3 parts, 0 shows" is not something a person
  should have to interpret at the end of a scan; the sentence and its remedy are
  written server-side and rendered as given, which also removed a sentence the
  client used to assemble for the one case it could see. Thresholds are
  deliberately forgiving — one show is enough to be a shows library, three
  episode-shaped names in a film library is a box set, and a library under five
  items is not judged — because a check that cries wolf is a check that gets
  ignored, which is worse than no check. It runs only on a **successful** scan:
  reporting "your TV library has no shows in it" because a drive vanished
  halfway through would be a false alarm about a permanent mistake.

  Two things the build taught, both found by running it rather than by
  reasoning. The verdict has to be **stored** (schema 20): it began on live scan
  progress, which dies with the process, so a library scanned on Tuesday looked
  fine on Wednesday — the wrong lifetime for a warning about a property that
  cannot be changed. And **withdrawing** it is the subtle half: the episode
  count is gathered during the walk, so a rescan that changes nothing produces
  no evidence and no verdict, which is not the same as a clean bill of health.
  As first written, any rescan silently erased a standing warning.
- ~~**Library editing, deferred to the settings redesign.**~~ — **built** in
  v0.6.16, and it governs more than the name: the *path* is editable too, which
  is the drive-letter case. Repointing rewrites every item path under the old
  root in one transaction, so a moved library keeps its matches, artwork, watch
  positions and playlist membership rather than being deleted and re-added.
  Kind remains immutable, for the reasons above.
- ~~**Crash reporting.**~~ — **built**, local only. A panic used to unwind
  through `net/http`, which closes the connection without a response: the client
  saw a network error and the operator saw nothing unless they happened to be
  reading the log at that moment — the recurring failure shape in this project,
  a fault with nowhere to appear. Now the request answers `500` with the
  ordinary error envelope and the panic becomes a numbered report, listed in
  Settings → Logs. Reports record the **route pattern** rather than the URL,
  because `GET /api/items/{id}` is what somebody fixes. They are JSON files in
  the data directory rather than database rows, since the crash most worth
  having is the one where the database was the thing going wrong; the newest 50
  are kept, because a crash loop writes the same stack a thousand times and the
  first one is the informative one. `http.ErrAbortHandler` is excluded — a
  deliberate connection drop is not a crash. **Nothing is sent anywhere**, and
  that is the point: "we do not phone home, except for crash reports" is how
  every product that phones home began.
- ~~**Internal log viewer**~~ — **built.** `GET /api/logs` returns the tail of
  `lancastd.log` and Settings shows it, collapsed by default and never polled.
  The log had been written beside the database since v0.4.2 and could only be
  read by opening a file manager — the wrong ask for the case it serves, since
  it matters most when the server runs as a service and something is wrong. It
  says when the view is partial rather than letting a reader believe they have
  the whole file. ~~**Debug logging** — raising the level from the UI~~ —
  **built**: a toggle on the Logs pane that takes effect on the next line
  logged, no restart, and survives one, because the faults worth turning it on
  for are the intermittent ones.
- ~~**Clear cache and data** and **reset settings** actions.~~ — **built**,
  with the boundary stated rather than assumed: every action must be
  recoverable *by the server itself*. Cached artwork re-downloads, transcode
  scratch is rebuilt on the next play, settings return to documented defaults —
  and the password hash, provider keys, certificate paths and ffmpeg location
  survive a reset, because a reset cannot restore them and losing the first
  would lock the operator out of their own server.
- ~~**Check for updates** with an **auto-update** toggle.~~ — **built,
  unreleased on `main`.** Signed releases, an update check on by default, and a
  download-verify-stage path that swaps the binary in on shutdown. What remains
  is the first release cut *with* the signing key in place, which is the only
  thing that can prove the published half end to end.
- ~~**Desktop lifecycle — "Open on Windows start" and "Close to tray"**~~ —
  **built** in v0.6.1 ([plan](desktop-lifecycle-plan.md)).

### Input and control

- ~~**Keyboard-control shortcut map and customizer**~~ — **built** on the
  existing spatial focus model (ADR 0004). The map was already in one place so
  the overlay and the settings pane could not disagree; the rows became
  *bindings* rather than being replaced by them, so everything that rendered the
  old shape renders the new one. Rebinding is by **capture** — press the key you
  want — because a text field can be given a key that does not exist, and
  because capture is also how the map ends up correct for a remote, which emits
  whatever it emits. Three rules make it safe to hand somebody this control:
  only **overrides** are stored, so a binding added in a later version still
  arrives instead of being invisible to everyone who ever opened the pane;
  Escape and the arrows are **fixed**, since a map that can strand you on a page
  you cannot leave is a trap rather than a customizer; and a key already in use
  is refused **by name** rather than silently taken. The motivating case is
  ordinary: `[` and `]` for subtitle tracks are one key on a US layout and a dead
  key on several European ones, and a shortcut you cannot physically press is
  not a shortcut.
- **Pop-out player** in our own window rather than the browser's
  ([ADR 0029](adr/0029-picture-in-picture-is-our-window.md), **accepted**, not
  yet built). Picture-in-picture hands the element to Chrome, so the window
  arrives with Chrome's chrome: our subtitles keep rendering in the parent tab
  while the picture is in the corner, a Live Caption button offers guessed
  transcription in place of the real tracks, and speed, audio track and queue
  disappear. Document PiP renders our own player instead. The clock — the fault
  that started it — was fixed without the rework, by reporting the true timeline
  through MediaSession (`72619a6`, unreleased). First piece of work is the
  acceptance test the ADR asks for, before any feature code: proving the media
  element survives an imperative cross-document move under React re-render.

### Resolved modeling question — multi-part and serial works

**Decided in [ADR 0017](adr/0017-collections-and-multi-part-works.md) and now
built end to end.** The four cases split on one axis — are the pieces
independent works or parts of one work? Independent works that continue a story
are a **collection** (many-to-many membership, a side table; members stay
top-level). Pieces of one work are **containment** via `parent_id`, with `kind`
values `part`/`chapter` and a `serial` kind for a closed, play-through-whole
story. All of it is implemented: TMDB `belongs_to_collection` ingestion, the
scanner's grouping heuristics, the client's members/parts views, Play-all, and
a library-kind that routes a miniseries to TV matching. Every motivating case
now works — Storm of the Century matches its miniseries, Toy Story's collection
groups, Baahubali is one work in two parts. Original framing kept for the
record:

- **Storm of the Century** — a Stephen King TV miniseries (one story, several
  parts).
- **Anne of Green Gables / Anne of Avonlea** — a film series that is one
  continuing story.
- **1940s Batman / Superman serials** — chaptered theatrical serials.
- **Baahubali** — a two-part film that is one work.

These do not fit "movie" or "episode of a show" cleanly, and the taxonomy is
deliberately open ([ADR 0002](adr/0002-one-wide-media-item-table.md)); this is
where that openness gets exercised.

## Dependencies that constrain ordering

- **Theme music → M2.** Needs TVDB ids and OST identification. Cannot land sooner.
- **TV client → keyboard focus model.** The roving-tabindex controller is the TV
  client's foundation. Compromise it during M3 and the TV client becomes a
  rewrite instead of a restyle.
- **Plugin contract → one full build of the core.** Deliberately last.
- **Users and auth → schema.** Already handled; can arrive late without data loss.
- ~~**API versioning → before any third-party client exists.**~~ — **built**,
  and deliberately not as a URL rewrite. [ADR 0018](adr/0018-api-contract-and-versioning.md)
  has stated the policy since M3 and nothing enforced it. Now every `/api`
  response carries `X-LANcast-API-Version`, a client may send the same header to
  assert what it was built against, and a version this build cannot serve is
  refused by name with `unsupported_api_version`. That refusal is the valuable
  half: without it a mismatch surfaces as a field that is mysteriously absent
  three screens later, and the report that arrives is "the library page is
  blank". Moving every route under a version prefix would have broken the
  existing client today to buy a property nobody is using yet, and ADR 0018
  already promises `/api` never changes meaning — the same guarantee at no cost.

## Next planning order

1. ~~Metadata and artwork (M2)~~ — **built.** See
   [metadata.md](metadata.md) and ADRs 0007–0010.
2. ~~Transcoding + React client (M3)~~ — **built.** Client executes design.md;
   theme music remains, blocked on OST identification.
3. ~~Security and remote access~~ — **transport security and multi-user
   accounts built** ([ADR 0014](adr/0014-transport-security.md), [ADR 0015](adr/0015-multi-user-accounts.md)).
4. ~~Data model past revision 1 + media organisation~~ — **built** (ADRs
   [0017](adr/0017-collections-and-multi-part-works.md)/[0018](adr/0018-api-contract-and-versioning.md)):
   collections, hierarchy, multi-part works, library-kind matching, delete/ignore.
5. ~~Browse-experience feature backlog~~ — **built** in three PRs
   ([plan](browse-experience-plan.md)): media-type library pages, Plex-style
   filters, per-library counts, ratings display.
6. ~~External ratings (RT/Metacritic/IMDb)~~ — **built**
   ([ADR 0019](adr/0019-external-ratings.md)): OMDb `RatingSource`, `item_rating`
   side table, `imdb_id` from TMDB, enrichment pass, detail display.
7. ~~Plugin architecture (M4)~~ — **built** across a runtime and a distribution
   flow ([ADR 0020](adr/0020-plugin-isolation-boundary.md), [ADR 0021](adr/0021-plugin-distribution-and-trust.md);
   [runtime plan](plugin-runtime-plan.md), [distribution plan](plugin-distribution-plan.md)):
   WASM sandbox, capability model, signed-bundle install with a two-layer trust
   model, Add-ons page, validated by OMDb-as-plugin.
8. ~~Music libraries~~ — **built server-side** and shipped in v0.5.0
   ([ADR 0024](adr/0024-music-libraries.md)): audio file types behind a
   kind-aware scan gate, embedded tags as an authoritative local source, the
   artist → album → track hierarchy, untagged-track scan diagnostics, and audio
   containers in the playback profile.
9. **Finish music.** Artwork came first — it was far smaller, it was
   server-side, and building the client grid against blank tiles would have
   meant designing it twice.
   1. ~~Album artwork~~ — **built.** Embedded cover art and
      `cover.jpg`/`folder.jpg` into the existing content-addressed cache.
   2. ~~Artist tiles~~ — **placeholder built.** Artists borrow their
      most-substantial album's cover, flagged `inherited`, until a real image
      supersedes it.
   3. ~~Music client UI.~~ **Built** ([plan](music-client-plan.md)): album view
      with a numbered track list, square sleeves, an audio mode in the player,
      and a docked mini-player. The grid artist images were waiting for now
      exists.
   4. **Artist images from TheAudioDB** — **provider built, not yet wired to a
      worker.** The lookup, the matching rules and the refusals are implemented
      and tested against fixtures; what it does **not** have is a live
      verification, because TheAudioDB needs an API key and none was added. The
      rule worth keeping is that a **near miss is refused**: a search endpoint
      returns neighbours, and taking the first row is how "Sun" gets a
      photograph of "Sunn O)))" — a wrong face is worse than the borrowed album
      sleeve it would replace, and refusing leaves that placeholder in place.
      Originally recorded as: **back burner, unblocked and not next.** The borrowed album cover is a placeholder good enough to wait
      behind, and it supersedes itself with nothing to clean up
      ([ADR 0025](adr/0025-artist-images.md)). This is the first music item
      whose absence is cosmetic rather than functional, which makes it the right
      place to stop.
10. ~~**Native desktop client (ADR 0023 stage 1)**~~ — **built and now the
    default** ([plan](native-client-plan.md)). Living with it settled it, which
    also unblocked and delivered the desktop lifecycle options.
11. ~~**Audit log**~~ — **built** in v0.6.1 ([ADR 0026](adr/0026-audit-log.md)).
12. ~~**Distribution trust and self-update**~~ — **shipped in v0.6.2**, which is
    the first signed release and therefore the first one that exercised the
    published half. It failed on the first attempt for a reason worth keeping:
    the release pipeline had never signed anything, so the signing step had
    never run.
13. **Nothing foundational remains.** What's next is breadth, from the feature
    backlog: more client surfaces (TV/mobile), more plugin kinds as real
    plugins need them, and theme music if OST identification lands. Each is
    planned immediately before it is built.

    Ahead of any of it: **Live TV plays at the wrong speed**, reported from a
    real install as channels running either far too slow or far too fast. The
    picture arrives and keeps arriving, so this is pacing rather than delivery
    — which makes it a timestamp question, and the first suspect is the
    unconditional `-fflags +genpts` in `liveInputArgs()`: generating
    presentation timestamps for a stream that already carries valid ones is a
    way to invent a frame rate. v0.6.42 fixed an HLS channel playing at 1.5×,
    which suggests the same family rather than the same bug. Undiagnosed as of
    v0.6.46 and not yet reproduced from the command line.

## What the last pass taught

*Three feature passes on 2026-08-15 — fifteen backlog items, PRs #245, #246 and
#247. The findings below are from those, and the first one is the finding.*

**Every browser pass found a fault the test suites could not, and all three were
the same fault.** Not a wrong calculation, not a bad query — a feature that
worked correctly and could not be reached:

| Pass | The suites said | The browser said |
|---|---|---|
| #245 | 112 client tests green | Rebinding a shortcut changed nothing: the customizer stored an override the handlers ignored |
| #246 | 133 green | The library shape warning vanished on restart, and a no-op rescan erased it |
| #247 | 142 green | Watch Together had no button on a single film — the case the feature is *for* |

The shape is worth naming, because it is a class rather than a coincidence.
Every one of those tests asserted **what the code does when called**. None
asserted **that anything calls it**. A test that queries the state a feature
writes is not a test that the feature works; the seam between "the logic is
right" and "a person can get to it" is exactly where all three fell, and it is
the seam a unit test is structurally unable to see. The keyboard fix is now
pinned by a test that presses keys at the real shell and asserts the navigation,
which is the shape the other two would have needed too.

**A rejected promise with no `catch` is a control that does nothing, for ever.**
Document picture-in-picture can be *present* and still refuse — an embedded
WebView reports the API and fails `requestWindow` with `InvalidStateError`,
because there is no window manager behind it. Feature detection cannot see that,
and there is no way to ask without a user gesture. Worse, the obvious fix is
also wrong: falling back inside the `catch` fails too, because the failed
attempt **spends the transient user activation**. The fallback has to be reached
synchronously from a click, so the first click becomes the probe and the button
relabels itself for the rest of the session. **Feature detection answers "is
this implemented", which is not the question "will this work here".**

**Live state has the wrong lifetime for a permanent mistake.** The
wrong-library-kind warning began on in-memory scan progress, so a library
scanned on Tuesday looked fine on Wednesday — while the mistake it reported
(kind is immutable) lasted for ever. Storing it exposed the subtler half:
*withdrawing* it. The evidence is gathered during the walk, so a rescan that
changes nothing produces no evidence and no verdict — which is not the same as a
clean bill of health, and as first written any rescan silently erased a standing
warning. **A warning may only be withdrawn by something that looked again.**

**A presence check that runs before recording presence deletes the punctual.**
Watch Together sweeps members who have not polled inside the timeout. Sweeping
before recording the caller's poll meant a host polling *exactly on the
interval* — which is when the interval lands — was judged absent and took down
their own room, mid-film, for being on time. Caught by a unit test with a fake
clock, because nobody sleeps ninety seconds to prove a timeout.

**Assembling prose in the client is a second rule that will disagree with the
first.** The client built the wrong-kind warning sentence from
`episodes_in_movie_library`. That covered one of the two ways a library ends up
the wrong kind and could not see the other at all — a shows library that
produced no shows is invisible in every count the client receives. The verdict,
its sentence and its remedy are the server's now, rendered as given.

**Two numbers that mean different things at different scales must carry their
scale.** Trending counts *accounts*, not plays, because `playback_state` holds
one row per item per user. On a single-account server every count is 1 and the
list is honestly "recently played" — so the API reports how many accounts
contributed and the shelf names itself from it. Calling one person's history a
trend would be a small lie told on the home page every day, which is the kind
that survives longest because nobody bothers to argue with it.

**A guarantee nobody can check is documentation, not a contract.** ADR 0018 had
stated the versioning policy since M3 and nothing enforced it. The valuable half
turned out to be the *refusal*: without it a client built against another
contract discovers the mismatch as a field mysteriously absent three screens
later, and the report that arrives is "the library page is blank".

**Build order is part of the build.** The client assets are embedded in the Go
binary at compile time, so `npm run build` alone leaves the running server
serving the previous bundle — the fix looks like it did nothing, the page hash
does not change, and a hard reload does not help. Two verification cycles were
spent on this before the served bundle name was compared with
`internal/web/dist/index.html`. That comparison is the fastest way to catch it.

### Carried forward from the pass before

**A release step that has never run has never been tested, and cutting the tag
is the test.** v0.6.2 failed at signing: the artifact path was passed
positionally as `"${artifact}"` and arrived at the shell empty, because the
placeholder is substituted where it appears *inside* an argument, not when it is
the whole of one. Two things made the fix quick rather than a guessing game: it
was reproduced locally with a snapshot build before anything was changed, and
the argv was *printed* rather than reasoned about. The same pass found a second
branch that had never run — the unsigned fallback, which could not have worked
either. **Every branch of a release pipeline is untested until a real tag takes
it.**

**A rule derived from reasoning met a library and lost.** The picture decoder
sent HEIC and HEIF to ffmpeg and nothing else, on the sound-sounding logic that
those are the formats Go cannot read. The first scan of a real library found
eight ordinary BMPs that Go's decoder rejects and ffmpeg reads without
complaint. The replacement rule needs no list: whatever the in-process decoders
refuse is offered to ffmpeg. **A list of exceptions is a claim about the world,
and the world has more cases than the person writing the list.**

**A new media type is an audit of every assumption the old ones left behind.**
Adding pictures found a detail page offering to *play* a photograph, "Play all"
over 779 of them, Fix match against a provider that will never exist, and a
library opening sorted by UUID. None were pictures bugs; they were places where
"a leaf is something you press play on" had been true so long it had stopped
being written down.

**A fake that is more permissive than the real thing tests nothing**, **a stated
cost decays in both directions when never measured** (ADR 0023 priced CGO that a
pure-Go binding did not need, and understated a certificate problem that
refuses rather than warns), **a capability nobody exercises is the same as one
that does not exist** (`?profile=` shipped and no client ever sent it, so every
browser in the house was served the floor), and **two things owning one fact is
a bug waiting for a witness** (the mini-player played the previous film because
a URL sync and a router both decided what was playing; the fix was deleting one,
not arbitrating between them).

## Amendments to schema revision 1

M2 planning surfaced two gaps in revision 1. Because M1 has not been built,
these belong **in** revision 1 rather than becoming the first migration:

- Add a `meta` table seeded with `schema_version = 1`. Without it, the first
  migration has to guess what it is migrating from.
- Make `container`, `size_bytes`, and `mtime` nullable, so M2 can create
  `media_item` rows for directories ([ADR 0010](adr/0010-shows-as-media-items.md)).
