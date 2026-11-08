# ADR 0118 — The home page is a bridge

Date: 2026-11-08 · Status: **accepted** 2026-11-08 · Related: [ADR 0027](0027-depth-in-the-canonical-look.md) (amends rule 3), [ADR 0003](0003-one-canonical-theme.md), [ADR 0045](0045-live-presence-between-paired-servers.md), [ADR 0051](0051-sensitive-content-is-obscured-until-asked-for.md), [ADR 0057](0057-what-a-forty-thousand-item-library-may-cost.md), [ADR 0071](0071-a-shared-library-is-a-standing-grant.md), [ADR 0116](0116-an-audiobook-is-its-own-library.md)

## Context

Home was built for films, shows and music, and then the library grew around
it. Audiobooks, comics, retro games and Steam games have no presence on it at
all, and the kinds that do are spread across three "in progress" shelves —
Continue Watching, Next up and Continue Listening — that were split for good
reasons: a half-played track in a row of posters read as a broken film, and a
square sleeve beside a 2:3 poster gave the row a ragged baseline.

The page is also arranged the way every media center arranges its page: a hero,
then a dozen rows of posters. ADR 0027 gave it depth; it did not give it a
shape anybody would recognise as LANcast's. The owner asked for "a new face" that
stands apart from the competition, and chose this direction on 2026-11-08.

Two things this project already owns and no competitor does were also absent
from the page: friends on paired servers (presence, ADR 0045; Watch Together;
play on another device, ADR 0117), and a deep-space field designed to take its
mood from the media in front of it.

## Decision

### 1. Above the fold is a grid of panels, not a row of shelves

Four panels share the first screen: the **hero** (the larger, left), the
**resume console** (right, top), and beneath it the **friends panel** and **On
this day** side by side. Shelves follow below.

At each size:

| Width | Arrangement |
|---|---|
| ≥ 1200px | The grid above: hero spans two rows at 2fr, the panels share 1fr. |
| 768–1199px | Hero full width; resume, friends and memory in one row beneath (1.4fr / 1fr / 1fr). |
| < 768px | One column, **resume console first**, then a short 16:9 hero, then friends and memory as one swipeable strip. |

Phone puts resume first on purpose: a phone is mostly where you pick something
back up, and a full-height hero spends the whole first screen on one
suggestion.

**TV** (today: the desktop window full screen on the theater TV) uses the
desktop grid with a fixed focus order — the hero's Play starts focused; right
enters the console, up and down move within it; down reaches friends, then
memory, then the shelves one row per press. Everything sits inside a 5% safe
margin. The order is declared, not left to fall out of the DOM.

### 2. One resume console replaces the three "in progress" shelves

**Continue Watching, Next up and Continue Listening are removed.** Their items
appear once, in the console, and **See all** opens a full Continue page.
Watchlist and Rate what you've watched stay as shelves: they are about what to
do next, not what you are in the middle of.

The console is a list of cards of **one height**. Each card's art sits at the
left **in its own aspect** — poster, sleeve, episode still, book cover, comic
page, save screenshot — and the text says what kind of thing it is. A uniform
card is what lets mixed kinds share a panel without the ragged baseline that
split the old shelves.

Rules for what is on it:

- **One card per thing you would resume:** the album, not its tracks; the book,
  not its parts; the show, not its episodes (the card names and pictures the
  next one); the game, not its saves.
- **Most recently touched first**, across kinds. Grouping by kind would rebuild
  the old shelves inside one panel.
- **At most two cards of any one kind.** One shared window once let songs push
  every film off Continue Watching (fixed in #1025). The server takes a window
  per kind and merges them; it never splits one list afterwards.
- **5 / 4 / 3 cards** on desktop / tablet / phone.
- **An item leaves the console when it falls outside the Continue window**
  (`continue_weeks`, 16 by default; see As built). One rule for every kind,
  not a window chosen per query.

**Progress is said in each kind's own terms:** "1h 12m left", "S2E4 · next",
"track 6 of 11", "chapter 9 · 3h left", "page 41 of 120", "saved 2 days ago",
"played Tuesday". The progress line is neutral, never gold.

**The menus carry over verbatim.** A card's right-click is the existing
`continueActions`: *Mark as watched/played* and *Remove from Continue
Watching/Listening* stay two separate entries because they record different
things about whether you saw it, and a show offers *Mark all as
watched/unwatched* through its episodes. Same code, not a reimplementation.

**The server decides what is in progress.** A new endpoint returns every kind
in one card shape, so the client draws and does not judge. It is an API
contract change, made in `docs/api.md` and `docs/openapi.json` with the
handler. It applies library access and sharing grants (ADR 0071) and the rating
ceiling exactly as the existing continue queries do, and it is held to the
home-shelf budget in ADR 0057.

### 3. No state of the page is an empty panel

- **No libraries:** the Bridge is one welcome panel, *Add your first library*.
- **Libraries, nothing in progress:** the console lists the libraries with
  their counts. That is the masthead's job today, moved.
- **Fewer items than the cap:** the panel is shorter. It is never padded with
  suggestions, which would blur "yours, in progress" with "recommended".

The masthead slims to the greeting and the count line ("N things to play, all
of them yours"); the library links live in the navigation and in the empty
console.

### 4. The friends panel says what is true about each friend

- **No paired servers:** an *Invite a friend* panel using the existing invite
  flow. An account that cannot send invites does not see a button that will
  fail; On this day takes the width.
- **Paired, nobody watching:** the roster, each friend online or quiet, and
  "Nobody's watching right now".
- **Someone watching:** "Georgia is watching *Title*" with **Join**, which sends
  the existing Watch Together request. No presence grant, no Join — the
  server's own rule (ADR 0045 §7), not a second copy of it.
- **A friend's server cannot be reached:** said in words ("Can't reach Kiousu's
  server"), never shown as quiet. One-way pairings and dead addresses have
  silently killed presence before; the panel does not hide it.

This is built on what `GET /api/people/peers` and the `presence` event already
carry. What friends have *added* needs new federation data and is not in this
decision.

### 5. The look

**The hero's artwork chooses which wisps of the field brighten.** When artwork
is cached, the server records a small summary of its colour — warm or cool,
light or dark, and which of the field's wisps it is nearest. The hero then
raises those wisps and lowers the others, within fixed bounds: a desert film
warms the ember, a night thriller lifts the blue and teal.

**This amends ADR 0027 rule 3**, which tints the hero with violet and blue
only. The principle that rule protected survives: **no colour is introduced.**
Every hue on screen is still one of the field's own five wisps; the artwork
only decides their weights. Nothing ever moves toward gold, and the base stays
deep-space. An item that is obscured as sensitive (ADR 0051) does not steer the
field.

**Panels are viewports, not boxes.** A panel is a slightly darker cut-out in
the field with the resting gold hairline design.md already allows. When focus
enters a panel, the panel lifts one elevation step; the item focused inside it
carries the gold ring. A panel never takes the ring itself, so there are never
two golds in one focus scope — and from across a room the lift says *which
panel* while the gold says *which thing*.

## Consequences

**Home covers every kind the server holds.** Books, comics and games reach the
first screen through the console, without a shelf each.

**The ragged-row problem is solved by shape, not by separation.** The old split
was correct for rows of tiles; a list of uniform cards does not have the
problem, so the split can go.

**There is one place to look for "in progress".** The cost is that Next up's
row of stills becomes one card per show plus the See all page. With more than
two shows in progress, the rest are a click away instead of a scroll.

**The field now varies from visit to visit.** That is intended, and bounded: the
same five wisps, the same base, no new colour. ADR 0003 holds — this is the
media lighting the one look, not a second look.

## Rejected

- **Tinting the field with colour sampled from the artwork.** The most vivid
  version, and it introduces arbitrary hues — exactly what ADR 0027 rule 3
  exists to prevent. Weighting the field's own wisps gets most of the effect.
- **Keeping Next up as a shelf beside the console.** Every show in progress
  would appear twice.
- **Padding a short console with suggestions.** It makes the console's
  contents a mixture of facts and guesses.
- **A gold ring on the focused panel.** Two golds in one scope.

## Delivery

Homepage 2.0 is its own release line; v0.10.8 is reserved for unrelated fixes.

- **v0.10.9:** the grid at all three widths and the TV focus order; the resume
  console and its endpoint; the friends panel as above; On this day as a panel;
  one *New this week* section replacing the three Recently Added shelves; the
  wisp-weighted hero; viewport panels.
- **v0.10.10:** a hero mode that learns from your own play history ("Tonight,
  because…"), offered beside the existing modes; shelves you pin yourself, kept
  per account on the server (a schema change with its own ADR); On this day
  beyond photographs.

## As built

### The resume endpoint (2026-11-08)

`GET /api/resume` (`internal/api/resume.go`, `internal/store/resume.go`). Each
card has a `kind` (`video`, `show`, `music`, `book`, `comic`, `game`), a
`touched_at`, and the `item` it pictures and opens. A music card adds `track`
(the track to resume, its number, and the album's count) and a comic card adds
`page`. `more` says something was left out, and `?all=true` is the See all
page.

This settles three of the questions listed below, and changes one decision:

- **The staleness rule is the existing Continue window, not 60 days.** The
  server already had one: `continue_weeks`, 16 by default and set by the
  administrator. A second window of 60 days would have been two rules that can
  disagree, which is what §2 says to avoid. So the console uses that setting.
- **Steam's last-played time *is* readable** (`games.Game.LastPlayed`), but the
  desktop app reads it, on its own machine, for that machine's Steam account
  (`cmd/lancast/games.go`). The server has no per-account fact to put on the
  console, so launcher games are left out. Showing one machine's Steam history
  to every account on the server would be wrong, not just incomplete.
- **Retro saves carry no screenshot** (`rom_save` holds bytes and hashes), so a
  game card uses the game's box art.

## Not decided here

- **Launcher games on the console.** That would need the desktop app to report
  per account what it reads locally. It is not planned for v0.10.9.
