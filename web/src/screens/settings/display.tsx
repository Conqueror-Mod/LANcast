import { useState } from "react";
import {
  capabilities,
  claimable,
  clearDenials,
  deniedCapabilities,
  restore,
  withhold,
  withheldCapabilities,
} from "@/playback/capabilities";
import { forgetHLS, hlsRecord } from "@/playback/fileTransport";
import { useBigscreen } from "@/lib/bigscreen";
import { useLiveTransport } from "@/lib/liveTransport";

/*
 * Display — the device's own view of the app.
 *
 * A device pane rather than a server one, and that is the substance of the
 * choice: "make everything larger because I am ten feet away" is a fact about
 * the room this device is in. Syncing it would shrink the phone in somebody's
 * hand because the television downstairs is a television.
 *
 * The spotlight and spoilers lived here too until settings-3-plan step 3, for
 * want of anywhere per account to put them. They are on Your account › Home now
 * (ADR 0120).
 */
function DisplaySection() {
  const [bigscreen, setBigscreen] = useBigscreen();
  const [liveTransport, setLiveTransport] = useLiveTransport();

  return (
    <section className="settings__section">
      <span className="section-label">Display</span>

      {/*
       * Live TV transport, and it is deliberately a device setting rather than
       * a server one: what a browser can do with MediaSource is a fact about
       * this machine, not about the library.
       *
       * Off by default, and staying that way until the new path has been lived
       * with — step 4 of the ADR 0013 amendment. The old path is one toggle
       * away for exactly as long as that takes.
       */}
      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={liveTransport === "mse"}
          onChange={(e) =>
            setLiveTransport(e.target.checked ? "mse" : "progressive")
          }
        />
        <span>
          <strong>Improved live TV playback</strong>
          <span className="set-toggle__desc">
            Plays channels through a segmented stream instead of one long
            response, which lets the player see how much it is holding rather
            than guessing. It should stutter less and stop drifting behind live.
            New, and off by default — if a channel behaves worse with this on,
            turn it off and it will play the way it always has. Applies on this
            device only.
          </span>
        </span>
      </label>

      <label className="set-toggle set-toggle--described">
        <input
          type="checkbox"
          checked={bigscreen}
          onChange={(e) => setBigscreen(e.target.checked)}
        />
        <span>
          <strong>Bigscreen mode</strong>
          <span className="set-toggle__desc">
            Scales the whole interface for a television across the room. The
            same screens, larger — not a separate client. Applies on this device
            only, survives a restart, and toggles with <kbd>Ctrl</kbd>{" "}
            <kbd>Shift</kbd> <kbd>B</kbd> from anywhere, so you can get back out
            without finding this page again.
          </span>
        </span>
      </label>

    </section>
  );
}

/*
 * What this browser is claiming it can decode, and what it has stopped claiming.
 *
 * Sits under the re-probe row because it is the mirror image of it: that row is
 * what the *server* knows about your files, this is what the *client* says
 * about itself. Both go stale, and neither used to say so.
 *
 * It exists because this state was invisible and wrong at the same time. A real
 * install was found withholding every claim it is capable of making — hevc,
 * hevc10, ac3, eac3 — and had been serving a full 4K re-encode of every HEVC
 * film as a result. Clearing it made the same file direct-play with no ffmpeg.
 * Nothing had failed: falling back is correct behaviour, so the symptom was
 * only ever "this seems to work hard".
 *
 * Denials now expire on their own, so the button is the impatient path rather
 * than the only one — someone who has just installed a codec extension should
 * not wait a fortnight to learn whether it took.
 */
function CodecDenialsRow() {
  const [denials, setDenials] = useState(() => deniedCapabilities());
  const [claims, setClaims] = useState(() => capabilities());
  const [off, setOff] = useState(() => withheldCapabilities());
  /*
   * Segmented delivery is remembered the same way a codec is, so it is cleared
   * by the same button.
   *
   * It is a capability this device was observed failing at, and until now there
   * was no way back from that observation at all — the only lever was bumping a
   * storage key and shipping a release, which is a migration wearing a
   * constant's clothes. It cost a real machine the better delivery path
   * permanently, on one refusal of a playlist later proven fine.
   */
  const [segments, setSegments] = useState(() => hlsRecord().verdict);
  const offered = claimable();

  function refresh() {
    setDenials(deniedCapabilities());
    setClaims(capabilities());
    setOff(withheldCapabilities());
    setSegments(hlsRecord().verdict);
  }

  function reset() {
    clearDenials();
    forgetHLS();
    refresh();
  }

  function toggle(name: string, on: boolean) {
    if (on) restore(name);
    else withhold(name);
    refresh();
  }

  /*
   * Two mechanisms turn a codec off, and the summary used one word for both.
   *
   * A *denial* is automatic and temporary: recorded when a direct play fails,
   * expiring after a fortnight, cleared by the button. A *withholding* is
   * manual and permanent: the checkboxes below. The line said "Withheld after a
   * failure: hevc, ac3" — which is a denial, described with the word the
   * checkboxes own — directly above those same checkboxes, ticked.
   *
   * So the panel contradicted itself on screen: it named codecs as withheld
   * while showing them as not withheld. Both statements were true about
   * different things and there was no way to tell from reading it.
   *
   * Named rather than counted, still: "2 off" invites a shrug, and "hevc, ac3"
   * is the thing a person can connect to a film that plays badly.
   */
  const parts: string[] = [];
  if (denials.length > 0) {
    parts.push(`off after a failure: ${denials.map((d) => d.name).join(", ")}`);
  }
  if (off.length > 0) {
    parts.push(`turned off by you: ${off.join(", ")}`);
  }
  // Named alongside the codecs because it is the same kind of fact and has the
  // same consequence: a file this device could have been sent in pieces is sent
  // as one long response instead, which is what makes a long film stutter.
  if (segments === "refused") {
    parts.push("segmented delivery: off after a failure");
  }

  let sub: string;
  if (offered.length > 0 && !claims) {
    /*
     * The state this row was built to make visible, said outright.
     *
     * A browser that claims nothing has every affected file converted by the
     * server instead — a full re-encode of every HEVC film, on a real install,
     * for as long as it went unnoticed. Nothing fails, so the only symptom is
     * that the server seems to work hard, and the previous wording buried the
     * cause in a word that appeared to disagree with the ticks underneath it.
     */
    sub = `Claiming nothing, so the server converts anything that needs these — ${parts.join("; ")}.`;
  } else if (parts.length > 0) {
    sub = `Claiming ${claims.split(",").join(", ")}; ${parts.join("; ")}.`;
  } else if (claims) {
    sub = `Claiming ${claims.split(",").join(", ")}.`;
  } else {
    // No claims and nothing turned off is the ordinary state of a browser that
    // simply decodes none of the optional codecs, and is not a fault.
    sub = "This browser decodes none of the optional codecs.";
  }

  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">Playback codecs</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        {/* Only the automatic half. Clearing denials is not "turn everything
            back on": a codec somebody switched off by hand stays off, because
            that was a judgement about a picture and this button is about
            failures. The title says so, since the label cannot. */}
        <button
          className="set-btn"
          disabled={denials.length === 0 && segments !== "refused"}
          onClick={reset}
          title="Clears what was turned off automatically after a failure — codecs, and segmented delivery. Ones you turned off stay off."
        >
          Try them again
        </button>
      </div>

      {/*
       * Turning a codec off by hand, which the automatic net cannot do.
       *
       * A denial is recorded when a direct play *fails*. A file that decodes
       * in hardware and stutters has not failed — measured on a real film:
       * HEVC Main 10 direct-played with the decode engine at 8-9%, the server
       * idle, and a juddering picture in two different browsers, while the
       * same file re-encoded played perfectly. Nothing in here can see the
       * difference between a smooth picture and a rough one. A person can.
       *
       * Only what this browser actually claims is offered: a switch for a
       * codec the engine never claimed would be a control that does nothing.
       */}
      {offered.length > 0 && (
        <div className="set-codecs">
          <div className="set-codecs__note">
            If a file plays but stutters, the browser is claiming a codec it
            cannot really keep up with. Turn it off here and the server will
            convert those files instead.
          </div>
          {offered.map((name) => {
            const on = !off.includes(name);
            return (
              <label className="set-codecs__item" key={name}>
                <input
                  type="checkbox"
                  checked={on}
                  onChange={(e) => toggle(name, e.target.checked)}
                />
                <span>{name}</span>
              </label>
            );
          })}
        </div>
      )}
    </div>
  );
}

export function DisplayPane() {
  return (
    <>
      <DisplaySection />
      {/* On the device pane rather than the admin Playback one: a
          denial is stored per browser, so the person it slows down is
          the one sitting here, who may not be an administrator. */}
      <section className="settings__section">
        <span className="section-label">Playback</span>
        <CodecDenialsRow />
      </section>
    </>
  );
}
