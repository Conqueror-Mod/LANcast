// What this browser can actually decode.
//
// The server's default profile is a floor: it assumes only what every browser
// manages, because guessing generously for a client it cannot identify is how
// black rectangles happen (docs/api.md). The consequence is that a browser
// which decodes HEVC in hardware — Chromium on Windows with a capable GPU, and
// the WebView2 window LANcast now ships — is still served a full re-encode of
// every HEVC file, which is seconds of waiting per film for nothing.
//
// The engine knows the answer and can be asked. This asks it, once, and the
// answer rides along with the playback request (docs/client-capabilities-plan.md).

// Capabilities the server understands. Sending something outside this set is
// harmless — unknown claims are ignored — but there is no reason to.
const PROBES: Record<string, string> = {
  // Main profile, 8-bit.
  hevc: 'video/mp4; codecs="hvc1.1.6.L93.B0"',
  /*
   * Main 10, asked separately because it is a separate question.
   *
   * The 8-bit string above was being taken as covering both, and it does not:
   * on Windows the engine can answer "probably" for Main and still decode Main
   * 10 badly enough to glitch. Reported as a film that direct-played with
   * perfect audio and a stuttering picture — which is the worst shape of this
   * bug, because nothing fails. The safety net that records a failed claim and
   * stops making it never fires, since playback technically worked.
   *
   * `hvc1.2.4.L120.B0`: profile 2 (Main 10), level 4.0. The server treats it as
   * permission for a bit depth rather than a codec, so a client that cannot
   * answer for it gets the file transcoded instead.
   */
  hevc10: 'video/mp4; codecs="hvc1.2.4.L120.B0"',
  ac3: 'audio/mp4; codecs="ac-3"',
  eac3: 'audio/mp4; codecs="ec-3"',
  /*
   * FLAC and Opus *inside MP4*, which is not the same question as whether this
   * browser can play a .flac or a .opus file — it can, and the server's floor
   * already assumes so.
   *
   * The server could not previously copy either into fragmented MP4, so a file
   * needing only a container rewrite had its audio re-encoded; for FLAC that is
   * lossless turned into AAC to change a box. It can carry them now, but only
   * on a client that says it can read them there, because FLAC-in-MP4 is legal
   * by spec and not universally decodable.
   *
   * Asked separately from each other for the reason hevc10 exists: they are two
   * engine answers, and treating one as covering the other is exactly the
   * mistake that shipped a stuttering Main 10 picture.
   */
  flacmp4: 'audio/mp4; codecs="flac"',
  opusmp4: 'audio/mp4; codecs="opus"',
};

// Capabilities this browser has been caught lying about.
//
// `canPlayType` answers "probably", never "definitely": HEVC in particular
// depends on the GPU and, on Windows, on a codec extension that may not be
// installed. When a direct-played file fails, the claim that produced it is
// recorded here and never made again on this machine — otherwise every HEVC
// file would fail the same way, once each, forever.
const DENIED_KEY = "lancast:codec-denied";

function denied(): Set<string> {
  try {
    const raw = localStorage.getItem(DENIED_KEY);
    return new Set<string>(raw ? JSON.parse(raw) : []);
  } catch {
    return new Set();
  }
}

/**
 * deny records that a capability did not work here, so it stops being claimed.
 * Returns true when this is news — the caller retries only on news, which is
 * what stops a retry loop.
 */
export function deny(capability: string): boolean {
  const set = denied();
  if (set.has(capability)) return false;
  set.add(capability);
  try {
    localStorage.setItem(DENIED_KEY, JSON.stringify([...set]));
  } catch {
    // A browser with no storage still works; it just re-learns each session.
  }
  return true;
}

let cached: string | null = null;

/**
 * capabilities is the `can=` value for this browser: the codecs it claims,
 * minus any it has already failed to play.
 *
 * Measured once. The answer cannot change while the page is open, and asking
 * per playback would be a media element per film for no new information.
 */
export function capabilities(): string {
  if (cached !== null) return cached;

  const el = document.createElement("video");
  const no = denied();
  const can: string[] = [];

  for (const [name, type] of Object.entries(PROBES)) {
    if (no.has(name)) continue;
    // "probably" and "maybe" are the only positive answers the API gives, and
    // "maybe" is too weak to burn a failed playback on — it is the answer for
    // "this container might hold something I can decode".
    if (el.canPlayType(type) === "probably") can.push(name);
  }

  cached = can.join(",");
  return cached;
}

/** Forget the measurement, so the next call re-reads it. For after a denial. */
export function resetCapabilities() {
  cached = null;
}

/** Appends `can=` to a URL, or returns it unchanged when there is nothing to say. */
export function withCapabilities(url: string): string {
  const can = capabilities();
  if (!can) return url;
  return url + (url.includes("?") ? "&" : "?") + "can=" + encodeURIComponent(can);
}
