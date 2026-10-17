# ADR 0088 — A certificate you bring is followed, and cannot stop the server

Date: 2026-10-17 · Status: **accepted** 2026-10-17 · Amends [ADR 0014](0014-transport-security.md)

The plan is [settings-plan.md](../settings-plan.md), Network: "Custom TLS
certificate — `tls_cert_file` and `tls_key_file` exist in config with no
control." Chris decided on 2026-10-17:
- **File paths, not uploads.** A renewal tool rewriting those files keeps
  working, with no re-upload and no private key crossing the connection.
- **A certificate that will not load is served around, and the reason shown.**
  It never stops the server starting.

## Context

ADR 0014 made bring-your-own-certificate the recommended production setup and
kept it **off the settings API**: certificate paths were "a
filesystem/deployment concern", they took effect only on restart, and keeping
them off the remote API kept the surface small.

Three things have changed since then.

- **An administrator can already name any path.** Library creation and
  filesystem browsing take arbitrary paths, which is exactly why the loopback
  rule exists. Naming a certificate file adds no new read capability, and
  these routes never return the key or any file's contents.
- **A bad path stopped the server.** `loadTLSCert` returned an error at
  startup, and startup gave up. A file moved, a certificate expired over a
  holiday, or a key replaced by a renewal tool's half-finished write meant
  LANcast would not start. The way back in was editing `config.json` by hand.
- **The desktop window on this PC pins the generated certificate's key.**
  `serverCertPin` reads `<data>/tls/cert.pem` and the window trusts that key
  and nothing else (ADR 0070). Serving any other certificate to it is a
  security warning inside a native window, then a blank page. So the custom
  path had never been usable with the app's own window. Nobody had noticed
  because nobody had used it.

## Decision

**1. Two paths, set in Server › Network.** The routes are
`GET`/`PUT`/`DELETE /api/settings/custom-certificate`, admin only and
audited. `PUT` reads and checks the pair before saving anything: the files
must be full paths, readable by the server, a matching pair, and in date.
Otherwise it answers `400` with which check failed, so the mistake shows up
on the form rather than as a browser warning on somebody's phone. Surrounding
quotes, as Windows' *Copy as path* adds them, are removed.

**2. Applied at once, with no restart.** Certificates are chosen per handshake
by `tlscert.Serving`.

**3. The files are followed.** Every 30 seconds, at most, the two files are
checked for changes, and re-read when they have changed. A renewal tool
(win-acme, certbot, a script) rewriting them in place is picked up without a
restart. If a new copy will not load, the previous copy keeps being served
while it is still in date. A renewal caught halfway through writing is the
likeliest cause, and the next check sees the finished files.

**4. It cannot stop the server.** At startup, and whenever the served copy
expires, a pair that will not load means the **generated** certificate is
served instead, and `error` says why, on the Network page and in the log.
The connection is still HTTPS, and other devices see a certificate warning
until it is fixed.

This reverses ADR 0014's startup behaviour on purpose. The cost is a warning
on other devices. The cost of refusing to start is an administrator locked
out of a server they can only repair by hand.

**5. The generated certificate always exists, and keeps three jobs.**
- **Connections arriving on a loopback address** get it. That is this PC's
  own window, which pins its key.
- **The public certificate (ADR 0081)** is issued for its key. Choosing or
  removing a custom certificate therefore never costs a reissue from the CA.
- **It is the fallback** whenever a custom certificate can't be used.

**6. Order per handshake:**
1. the CA's validation handshake (ADR 0081);
2. peers, by ALPN (ADR 0044);
3. the public certificate's own name;
4. loopback, which gets the generated certificate;
5. everything else, which gets the custom certificate when it loads, and the
   generated one otherwise.

`GET /api/network`'s `certificate_fingerprint` and `custom_certificate`
describe what step 5 serves right now. That fingerprint is what another
device is shown when it asks whether to trust this server.

## Consequences

- **A key change at another device.** A LANcast desktop app on another
  machine that already knows this server sees the new key once, as a
  rotation, and is asked once (ADR 0070's corrected rule). Browsers that
  trust the new certificate's authority stop warning.
- **A browser on this PC that visits `https://localhost`** still gets the
  generated certificate. It should visit by the name the certificate covers.
- **Nothing here copies, stores or returns key material.** The server's
  account must be able to read the files, and saving checks that as the
  service itself.
- **A combination nobody had before:** a public certificate stored before
  this change, from a server that had a custom certificate at startup, was
  issued for the custom key. It is ignored once ("for a different key") and
  issued again for the generated key. No such install is known.
