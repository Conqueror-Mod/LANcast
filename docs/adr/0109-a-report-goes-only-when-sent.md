# ADR 0109 — A report goes only when it is sent

Date: 2026-10-31 · Status: **accepted** · Replaces the mail link in `lib/contact.ts`

## Context

Report an issue was a `mailto:` link with the version filled into the body.
It was chosen so LANcast would never make a request of its own to its
developer (README, *No phone-home*): the person's own mail program sent it.

It didn't work where most people use LANcast. In the desktop window, a
`mailto:` opened Windows' "how do you want to open this" box and then
nothing. A machine without a mail program configured has nowhere to hand it.
And a mail link cannot carry an attachment, so the one thing that answers
most reports, the server log, could never go with it.

## Decision

**A form in the app, and the server sends it.** Report an issue opens a
dialog: what happened, what was expected, the steps, and optionally how to
reach the sender. **Send** asks this server to post it, as one message, to a
Discord webhook in the developer's private channel. Nothing opens a browser
or a mail program.

**Only when Send is pressed.** This is the one request LANcast makes to its
developer, and it is the person's own action, the same standing the
certificate request has once the public name is turned on. The principle
says the server does no network things you did not ask for; pressing Send is
asking. There is no background report, no crash upload, no "send
automatically next time".

**What goes with it is said beside the button.** LANcast's version, the
server's OS and architecture, which client sent it, and whether the sender
is an administrator. Never an account name, an address, or the server's
identity, unless the person types it.

**The log is the operator's to share.** Only an administrator can attach the
server log (the last 2,000 lines, at most 1 MB), because it names folders,
addresses and other people's accounts. The form lets them read every line
before ticking the box.

**The destination is never in the repository.** Discord is a GitHub
secret-scanning partner: a webhook URL pushed to a public repository is
revoked within minutes, and scrapers that find one first spam it. The URL is
stamped into release builds (`-X lancast/internal/report.WebhookURL`) from
the `LANCAST_REPORT_WEBHOOK` Actions secret. A build without it, which is
any local or self-built one, says it cannot send and offers **Copy report**
instead. The same environment variable at run time points a server
elsewhere: a self-builder's own channel, or a test listener.

**Limited.** Five reports an hour for the whole server and one every 20
seconds, so a stuck button cannot flood the channel. Mentions are switched
off in the message, so typed text cannot ping anyone. Each send is audited
as `report.send`.

## Consequences

- Reports work from the desktop window, a phone and a browser alike, and can
  carry the log.
- The URL is extractable from a release binary by anyone determined. The
  worst they can do is post to that one channel; if it is abused, the
  webhook is deleted and a new secret goes into the next release, and older
  builds then fail to send and offer copy.
- The developer's contact address stays on Settings › About as text to copy,
  not a link.
- A reply goes only to whatever contact the sender chose to type.

## Amendment, 2026-11-01: pictures

A report may carry up to four pictures, chosen or pasted into the form and
shown there before Send. Anyone who may send may attach them: unlike the log,
a picture is something the sender chose and has seen.

The server never forwards a picture as it arrived. It decodes each one (PNG,
JPEG, GIF or WebP, at most 6 MB, refusing a header that claims more than 50
million pixels) and encodes it again, at most 2,560 pixels on its long edge
and 2 MB. That proves it is a picture, and it drops metadata: a phone photo
records where it was taken, and attaching it to a bug report is not consent
to send that location. Four pictures and a full log stay under the 10 MB
Discord takes in one message. A picture that will not open is refused before
the rate limit counts the send.
