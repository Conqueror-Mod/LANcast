# Picture-in-picture over games: plan

[ADR 0076](adr/0076-a-game-and-a-film-are-two-pictures.md). Chris's answers on
2026-10-08:
- **Where:** in the app's corner first.
- **What can go in the corner:** films and episodes, and music with cover art.
- **Sound:** both play, each with its own volume.

## Stage 1: a film carries on in the corner while a game runs

**Client (Go)**
- `webview2/overlay.go`: a **game window**, a second owned popup of
  `videoClass` with no activation. `SetGameLayout(full|hidden)`. The game window
  goes directly beneath the page overlay. The overlay is needed if a film is
  `full` *or* a game is `full`.
- A `pip` video layout: the video window goes above the overlay at the docked
  box. `ours()` learns the game window. The shield stays docked-only.
- `clientwindow.Controller`: `GameWindow()` and `SetGameLayout()`.
- `cmd/lancast/retro_windows.go`:
  - Draw into `GameWindow()` (GDI, WGL, and the input focus check).
  - Show the game window when a game starts and hide it when it ends.
  - **Stop calling `stopVideo`.**
- `host`: `Session.SetVolume`, which scales the samples before they reach the
  sink, and a `volume` command through the bindings.

**Page**
- `nativeLayout(surface, native, box, ratio, overGame)`: docked over a game
  is `pip`.
- A small store saying whether a game screen is up. RetroPlay sets it, and
  PlaybackProvider reads it and re-sends the layout when it changes.
- RetroPlay stops sending `lancastMpvLayout("hidden")` on the way out. That
  would hide a film the person is still watching. The provider owns that
  window.
- The game menu gets **Game volume**: off, 25, 50, 75 or 100%, remembered per
  client and applied when the game starts.

**Checks**
- **Go:**
  - The layout decision: the overlay is wanted for full film or full game, and
    the z-order intent is a pure function.
  - The volume scaling.
  - A game in a window of its own.
- **Vitest:**
  - `nativeLayout` gives `pip` over a game and `mini` elsewhere.
  - The provider re-sends the layout when a game screen comes and goes.
  - RetroPlay no longer hides the video window on unmount.
  - The volume item cycles and sends.
- **In the window, which is the check that counts:**
  - Start a film, dock it, then start a game. The film keeps playing in the
    corner above the game.
  - Quit the game. The film is still docked.
  - Turn the game's volume down. The film is not affected.
  - Music in the corner shows its cover, as today.

## Stage 2: polish, after living with it

**Built 2026-10-08**, with Chris's choices: drag the card by its grip and it settles into the nearest corner; Small, Medium and Large, cycled from the strip or the game menu. Both are remembered on this computer (`lib/dock.ts`). Pause, play and stop from the game menu shipped earlier, in #788.

- Pause and resume the film from the game menu, so it can be done without the
  mouse.
- Choose which corner the card sits in, and let it be dragged.
- Make the card smaller over a game than while browsing, if it turns out to
  cover too much.

## Stage 3: a picture over other applications

Deferred by the ADR. It needs an always-on-top window outside the page, with
controls of its own.
