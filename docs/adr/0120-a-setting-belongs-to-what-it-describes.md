# ADR 0120 — A setting belongs to what it describes: the server, you, this device, or the media

Date: 2026-11-08 · Status: **accepted** 2026-11-08 · Related: [ADR 0006](0006-playback-state-keyed-by-user.md), [ADR 0083](0083-an-intro-skips-itself.md), [ADR 0099](0099-a-preroll-plays-before-a-film-from-the-start.md), [ADR 0117](0117-play-on-another-device.md), [docs/settings-3-plan.md](../settings-3-plan.md)

## Context

Settings 2.0 (docs/settings-plan.md) grouped Settings by who a setting belongs
to: the server, your account, this device. It said so in the nav and nowhere
else. In the month after it shipped, every new feature chose its own scope:

- Spotlight is stored per device and pinned shelves per account, though both
  are about what Home shows you.
- Book speed is per book. Comic place is per account. Spoilers are per device.
- Skip intros and the pre-roll are per account, yet are drawn on This device ›
  Player beside settings that are not.

Some of these choices are right and some are accidents. With no rule written
down, nobody can tell which, and the next feature will choose again.

Chris accepted the rule below on 2026-11-08 (settings-3-plan §7, decision 1).

## Decision

Every setting belongs to one of four owners. **Its owner is what the setting
describes**, not where it is convenient to store it.

1. **The server.** It changes things for everyone, so it is an
   administrator's. Stored in `config.Settings`.
2. **You.** A fact about the person — what you like, what you have connected,
   what you allow. It follows you to every device you sign in on. Stored on
   the server, per account.
3. **This device.** A fact about the screen and speakers in front of you:
   size, viewing distance, audio output, bandwidth, what this machine can do.
   It stays here. Stored in the browser (`localStorage`, as `playback/prefs`
   already explains).
4. **The media.** A fact about a library or a title, such as a manga
   library that reads right to left. Stored with the media, set per library,
   overridable per series where that makes sense.

Two further rules settle the mixed cases:

- **A device setting may have an account default.** Where both exist, a value
  set on this device wins. Until one is set, the device uses your account's,
  and the control says "Using your account's default". This is how a TV and a
  phone can want different subtitle sizes without either losing the
  preference you set once.
- **An account setting may be drawn beside the device settings it governs**,
  when that is where somebody would look for it. It says that it follows your
  account. Skip intros and the pre-roll stay on This device › Video on these
  terms, as ADR 0083 and ADR 0099 already word them. The rule is about where a
  value is kept; the nav may put it where it is found.

## Consequences

- **Moving a setting is a change of owner, not a refactor.** Spotlight and
  spoilers move from the device to the account (settings-3-plan step 3). The
  move reads the old `localStorage` value once, so nobody's setting resets. If
  two devices disagree, the first one to carry over wins.
- **Defaults for books and comics are account settings** (step 4), and reading
  direction is a property of the library (step 6).
- **A new setting states its owner in the code that stores it**, as the comment
  on `playback/prefs` does today. A reviewer asks "what does this describe?",
  and the answer picks the store.
- **The Player pane splits by subject** (step 2): Video, Music & Now Playing,
  Theater output and Remote play. All four are owned by the device. The split
  is about finding things; this ADR is about who owns them.
- **Nothing here changes the server's own rules.** The loopback rule, locked
  fields and the access ceiling are not settings in this sense and are not
  re-decided by it.
