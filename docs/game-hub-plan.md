# Game Hub: plan

From Chris's notes, 2026-10-08:

> Consolidate navigation Retro Games and PC Games into a `Game Hub` category
> that, when clicked, shows a split screen selector for installed PC Games on
> left, and Retro Games on the right. PC games will lead to the current `PC
> Games` screen. Retro Games will lead to current Retro games library screen.
> IF no installed games exist, let user know at split screen selector; if no
> retro game library detected tell user how to add, at split screen.

Not built yet. This plan is for Chris to read before any code is written.

## What changes

**The rail.** One **Game Hub** entry takes the place of Retro Games and PC
Games, in the slot they hold now: Movies, TV Shows, Music, **Game Hub**,
Pictures (`lib/railOrder.ts`, one rank instead of two). The hub is
highlighted while you're on the hub itself, inside PC Games, or inside a
retro library, so the rail always says where you are.

**The hub** (`/games/hub`, a new screen) has two halves:

| Left: PC Games | Right: Retro Games |
|---|---|
| A large tile showing a few installed games' covers, and the count | A large tile showing a few box arts, and the count |
| Opens the current PC Games screen (`/games`), unchanged | Opens the current retro library screen (`/library/:id`), unchanged |

**Everything behind the hub stays as it is.** Neither screen changes. The hub
is a front door; it doesn't replace anything.

## The empty and edge states, as the notes ask

| Situation | Left half says | Right half says |
|---|---|---|
| No PC games installed | "No installed games found." It names the launchers LANcast reads (Steam, Epic, Battle.net, Xbox, GOG, EA) and says when it last looked. | |
| PC Games switched off in Settings | "Show your installed PC games here", with a button straight to Settings → PC Games | |
| Browser or phone (not the desktop app) | "PC games play in the LANcast desktop app." The tile isn't clickable, because a phone can't start a PC game (ADR 0066). | |
| No retro library | | "Add a Retro games library." Three steps: add a library of type Retro games, point it at a folder of ROMs, then download the ROM database. Admins get a button to Settings → Libraries. |
| Retro library exists, ROM database not installed | | Opens as usual, with a one-line nudge to download the database |
| **Several retro libraries** | | The tile lists each library; one click opens it |

## Decisions to confirm (defaults chosen)

1. **Several retro libraries.** Default: the right half lists them. The other
   choice is to open the first one.
2. **Pad and keyboard.** Default: left and right move between the halves, and
   Enter opens one. It's the same focus model as the rest of the client
   (ADR 0004).
3. **Does Game Hub replace the two rail entries, or sit above them?** Default:
   replace, which is what "consolidate" in the notes means. The order shipped
   in #790 is the step before this.
4. **The hub on a browser or phone.** Default: shown, with the PC half
   explaining itself. The other choice is to hide the hub there and keep the
   retro library as a plain rail entry.

## Stages

1. **The hub screen and the rail entry.** Both halves, every state in the
   table, tests for each state.
2. **The look.** Cover mosaics on the tiles, and the hub's own background.
   This is a design pass, done against `docs/design.md`: gold only for focus.

No server change. No API change: the hub reads the libraries list, the retro
library's items, and the client's installed-games list, all of which exist.
