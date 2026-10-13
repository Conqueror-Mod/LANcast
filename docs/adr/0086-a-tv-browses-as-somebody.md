# ADR 0086 — A TV browses as somebody

Date: 2026-10-13 · Status: **accepted** 2026-10-13

The plan is [settings-plan.md](../settings-plan.md), step 6, "DLNA, briefly".
Chris decided on 2026-10-13 that DLNA is built next, and that **a TV browses as
an account an administrator chooses in Server › DLNA**.

## Context

DLNA (UPnP AV) lets a TV, a console or a speaker find a media server on the
network and play from it with no app. Smart TVs, game consoles, VLC and
Kodi all speak it. It is the one way a household device with no LANcast client
can use the library.

It conflicts with three rules:

- **Every request is someone's.** LANcast has no anonymous access. Every route
  is a session, a ticket or a peer. DLNA has no sign-in at all; anything on the
  network may browse.
- **No phone-home, and nothing unasked.** Discovery works by multicast on the
  LAN. That never leaves the network, but it is the first time LANcast would
  announce itself without being asked.
- **An unsecured server binds loopback only.** DLNA is by definition for other
  machines.

## Decision

1. **Off by default.** Nothing is announced or opened until an administrator
   turns it on.
2. **It answers as one chosen account** (Chris's decision). Server › DLNA
   names an account; a TV sees exactly what that account sees: its visible
   libraries and its rating limit (ADR 0084), enforced by the same store
   chokepoints, because every lookup goes through `GetItem` and `ListItems`
   as that account. Choose a child's account and every TV in the house is
   child-safe. DLNA does not run with no account chosen. An administrator
   account can be chosen; the screen says what that means.
3. **Only where the loopback rule already allows the network.** DLNA runs
   only on a server bound to the LAN, which means a password is set. It never
   relaxes the gate.
4. **LAN clients only.** The DLNA listener refuses any connection whose
   address is not private, link-local or loopback. Port forwarding or a
   public IPv6 address cannot expose it. This check is per connection, not a
   firewall rule (the installer never touches the firewall).
5. **Its own plain-HTTP port, 8200.** TVs do not accept LANcast's self-signed
   certificate, and most cannot do TLS to a LAN server at all, so DLNA cannot
   share the HTTPS listener. It is a separate listener serving only the DLNA
   routes:
   - the device description;
   - the service descriptions;
   - Browse;
   - media files and artwork, by item id, as the chosen account.

   It does not serve the LANcast API. 8200 is the port other home media
   servers use for this.
6. **Discovery by SSDP**, multicast on UDP 1900 on the LAN interfaces:
   - it answers searches;
   - it announces itself periodically (`ssdp:alive`);
   - it says goodbye (`ssdp:byebye`) when DLNA is turned off or the server
     stops.

   The device's UDN is derived from the server's identity (ADR 0044), so a TV
   that remembers the server finds the same one after a restart.
7. **Browse mirrors the library tree.** The root holds one folder per
   library the account can see. A library holds its top-level titles. A show
   holds seasons, a season holds episodes, an artist holds albums, and an
   album holds tracks. Films, episodes, tracks and photographs are playable
   items with their file as the resource and their poster as album art.
8. **Files are sent as they are.** There is no transcoding in this first
   build: a TV plays what it can and refuses the rest. Every file goes through
   the same containment check as every other file route (CLAUDE.md).
9. **Playing over DLNA records nothing.** DLNA has no way to report a
   position. Nothing is written to the account's history, Continue Watching
   or watched state. A shared living-room TV writing into one person's
   history would be wrong anyway.

## Firewall

Windows blocks inbound connections to a service unless a rule allows them, and
LANcast's installer deliberately creates none. DLNA needs **TCP 8200** and
**UDP 1900** allowed inbound on private networks. The DLNA pane says so, and
gives the exact command, rather than letting a TV that finds nothing look like
a LANcast bug.

## Consequences

- **Settings:**
  - `dlna_enabled` (false);
  - `dlna_account` (an account id, empty until chosen);
  - `dlna_name` (what TVs show, default "LANcast").

  Changing any of them takes effect without a restart.
- **A new package, `internal/dlna`,** holds the protocol: SSDP messages, the
  description documents and DIDL-Lite. These are pure functions, tested
  without a network, beside the store and the HTTP layer, as `probe`'s
  decision is kept apart from ffmpeg.
- **Server › DLNA** holds:
  - on/off;
  - the account;
  - the name;
  - whether it is running, and why not if it isn't;
  - the firewall note.

## Alternatives rejected

- **Answering as an anonymous "guest" with everything visible.** That breaks
  "every request is someone's", and puts every library on every screen in the
  house.
- **Serving DLNA on the HTTPS listener.** TVs would not connect, and it would
  mix an unauthenticated protocol into the listener every authenticated route
  shares.
- **A third-party UPnP library.** The protocol subset a media server needs is
  small and stable. LANcast keeps its dependencies few, and the parts worth
  testing are string-building that the standard library does fine.
