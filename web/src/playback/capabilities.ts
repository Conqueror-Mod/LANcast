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
  /*
   * 10-bit H.264 — High 10, `avc1.6e0033`: profile_idc 110 (0x6e), level 5.1.
   *
   * Named for the profile rather than the bit depth because "High 10" belongs
   * to H.264 alone, where HEVC's is Main 10 — so the claim cannot be misread as
   * covering both, which is what `hevc` was and what `hevc10` had to undo.
   *
   * Worth asking rather than assuming in either direction. The server's floor
   * excludes it because High 10 is in no browser's baseline, and that was read
   * for years as "browsers cannot do this"; Chromium answers `probably`. It is
   * most of an anime library, and every one of those files was a full video
   * re-encode of something the engine can play.
   */
  high10: 'video/mp4; codecs="avc1.6e0033"',
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

/*
 * Capabilities this browser has been caught lying about.
 *
 * `canPlayType` answers "probably", never "definitely": HEVC in particular
 * depends on the GPU and, on Windows, on a codec extension that may not be
 * installed. When a direct-played file fails, the claim that produced it is
 * recorded here and stops being made — otherwise every HEVC file would fail the
 * same way, once each, forever.
 *
 * # Why these expire
 *
 * They used to be permanent, and permanence turned a safety net into a ratchet.
 *
 * Found on a real install: this key held `["hevc","hevc10","ac3","eac3"]` —
 * every claim the client is capable of making. The machine had been serving a
 * full 4K HEVC re-encode *and* an audio re-encode for every such film, a core
 * per viewer, for an unknown length of time. Clearing it and replaying the same
 * file direct-played with no ffmpeg at all, so all four denials were false.
 *
 * Nothing surfaced any of it. The fallback is correct behaviour, so a machine
 * quietly downgraded for ever looks exactly like a machine working properly.
 *
 * The reason a denial goes stale is that the thing it describes is not a fact
 * about the codec, it is a fact about this machine *today*: a GPU driver
 * updates, WebView2 updates, someone installs the HEVC Video Extensions. None
 * of those can announce themselves here, and a denial that cannot expire will
 * never notice any of them.
 */
const DENIED_KEY = "lancast:codec-denied";

/*
 * How long a denial stands before the claim is tried again.
 *
 * The asymmetry decides this. Retrying too eagerly costs one failed direct
 * play, which `retryWithoutClaims` already recovers from in a second or two.
 * Retrying too rarely costs a needless full re-encode on every affected file
 * until someone notices — and nobody noticed for months.
 *
 * A fortnight keeps a genuinely-unsupported codec down to one hiccup every two
 * weeks, while a real fix — the codec extension finally installed — pays off
 * without waiting on a release.
 */
export const DENIAL_TTL_MS = 14 * 24 * 60 * 60 * 1000;

/*
 * Reads the store, dropping anything that has aged out.
 *
 * The legacy shape — a bare array of names, written when denials were for ever
 * — is read as *expired* rather than as denied-just-now. Every install carrying
 * one has been sitting on it for an unknown time, and the one on which this was
 * found was wrong about all four entries. Giving them one retry on upgrade is
 * the whole repair, and a denial that is still true simply comes straight back.
 */
function denials(): Record<string, number> {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(DENIED_KEY);
  } catch {
    return {};
  }
  if (!raw) return {};

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  if (Array.isArray(parsed)) return {}; // legacy: expire it

  const now = Date.now();
  const cutoff = now - DENIAL_TTL_MS;
  const out: Record<string, number> = {};
  let corrected = false;
  for (const [k, at] of Object.entries(parsed as Record<string, unknown>)) {
    if (typeof at !== "number" || at <= cutoff) continue;
    /*
     * A time in the future is a clock that moved, not a denial entitled to
     * outlive the ceiling — and it has to be *written back*, not merely clamped
     * on the way out. Clamping the returned value alone leaves the future
     * timestamp in the store, where it stays ahead of every cutoff for ever:
     * permanence reintroduced by accident, which is the bug this file is here
     * to remove.
     */
    if (at > now) corrected = true;
    out[k] = Math.min(at, now);
  }
  if (corrected) {
    try {
      localStorage.setItem(DENIED_KEY, JSON.stringify(out));
    } catch {
      // Read-only storage still gets the clamped view for this session.
    }
  }
  return out;
}

function denied(): Set<string> {
  return new Set(Object.keys(denials()));
}

/**
 * deny records that a capability did not work here, so it stops being claimed.
 * Returns true when this is news — the caller retries only on news, which is
 * what stops a retry loop.
 */
export function deny(capability: string): boolean {
  const current = denials();
  if (capability in current) return false;
  current[capability] = Date.now();
  try {
    localStorage.setItem(DENIED_KEY, JSON.stringify(current));
  } catch {
    // A browser with no storage still works; it just re-learns each session.
  }
  return true;
}

/**
 * deniedCapabilities lists what is currently withheld, newest first.
 *
 * For the settings panel. The point of showing it is that this state was
 * invisible, and invisible is how it survived being wrong for months.
 */
export function deniedCapabilities(): { name: string; at: number }[] {
  return Object.entries(denials())
    .map(([name, at]) => ({ name, at }))
    .sort((a, b) => b.at - a.at);
}

/**
 * clearDenials forgets every denial, so each capability is measured again.
 *
 * The manual half of the same repair the expiry does on a timer: someone who
 * has just installed a codec extension should not have to wait a fortnight to
 * find out it worked.
 */
export function clearDenials() {
  try {
    localStorage.removeItem(DENIED_KEY);
  } catch {
    // Nothing stored, nothing to forget.
  }
  resetCapabilities();
}

/*
 * Capabilities a person has withheld by hand.
 *
 * Separate from `DENIED_KEY`, and permanent, because it answers a different
 * question. An automatic denial records "this failed here", which is a fact
 * about a machine on a day — a driver updates, a codec extension is installed,
 * and it goes stale, which is why those expire after a fortnight.
 *
 * This records "I watched it and it played badly", and that does not go stale
 * on a timer. Expiring it would put the judder back every fortnight and give
 * the person no way to make it stop.
 *
 * It exists because the automatic net cannot catch the worst version of this
 * bug. A denial is written when a direct play *fails*; a file that decodes in
 * hardware and stutters has not failed, so nothing fires. Measured on a real
 * film: HEVC Main 10 direct-played with hardware decode engaged at 8-9%, the
 * server idle, the bytes arriving, and a picture that juddered in both WebView2
 * and Chrome — while the same file re-encoded to H.264 played perfectly. The
 * engine answers `probably` for `hvc1.2.4.L120.B0` and means "I can decode
 * this", not "I can decode this well enough to watch".
 *
 * Nothing can measure the difference from in here. A person can see it.
 */
const WITHHELD_KEY = "lancast:codec-withheld";

function withheldSet(): Set<string> {
  try {
    const raw = localStorage.getItem(WITHHELD_KEY);
    if (!raw) return new Set();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((x): x is string => typeof x === "string"));
  } catch {
    // No storage, or nonsense in it: withhold nothing rather than fail to play.
    return new Set();
  }
}

function saveWithheld(names: Set<string>) {
  try {
    localStorage.setItem(WITHHELD_KEY, JSON.stringify([...names]));
  } catch {
    // A browser with no storage cannot remember this. It still plays.
  }
  resetCapabilities();
}

/** withhold stops a capability being claimed, until somebody restores it. */
export function withhold(capability: string) {
  const set = withheldSet();
  set.add(capability);
  saveWithheld(set);
}

/** restore starts claiming a capability again. */
export function restore(capability: string) {
  const set = withheldSet();
  set.delete(capability);
  saveWithheld(set);
}

/** withheldCapabilities lists what a person has turned off, for the settings panel. */
export function withheldCapabilities(): string[] {
  return [...withheldSet()].sort();
}

/**
 * claimable lists every capability this browser answers `probably` for,
 * ignoring both kinds of withholding.
 *
 * The settings panel needs it: to offer "stop claiming hevc10" it has to know
 * the engine claims it at all, and `capabilities()` has by then removed it.
 * Asking the engine again here is cheap and cannot disagree with itself — it is
 * the same question `capabilities()` asks, without the subtraction.
 */
export function claimable(): string[] {
  const el = document.createElement("video");
  return Object.entries(PROBES)
    .filter(([, type]) => el.canPlayType(type) === "probably")
    .map(([name]) => name);
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
  const off = withheldSet();
  const can: string[] = [];

  for (const [name, type] of Object.entries(PROBES)) {
    // Withheld by a person outranks everything: they have watched it and the
    // engine has not.
    if (no.has(name) || off.has(name)) continue;
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
  return (
    url + (url.includes("?") ? "&" : "?") + "can=" + encodeURIComponent(can)
  );
}
