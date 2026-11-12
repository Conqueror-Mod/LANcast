# The run to 1.0: two weekly budgets, Saturday 2026-11-14 onward

This is the order of work for [v1-plan.md](v1-plan.md) sections 3B to 3F and 6. Scope froze at v0.11.0, so nothing here is a feature. The plan is built around one constraint: **agent hours are plentiful after a reset, and Chris's hours are not.** Everything a session can prove on its own runs unattended and in parallel. What needs a person (sound, a phone, a pad, a friend, an installer's finish page) is grouped into three short sittings.

## Rules for every unattended session

- Each session gets its own worktree and branch, and touches only the files listed for it. Two sessions never share a file. That's why the security.md work is one session, not two.
- Each one ends in a PR, with all three suites green (`go test ./...`, `go build ./...`, `npm --prefix web test`). Nobody merges with `--auto`. Merging is a separate pass that checks every CI row.
- A session that finds a real fault fixes it on its own branch if the fix is small. If it isn't small, the session writes the fault up and stops. Either way, a fault is never folded into another session's PR.
- Anything touching ffmpeg is verified **as the service**, using a swap kit in `LANcast-test-builds/`. A shell run doesn't count.

## Week 1: Saturday's reset (unattended wave)

Start all six at once. None of them depends on another.

| Session | Work | Files | Done when |
|---|---|---|---|
| **S1 Version step** (§3D.3) | Add a unit test that 0.11.x → 1.0.0 counts as newer. Write down that release candidates go through swap kits, never `-rc` tags. | `internal/update/*_test.go` | A PR with the test |
| **S2 ffmpeg** (§3E 1–2) | Cost a restricted token or low-privilege account for ffmpeg/ffprobe children. Record the pinned ffmpeg's version and age, and what an update would touch. Build the change if it's ≤ a few days of work *and* passes as the service. Otherwise write it into security.md as a 1.1 item. | `internal/transcode`, `internal/probe` (only if building), `docs/security.md` §ffmpeg | A PR, plus a swap kit if code changed |
| **S3 security.md against the code** (§3E 3) | Bring security.md up to date with what was added since its last revision: guest tickets, invite links, play on another device, webhooks, metrics key, built-in provider keys, the body cap. Fix any place where the doc and the code disagree. If the code is wrong, that becomes its own branch. Also cover post-quantum key exchange: capture a real handshake from the browser, the desktop window and a peer to confirm TLS 1.3 negotiates `X25519MLKEM768`. Go 1.24+ offers it by default, and LANcast never sets `CurvePreferences`. Add a test that fails if any `tls.Config` sets `CurvePreferences`, and give security.md a line on what is and isn't quantum-resistant. | `docs/security.md` (after S2 merges, or rebase on it), plus one test | A PR |
| **S4 architecture.md** (§3F) | Cover everything since 2026-11-01: remote players, history, previews, comics, picture, cheevos, contain, live_tv switch. | `docs/architecture.md` | A PR |
| **S5 Newcomer read** (§3F) | Read README, install.md and privacy.md as a stranger would. Label ledger rows 10–11 ("checked with simulated clients", theater parked, live TV off). Name arm64 as *built, not run*. | `README.md`, `docs/install.md`, `docs/privacy.md` | A PR |
| **S6 Linux amd64** (§3D.4) | Run the release archive under WSL2 with systemd: install, scan the test libraries, and play one film through ffmpeg. If WSL2 with systemd isn't set up here, the session stops and leaves Chris one PowerShell line. | No repo files, except a short result in `docs/install.md` *after S5* | A written result |

Run these **with the driving skill**. They are unattended, but they use the installed service on Aither, one at a time and after S2's kit is decided:

- **D1 ledger 1:** MusicBrainz states, read from a copy of the live database (WAL + shm). This is mostly done already: review is down to 83.
- **D2 ledger 3, 6, 9:** an MKV extra in the window; the follower side of Watch Together on one server (two windows); skip credits on a converted film in a browser, the remux path, and the denial reset row.
- **D3 Sandbox install** (§3D.1): a `.wsb` with a logon command that runs `LANcast-Setup-0.11.0.exe`, then first run, the ffmpeg fetch, an account, a library and a film. The installer's finish page is the one screen that crashed before (v0.9.29), so a screenshot of it is required evidence.

End of week 1: merge S1–S5 (and S2's code, if it passes as the service), then cut **v0.11.1** as the first real candidate.

## Week 1: Chris's three sittings, any evening that week

1. **Sound and couch (about 45 min, Aither + TV + pad):**
   - ledger 2: an audiobook across a chapter change, with the sleep-timer fade;
   - ledger 7: GB/GBC dumps, the pad on the TV, touch on the phone (Master System stays labelled unless a dump turns up);
   - ledger 8: Now Playing on desktop and TV, synced lyrics, MilkDrop on the phone.
2. **Georgia evening (about 1 h):** ledger 4–5. This is the most valuable single check. It covers Watch Together across networks at v0.11.x, roster push, a join request over a full-screen film, and an invite link on a phone.
3. **Open faults (§3C), whenever they happen:** if music stalls on the phone or a sign-in is refused at the public name, copy the log *at once*. If neither comes back by week 2, both go into the notes as *reported once, not reproduced*.

The update from 0.9.79 (§3D.2) can be a fourth sitting, or an agent can do it in Sandbox alongside D3. Prefer Sandbox.

## Week 2: the second reset, the acceptance run and 1.0

Follow §6 of v1-plan exactly, against the candidate built from the frozen commit:

1. All three suites pass.
2. A clean install.
3. Updates from 0.9.79 and from 0.11.1.
4. Linux amd64.
5. On Aither as the service: a film down each path (direct, remux, a hardware-decode transcode), a song, an audiobook, a comic, a photo, a retro game, and Watch Together with Utopia. Then grep `lancastd.log` for `ffmpeg reported errors` and `level=ERROR`.
6. Release v1.0.0 through ship-release:
   - stranger-facing notes, with §5's *not in 1.0* list and the known issues;
   - README status v1.0.0;
   - the roadmap split into status and history (decision 7).

Chris's part in week 2 is one sitting: playing the paths in step 5 and looking at the installer screenshot. Agents do everything else.

## After 1.0 (not before)

- **Sealed secrets: XChaCha20-Poly1305 over AES-256-GCM is not needed.** AES-GCM with random 96-bit nonces is safe far beyond the few hundred seals a server ever makes. If it changes in 1.x, the only reason would be uniformity, and it needs a version byte on every sealed value plus a re-seal migration.
- **Passwords: argon2id over bcrypt-12** is the defensible 1.1 change. It's memory-hard, and it migrates transparently by rehashing at the next sign-in.
- ffmpeg's lower privileges, if S2 costs them over budget.
- **Hybrid identity signatures (Ed25519 + ML-DSA)** once Go's standard library ships ML-DSA. This needs an ADR, because the server identity (ADR 0044) is never regenerated. X-Wing was considered and declined (2026-11-12): TLS already does hybrid key exchange, and LANcast has no other place that needs to agree a key.
