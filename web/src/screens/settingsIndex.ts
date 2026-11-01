/*
 * The settings panes, and the index that makes single settings findable.
 *
 * Settings grew past forty panes' worth of switches, and the question somebody
 * brings to the page is usually "where was the one that…", not "which category
 * is it in". The search box at the top of the nav answers it from this list.
 *
 * Written by hand rather than read off the rendered page, because most panes
 * render nothing until the server has answered, and only one pane is mounted
 * at a time: a search that read the DOM would find only what is on screen.
 * settingsIndex.test.ts checks every label here still appears in the client's
 * source, so renaming a setting without renaming its entry fails the build
 * instead of leaving a search result that leads nowhere.
 *
 * A label is the setting's visible text, as it reads on screen, because that is
 * also what the page is searched for when the result is chosen: the pane opens,
 * and the row carrying that text is scrolled to and marked. `terms` are the
 * other words somebody might type for it.
 */

export interface Pane {
  id: string;
  label: string;
  admin?: boolean;
}

/*
 * Four groups (docs/settings-plan.md, decided 2026-10-12): Status is what the
 * server is doing, Server is how it is configured, Your account follows the
 * person to any device, This device stays on this screen. Panes from the plan
 * that have nothing in them yet are not listed until they do — a heading that
 * leads to an empty column is the fault DESKTOP_ONLY exists to avoid.
 */
export const STATUS_PANES: Pane[] = [
  // First, for an administrator: what is happening, before how it is set up.
  { id: "dashboard", label: "Dashboard", admin: true },
  { id: "tasks", label: "Scheduled tasks", admin: true },
  // Optimized copies converting and done (ADR 0087).
  { id: "conversions", label: "Conversions", admin: true },
  { id: "troubleshooting", label: "Troubleshooting", admin: true },
];

export const SERVER_PANES: Pane[] = [
  { id: "general", label: "General", admin: true },
  // This machine and the LAN. Remote access is the outside world.
  { id: "network", label: "Network", admin: true },
  { id: "remote", label: "Remote access", admin: true },
  { id: "users", label: "Users & access", admin: true },
  { id: "libraries", label: "Libraries", admin: true },
  { id: "metadata", label: "Metadata", admin: true },
  { id: "transcoding", label: "Transcoding", admin: true },
  { id: "quality", label: "Quality", admin: true },
  { id: "progress", label: "Playback & progress", admin: true },
  { id: "livetv", label: "Live TV", admin: true },
  /*
   * Pictures has its own pane rather than a corner of Libraries: sensitive
   * marking, face grouping and places each have a download, a state or a
   * switch of their own.
   */
  { id: "pictures", label: "Pictures", admin: true },
  // Retro games (ADR 0073), for the reason Pictures has one: an optional
  // download and a network switch that would be lost in a general pane.
  { id: "retro", label: "Retro games", admin: true },
  /*
   * Other servers beside Users & access because both answer who may do what
   * here. Its own pane because a peer is not an account and must never come
   * to look like one — pairing grants nothing, where adding a user grants the
   * whole library.
   */
  { id: "peers", label: "Other servers", admin: true },
  // TVs and consoles on the network, with no LANcast app (ADR 0086).
  { id: "dlna", label: "DLNA", admin: true },
  // Outbound messages to addresses an administrator chose (ADR 0097).
  { id: "webhooks", label: "Webhooks", admin: true },
  /*
   * MilkDrop presets (ADR 0091): uploads every signed-in browser will run,
   * so an administrator's pane, and its own, because what is uploaded here
   * is drawn on everyone's screen and an upload's refusal needs room to say
   * why.
   */
  { id: "visualizers", label: "Visualizers", admin: true },
  { id: "addons", label: "Add-ons", admin: true },
  /*
   * Backup apart from Troubleshooting's maintenance, which throws away what
   * the server can make again; this protects what it never can.
   */
  { id: "backup", label: "Backup & storage", admin: true },
];

export const ACCOUNT_PANES: Pane[] = [
  { id: "profile", label: "Profile" },
  { id: "security", label: "Security" },
  { id: "languages", label: "Languages & subtitles" },
  { id: "privacy", label: "Privacy" },
];

export const DEVICE_PANES: Pane[] = [
  { id: "player", label: "Player" },
  { id: "display", label: "Display & home" },
  { id: "keyboard", label: "Keyboard" },
  { id: "app", label: "This app" },
  /*
   * Games has a pane of its own rather than a row inside "This app".
   *
   * It was a row there, and the first person to look for it did not find it:
   * nothing on that path says the word "games", so a feature that had shipped
   * and worked was reported as having no interface at all. Off by default is
   * the right decision (ADR 0066) and undiscoverable is not the same thing.
   */
  { id: "games", label: "PC Games" },
  // Last, where every app keeps it, and for everyone: nothing in it is an
  // administrator's, and the licence is anybody's business.
  { id: "about", label: "About" },
];

export const PANE_GROUPS: { label: string; panes: Pane[] }[] = [
  { label: "Status", panes: STATUS_PANES },
  { label: "Server", panes: SERVER_PANES },
  { label: "Your account", panes: ACCOUNT_PANES },
  { label: "This device", panes: DEVICE_PANES },
];

export interface SettingEntry {
  pane: string;
  label: string;
  terms?: string;
}

export const SETTINGS_INDEX: SettingEntry[] = [
  // Status
  { pane: "dashboard", label: "Server activity", terms: "history graph cpu bandwidth streams" },
  { pane: "dashboard", label: "Audit log", terms: "who changed what record" },
  { pane: "tasks", label: "Scheduled tasks", terms: "jobs timer run now" },
  { pane: "conversions", label: "Optimized versions", terms: "convert copies transcode ahead" },
  { pane: "troubleshooting", label: "Server log", terms: "logs lancastd.log errors" },
  { pane: "troubleshooting", label: "Crash reports", terms: "crash dump" },
  { pane: "troubleshooting", label: "Metrics for Prometheus", terms: "metrics grafana token" },
  { pane: "troubleshooting", label: "Write debug detail to the log", terms: "debug logging verbose" },
  { pane: "troubleshooting", label: "Clear cached artwork", terms: "posters cache images" },
  { pane: "troubleshooting", label: "Clear transcode scratch", terms: "temp segments cache" },
  { pane: "troubleshooting", label: "Reset settings", terms: "defaults factory" },

  // Server
  { pane: "general", label: "Identity fingerprint", terms: "key server id" },
  { pane: "general", label: "TLS certificate fingerprint", terms: "cert https pin" },
  { pane: "general", label: "Updates", terms: "upgrade install new version" },
  { pane: "general", label: "Check for updates automatically", terms: "auto update" },
  { pane: "general", label: "Check for new LANcast releases", terms: "github update check" },
  { pane: "network", label: "Port", terms: "listen address 8443" },
  { pane: "network", label: "Every address", terms: "ip lan bind interfaces" },
  { pane: "network", label: "Trusted proxies", terms: "reverse proxy nginx caddy x-forwarded-for" },
  { pane: "network", label: "Your own certificate", terms: "custom cert tls pem key file" },
  { pane: "remote", label: "Public certificate", terms: "lets encrypt acme domain https outside" },
  { pane: "users", label: "Add user", terms: "account create member admin" },
  { pane: "users", label: "Show accounts on the sign-in page", terms: "login picker names" },
  { pane: "users", label: "Signed-in devices", terms: "sessions sign out revoke" },
  { pane: "users", label: "Reset password", terms: "forgot password user" },
  { pane: "users", label: "Content rating limit", terms: "parental controls kids age ceiling" },
  { pane: "libraries", label: "Scan every library", terms: "rescan refresh update libraries" },
  { pane: "libraries", label: "Rescan libraries automatically", terms: "scan interval timer schedule" },
  { pane: "libraries", label: "Start scans at", terms: "scan hour time" },
  { pane: "libraries", label: "Allow deleting media files from disk", terms: "delete remove files" },
  { pane: "libraries", label: "Forget missing files after a scan", terms: "empty trash missing" },
  { pane: "metadata", label: "OpenSubtitles account", terms: "subtitles login provider" },
  { pane: "metadata", label: "Use LANcast's built-in keys", terms: "tmdb omdb api key provider" },
  { pane: "metadata", label: "Age rating country", terms: "certification pg-13 fsk country" },
  { pane: "metadata", label: "Automatically fetch metadata after a scan", terms: "auto enrich match" },
  { pane: "metadata", label: "Write NFO sidecar files next to media", terms: "nfo kodi sidecar" },
  { pane: "metadata", label: "Provider requests", terms: "rate limit tmdb" },
  { pane: "transcoding", label: "Video encoder", terms: "hardware nvenc qsv amf gpu encode" },
  { pane: "transcoding", label: "Conversions at once", terms: "max transcodes concurrent" },
  { pane: "transcoding", label: "Media tools", terms: "ffmpeg ffprobe install" },
  { pane: "transcoding", label: "Re-read media files", terms: "reprobe probe codecs" },
  { pane: "quality", label: "Maximum streaming quality", terms: "bitrate cap limit 1080p 720p" },
  { pane: "progress", label: "Counts as watched at", terms: "watched threshold percent" },
  { pane: "progress", label: "Weeks to keep in Continue Watching", terms: "continue watching shelf" },
  { pane: "progress", label: "Items in Continue Watching", terms: "continue watching shelf limit" },
  { pane: "progress", label: "Detect where the credits start", terms: "markers intro skip credits" },
  { pane: "progress", label: "Show a picture over the scrub bar", terms: "scrub previews thumbnails seek trickplay" },
  { pane: "progress", label: "Pre-roll", terms: "trailer clip before film intro video" },
  { pane: "livetv", label: "Live TV", terms: "iptv m3u channels guide epg xmltv tuner" },
  { pane: "pictures", label: "Allow folders to be marked sensitive", terms: "private hidden photos" },
  { pane: "pictures", label: "Read where photos were taken", terms: "gps location places map" },
  { pane: "pictures", label: "Group faces", terms: "people face recognition" },
  { pane: "pictures", label: "Search photographs by description", terms: "semantic clip ai search" },
  { pane: "retro", label: "Name games from the ROM database", terms: "dat no-intro identify" },
  { pane: "retro", label: "Play in a browser", terms: "web cores emulator" },
  { pane: "retro", label: "Fetch box art for identified games", terms: "artwork covers screenshots" },
  { pane: "peers", label: "Your invite", terms: "pair federation friend server" },
  { pane: "peers", label: "Add a server", terms: "pair federation invite" },
  { pane: "dlna", label: "Let TVs and consoles on your network find this server", terms: "dlna upnp ssdp tv xbox playstation" },
  { pane: "dlna", label: "Name on TVs", terms: "dlna friendly name" },
  { pane: "webhooks", label: "Add a webhook", terms: "home assistant discord notify" },
  { pane: "visualizers", label: "MilkDrop presets", terms: "visualizer butterchurn upload" },
  { pane: "addons", label: "Add-ons", terms: "plugins wasm extensions" },
  { pane: "backup", label: "Take a backup", terms: "backup database export" },
  { pane: "backup", label: "Back up automatically", terms: "backup schedule" },
  { pane: "backup", label: "Scheduled backups to keep", terms: "backup retention" },
  { pane: "backup", label: "Artwork cache limit", terms: "storage disk space posters" },

  // Your account
  { pane: "profile", label: "Display name", terms: "rename name" },
  { pane: "security", label: "Passkeys", terms: "webauthn fingerprint face id sign in" },
  { pane: "security", label: "Two-factor sign-in", terms: "2fa totp authenticator mfa" },
  { pane: "security", label: "API keys", terms: "token bearer script" },
  { pane: "security", label: "Change password", terms: "password" },
  { pane: "security", label: "Authorized devices", terms: "sessions sign out" },
  { pane: "languages", label: "Preferred audio", terms: "language dub" },
  { pane: "languages", label: "Subtitle language", terms: "subs captions" },
  { pane: "languages", label: "Subtitle style", terms: "font size colour color captions" },
  { pane: "privacy", label: "Let others here see what I have watched", terms: "sharing activity" },
  { pane: "privacy", label: "Let paired servers list me", terms: "presence peers federation" },
  { pane: "privacy", label: "Let this server's webhooks report what I watch", terms: "webhook consent" },

  // This device
  { pane: "player", label: "Quality on this device", terms: "bitrate resolution" },
  { pane: "player", label: "Dialogue boost", terms: "speech voice audio" },
  { pane: "player", label: "Night mode", terms: "compressor quiet loud audio" },
  { pane: "player", label: "Play the next episode or track automatically", terms: "autoplay up next" },
  { pane: "player", label: "Skip intros automatically", terms: "skip intro" },
  { pane: "player", label: "Play the pre-roll before films", terms: "trailer pre-roll" },
  { pane: "player", label: "Equaliser", terms: "eq equalizer bass treble" },
  { pane: "player", label: "Crossfade between albums", terms: "gapless fade music" },
  { pane: "player", label: "Even out loudness", terms: "replaygain volume normalise normalize" },
  { pane: "player", label: "Now Playing shows", terms: "visualizer cover art music" },
  { pane: "player", label: "Calm visualizers: half the motion", terms: "reduce motion milkdrop" },
  { pane: "player", label: "Theater output", terms: "tv display hdr monitor" },
  { pane: "player", label: "Match the display to the film's frame rate", terms: "refresh rate 23.976 hz judder" },
  { pane: "display", label: "Bigscreen mode", terms: "tv ten foot scale" },
  { pane: "display", label: "Improved live TV playback", terms: "hls segmented" },
  { pane: "display", label: "Spoilers on a season page", terms: "hide synopsis still" },
  { pane: "display", label: "Spotlight", terms: "hero homepage banner pin" },
  { pane: "keyboard", label: "Keyboard", terms: "shortcuts hotkeys keys bindings" },
  { pane: "app", label: "Start LANcast at login", terms: "startup boot tray" },
  { pane: "app", label: "Developer tools", terms: "devtools inspector" },
  { pane: "app", label: "Run as a Windows service", terms: "service background session" },
  { pane: "app", label: "App log", terms: "client log lancast-client.log" },
  { pane: "games", label: "Show my installed games", terms: "steam epic gog xbox battle.net" },
  { pane: "about", label: "This installation", terms: "version licence license" },
  { pane: "about", label: "Data sources", terms: "tmdb credits attribution" },
];

// Lower case, curly quotes made straight, runs of space made one: the same
// text whether it came from the index, the keyboard or the page.
export function normalize(s: string): string {
  return s
    .toLowerCase()
    .replace(/[‘’]/g, "'")
    .replace(/\s+/g, " ")
    .trim();
}

export interface SettingMatch extends SettingEntry {
  paneLabel: string;
  group: string;
}

/*
 * The settings this person can reach whose words cover the query. Every word
 * typed must start a word somewhere in the label, its terms, its pane or its
 * group, so "sub sty" finds Subtitle style and "night" finds both night
 * modes. Panes themselves are results too, after the settings in them.
 */
export function searchSettings(query: string, visible: Set<string>, limit = 12): SettingMatch[] {
  const words = normalize(query).split(" ").filter(Boolean);
  if (words.length === 0) return [];
  const where = new Map<string, { paneLabel: string; group: string }>();
  for (const g of PANE_GROUPS) {
    for (const p of g.panes) where.set(p.id, { paneLabel: p.label, group: g.label });
  }
  const panesAsEntries: SettingEntry[] = PANE_GROUPS.flatMap((g) =>
    g.panes.map((p) => ({ pane: p.id, label: p.label })),
  );

  const scored: { m: SettingMatch; score: number }[] = [];
  const seen = new Set<string>();
  for (const [i, e] of [...SETTINGS_INDEX, ...panesAsEntries].entries()) {
    const w = where.get(e.pane);
    if (!w || !visible.has(e.pane)) continue;
    const key = e.pane + "\u0000" + normalize(e.label);
    if (seen.has(key)) continue;
    const label = normalize(e.label);
    const hay = ` ${label} ${normalize(e.terms ?? "")} ${normalize(w.paneLabel)} ${normalize(w.group)}`
      .replace(/[^a-z0-9.' ]/g, " ");
    if (!words.every((word) => hay.includes(" " + word.replace(/[^a-z0-9.' ]/g, " ").trim()))) continue;
    seen.add(key);
    const q = words.join(" ");
    const score = (label.startsWith(q) ? 0 : label.includes(q) ? 1 : 2) * 1000 + i;
    scored.push({ m: { ...e, ...w }, score });
  }
  return scored
    .sort((a, b) => a.score - b.score)
    .slice(0, limit)
    .map((s) => s.m);
}
