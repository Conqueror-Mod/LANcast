# LANcast for Roku

A thin client for a LANcast server ([ADR 0125](../../docs/adr/0125-a-tv-pairs-by-code.md)), written in BrightScript and SceneGraph. The server makes every decision; the app shows the result and plays what it is sent.

## What it does so far (roku-plan step 3)

1. **Address.** You type the server's address once. An IP address or a bare machine name gets LANcast's port, 8080, unless you type one. A dotted name is treated as a public name on 443.
2. **Trust.**
   - A public name has a certificate from a public CA, so it is checked against Roku's own CA list and is never pinned.
   - Anything else is read once without checking. That certificate is what the pairing code is bound to, and every request after it is pinned to it, the pairing itself included (ADR 0125 §2, amendment 1).
3. **Pairing.** The TV computes an eight-character code from the server's nonce and the certificate it read, then shows it. Someone types the code into LANcast at Settings › Security › Pair a device, or at `/pair`. The TV polls until the code is approved, then keeps the API key it is given.
   - If a machine in the middle served the TV its own certificate, the TV shows a code the server never issued, and approval fails.
4. **Home.** Once signed in, the screen shows who is signed in and their libraries. Browsing is step 4.
5. **Signed out.** The TV asks to be paired again when:
   - the key is revoked in Settings;
   - the server's certificate changes;
   - you choose Sign out.

On every start-up, the app runs the server's own test vector through the TV's SHA-256 and alphabet, and prints the result to the debug console. If they disagree, the pairing screen says so.

**Focus** follows [design.md](../../docs/design.md): gold means only *where you are*.
- At rest, an edge is gold at 20% strength.
- On focus, the edge is full gold with a 13% halo.
- Only one item is ever gold at a time.

## Build

```powershell
cd clients/roku
npm ci
npm run build        # out/lancast-roku.zip
```

CI runs `bsc --create-package false`, which compiles and lints, and fails on any error.

## Sideload

1. Turn on developer mode on the Roku: Home ×3, Up ×2, Right, Left, Right, Left, Right.
2. Open the address it shows in a browser, signing in as `rokudev`.
3. Upload `out/lancast-roku.zip`.

To see the debug console, including the self-check line:

```powershell
telnet <roku-ip> 8085
```
