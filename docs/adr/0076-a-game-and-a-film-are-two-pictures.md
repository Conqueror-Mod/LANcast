# ADR 0076 — A game and a film are two pictures

Date: 2026-10-08 · Status: **accepted**

## Context

The first evening of playing retro games in the desktop client
([ADR 0073](0073-a-retro-game-is-a-file-the-server-owns.md)) turned up
something nobody had designed. Music kept playing in the docked mini-player
while a game ran, and that turned out to be one of the best things about it. The
request that followed was to make the same work for films: a game on the
screen, and a film or an episode going on in the corner.

**Music already does this, by accident of where it plays.** Music plays in the
page, and the docked mini-player is page content: a floating card in the
bottom-right corner, with cover art for a record. While a game runs, the page
sits transparent over the picture, so the card draws on top of the game.

**A film cannot, and for two reasons.**

- **Starting a game stops the film.** Since
  [ADR 0067](0067-the-desktop-client-plays-through-libmpv.md), a film is drawn
  by libmpv into a native *video window* beside the page. ADR 0073 had games
  draw into that same window, so `retroPlayer` calls `stopVideo` before a game
  takes it. Two renderers cannot share one window.
- **The docked picture cannot sit above a game.** The client has three
  layouts:
  - **full:** the video window covers the client area, and the page moves into
    a transparent *overlay* above it.
  - **mini:** the page goes back into the main window, opaque, and the video
    window floats over the docked box.
  - **hidden:** no picture.

  None of them fits. During a game the page has to stay in the overlay, because
  the game is under it. The docked film picture then has to go *above* the
  overlay, not above the main window.

**Document picture-in-picture
([ADR 0029](0029-picture-in-picture-is-our-window.md)) is not the answer
here.** It moves the page's own `<video>` element into a floating window. On
the desktop a film is not in a `<video>` element, and the embedded WebView
reports the API as present and then refuses to open a window anyway (see the
v0.9.x roadmap notes). ADR 0029 stands for the browser. This ADR is about the
desktop client's native pictures.

## Decision

**A game and a film each get a native window of their own. While a game is on
screen, the docked mini-player is the picture-in-picture: the same card, in the
same corner, with the film's picture floated above the page overlay into it.**

1. **Games draw into a game window, not the video window.** It is a second
   owned popup of the same black-backed class. It sits directly beneath the page
   overlay while a game runs and is hidden otherwise. libmpv keeps the video
   window to itself. Starting a game no longer stops a film.

   The game's OpenGL context also stops sharing a window with libmpv. Stage 3
   of ADR 0073 tested libmpv in a window an OpenGL game had already set a pixel
   format on, because a pixel format can never be unset. That check now guards
   a case that can no longer arise.

2. **A new video layout, `pip`.** The video window goes *above* the page overlay
   at the docked box. The page stays in the overlay, where the game needs it.
   The overlay is up whenever a film is `full` *or* a game is running, and the
   two no longer argue about whether it exists.

3. **The page decides, as it already does.** `nativeLayout` gains one input:
   whether a game screen is up. A docked film over a game is `pip`; docked
   anywhere else, it is `mini` as before. The client places windows and never
   decides where a picture belongs.

4. **No new player, and no second set of controls.** The card is the docked
   mini-player: its transport, its title button back to the full player, and
   its cover art for music. A second card built for games would be a second bar
   that agrees with the first by coincidence, which ADR 0029 already refused.

5. **Two sounds, two volumes.** A film keeps the player's own volume. A game
   gets its own volume in its menu, applied by scaling the samples in the
   session.

   It is not done with `waveOutSetVolume`. On Windows Vista and later that
   sets the volume of the whole application's audio session, which libmpv
   shares, so turning the game down would turn the film down with it.

6. **The keyboard and the pad belong to the game.** The card answers to the
   mouse. Nothing a person does with a controller reaches the film by accident.

## What this does not decide

- **A picture that floats over other applications.** A window that stays on top
  with LANcast minimised was considered and deferred. It needs controls of its
  own outside the page, and the in-app corner is what was asked for first.
- **A game in the corner.** Only films, episodes and music go into the card. A
  game you are not looking at is a game you are not playing, and pausing it is
  already one button away.
- **Which picture gets bigger.** The game always has the screen and the film
  always has the corner. Swapping them is a later question, if it is ever
  asked.

## Consequences

**Good:**
- **Films join music.** The case that delighted somebody with music works for
  films, with no new surface to learn.
- **Renderers stop sharing a window.** A game and libmpv no longer trade a
  window between them, which removes a hand-over, a `stopVideo`, and a GL pixel
  format left on a window libmpv will reuse.
- **Contained change.** No schema change and no API change: it is the desktop
  client and the page.

**Costs:**
- **Two pictures cost two decoders.** A 4K HDR film is CPU tone-mapped on some
  machines (memory: the 4K stutter was the CPU tone map), and a game beside it
  competes for the same cores. If that bites, it is solved in the film's
  quality choice, not here.
- **A fourth window to keep in step.** Position, visibility, owner and focus
  (`overlay.go`). That file already does this for two windows, and the game
  window follows the video window's rules.

## Revisit when

- Somebody asks for the picture to float over other applications.
- A game needs to be shrunk to the corner.
- A third native renderer appears and "one window each" stops scaling.
