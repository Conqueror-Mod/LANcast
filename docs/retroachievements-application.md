# Applying to RetroAchievements for hardcore

RetroAchievements counts hardcore unlocks only from emulators it has
approved. This is LANcast's checklist for that application (ADR 0112
step 6): each of RetroAchievements' requirements, where LANcast meets it,
and what is still open. The requirements are RetroAchievements'
[hardcore compliance requirements](https://docs.retroachievements.org/general/hardcore-compliance-requirements.html)
as ADR 0112 recorded them; check that page again before applying, since it is
theirs and may have changed.

**The earliest date to apply is 2027-02-02**, six months after LANcast's first
public release (v0.3.2, 2026-08-02). If RetroAchievements counts from the
first release with the retro player (v0.9.69, 2026-10-08), it is 2027-04-08.
Which one applies is RetroAchievements' call.

## The requirements

| Requirement | Where LANcast meets it | State |
|---|---|---|
| A unique, stable User-Agent | `LANcast/<version> (Windows <major>.<minor>) <core>/<version> rcheevos/<version>`, built by the desktop player (`cmd/lancast/raagent_windows.go`) and checked by the server (`ra.UserAgentOK`) | Met |
| Six months of public release | First public release 2026-08-02 | From 2027-02-02 |
| A findable privacy policy | [docs/privacy.md](privacy.md), linked from Settings › About and the README | Met |
| A public list of every core, with its licence | [docs/cores.md](cores.md), tied to the code by `TestTheCoreListMatchesTheCode` | Met |
| In hardcore, no loading of save states | The game session refuses a slot or an applied state (`internal/retro/host`) | Built, hidden |
| In hardcore, no rewind | Refused; turning hardcore on drops the rewind buffer | Built, hidden |
| In hardcore, no slow motion or frame advance | The desktop player has neither | Met |
| In hardcore, no cheats or debuggers | The desktop player has neither | Met |
| A resumed session drops to casual | Continue drops the session to casual and says why | Built, hidden |
| Casual to hardcore needs a full reset | Turning hardcore on mid-game restarts the game | Built, hidden |
| Hardcore is visible while playing | A "Hardcore" marker over the game | Built, hidden |
| Rich Presence | rcheevos sends it; it can't be turned off on its own | Met |
| Leaderboards | rcheevos runs them; the server forwards entries once approved | **Open:** the page draws no trackers or challenge indicators yet (ADR 0112 §6) |
| A secure offline unlock queue | Awards RetroAchievements can't take are kept on the server, without the token, and sent with their waiting time | **Open:** leaderboard entries are not queued yet, only awards |

"Built, hidden" means the rule exists and is tested, but no one can reach it,
because `ra.HardcoreApproved` is false.

## Before applying

1. Build the two open items above: the leaderboard trackers and challenge
   indicators on the game page, and queueing leaderboard entries on the
   server. Their RetroAchievements requests need checking against the pinned
   rcheevos source, as the award's were.
2. See one achievement fire on a real dump on each desktop core listed as
   supported (ADR 0112, "Not verified yet"). Gearsystem and SwanStation have
   not been measured. bsnes exposes no memory, so SNES is not supported.
3. Read RetroAchievements' requirements page again, and update this
   checklist if it has changed.

## Applying

The application goes through RetroAchievements' own process for emulator
approval, and is sent by the project owner. Nothing in LANcast sends it.

## Once approved

1. Set `ra.HardcoreApproved = true` (`internal/retro/ra/connect.go`), in a
   commit of its own that says when RetroAchievements approved LANcast. Its
   test, `TestHardcoreWaitsForApproval`, says the default is false on purpose;
   change that assertion in the same commit.
2. Replace the profile's "Hardcore isn't offered yet" line with a switch for
   whether games start in hardcore. Today hardcore can be turned on only from
   the game menu, with a restart.
3. Update [docs/privacy.md](privacy.md) if anything sent changes, and ADR 0112.
