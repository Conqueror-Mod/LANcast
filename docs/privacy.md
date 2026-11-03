# Privacy

LANcast is a server you run yourself. Your libraries, your accounts, what
everyone watched and played, and every setting live on that server, in its
own data folder, and nowhere else. There is no LANcast account, no LANcast
cloud, and nothing that reports to the developer on its own.

This page lists every way LANcast reaches past your own network, what it
sends, and what has to happen first. It is written from the code, and it
changes when the code does.

## What the developer receives

**Nothing, unless you send it.** LANcast has no analytics, no telemetry, no
crash reporting that runs by itself, and no install identifier.

The one exception is **Report an issue**. It sends a report only when you
press Send, and only what its form showed you. It goes to a private channel
the developer reads (a Discord webhook built into release builds). A build
you compiled yourself cannot send one; it offers to copy the report instead.

## What is on unless you turn it off

- **The update check.** LANcast asks GitHub's releases API whether a newer
  version exists. It's a plain request carrying no identifier, no library
  details and no version history; GitHub sees your address because every
  request carries one. Turn it off in Settings and nothing else changes.
  Downloading an update happens only when an administrator presses the
  button, and only a release signed by the project is installed.
- **Film and TV details from TMDB.** When a library is scanned, LANcast asks
  The Movie Database about the titles it finds (names, years and TMDB ids)
  and fetches posters and artwork from TMDB's image server. Release builds
  carry a key for this. You can use your own key, or turn the built-in keys
  off in Settings; with no key at all, nothing is asked. Automatic
  enrichment can also be turned off.
- **Album details from MusicBrainz.** LANcast asks musicbrainz.org about the
  albums in a music library — each album's title, its album artist and its
  year — and, for a match, the record's details. It needs no key and sends no
  identifier beyond the program's name and version, which MusicBrainz asks
  every client to send. It asks about one thing a second. Turn off
  "Identify albums with MusicBrainz" in Settings and nothing is asked; matches
  already made stay. Genres it supplies are its users' work, shared under
  CC BY-NC-SA, and are shown with that credit.

## What happens only when someone asks

- **Subtitles from OpenSubtitles.** When someone searches for subtitles, the
  server sends OpenSubtitles the title, its IMDb or TMDB id and a hash of the
  video file, and downloads the subtitle chosen. Release builds carry a key;
  an optional OpenSubtitles login raises the download allowance.
- **Ratings from OMDb**, only if an administrator enters an OMDb key: the
  IMDb ids of films and shows, for IMDb, Rotten Tomatoes and Metacritic
  ratings.
- **Media tools.** ffmpeg is downloaded from GitHub when an administrator
  presses the button, after being told what it is and its licence.
- **Face recognition models**, the same way: from GitHub and Hugging Face,
  when an administrator asks. Faces are then found and matched on your
  server; no picture leaves it.
- **Emulator cores.** The desktop app downloads its cores from libretro when
  someone asks it to, and the server downloads the browser player's cores
  when an administrator asks; see [cores.md](cores.md).
- **Game databases.** The lists games are identified against come from
  GitHub when an administrator asks. Identifying a game is then done on your
  server.
- **Retro game artwork**, if an administrator turns it on (off by default).
  LANcast asks libretro's thumbnail server for each identified game's box
  art, by the game's name.
- **A public certificate**, if an administrator turns on a public name. The
  server asks the certificate authority they chose (Let's Encrypt, by
  default) for a certificate, and the authority connects back to that name
  to check it.
- **Trailers.** The server returns only a trailer's identity. When someone
  presses play, their browser or app loads it straight from YouTube (its
  no-cookie player) or Vimeo. LANcast doesn't sit in between, and nothing is
  loaded until play is pressed.
- **Places in photos**, if an administrator turns it on (off by default).
  Where a photo was taken is read from the file and matched against a list
  of places built into LANcast. No location is looked up online. Turning it
  off deletes every location read.

## RetroAchievements, for each person who connects

Nothing reaches RetroAchievements until a person connects their own account
on their profile. It's per person and off until then.

- **Reading your progress:** with the Web API key you enter, the server reads
  your profile, recent unlocks and game progress from RetroAchievements.
  Badges and pictures come through your own server, so the device you're
  looking at never contacts RetroAchievements.
- **Earning while playing**, if you also turn that on and type your
  RetroAchievements password once:
  - The password goes to RetroAchievements through your server, and is
    swapped for a sign-in token. The password is not kept; the token is kept
    on the server, encrypted, and never sent to the device you play on.
  - While you play a game in the desktop app, your server sends
    RetroAchievements what any approved emulator does: which game it is (a
    hash of the game file), the start of the session, regular pings,
    achievements you unlock, and rich presence, a line such as "Level 3, 2
    lives". It also sends the app's version, the emulator core and the
    system, as RetroAchievements asks.
  - Rich presence can't be turned off on its own; turning earning off stops
    all of it.
  - An unlock that can't be sent yet is kept on the server until
    RetroAchievements answers.
- **Turning it off** deletes the token and any unlocks still waiting.
  Disconnecting the account deletes the key as well.

## Other servers, and services you choose

- **Paired servers.** LANcast talks to another LANcast server only after both
  sides have agreed to pair. What one server can see of the other's library
  is decided by sharing settings on each side. Who is watching what is shown
  to a paired server only for a person who has switched that on for
  themselves; it is off for everyone until they do, and it is never stored.
- **Webhooks**, if an administrator adds any: the events they chose are sent
  to the addresses they entered, signed, and to nobody else.
- **Live TV**, if set up: channel lists, guides and streams come from the
  addresses an administrator entered.
- **Plugins** can't use the network unless their manifest asks for it and
  an administrator installs them. Even then, they can only reach the hosts
  they named.

## Inside your network

The desktop app, phones and browsers talk to your server and nothing else,
apart from the trailers above and the desktop app's own cores. DLNA, which lets TVs on your network find
LANcast, is off by default. Logs and backups stay in the server's data folder.

The Windows desktop app draws its window with Microsoft's WebView2 runtime.
That runtime is part of Windows and is covered by Microsoft's own privacy
terms; LANcast adds nothing to what it sends.

## Changes

This page changes in the same commit as the code it describes, and its
history is the repository's. Questions go to the issue tracker on GitHub.
