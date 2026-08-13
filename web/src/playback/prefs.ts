import { useCallback, useEffect, useState } from "react";

/*
 * Playback preferences, per device.
 *
 * Deliberately not per user on the server, which ADR 0006 does for *playback
 * state* — where you are in a film is a fact about you and should follow you to
 * the next screen you sit at. These are facts about the screen you are sitting
 * at: what its speakers are, how big the subtitles need to be at this viewing
 * distance, and how much bandwidth there is between here and the server. Roamed
 * to another device they would all be wrong, and the audio device could not
 * even be named there.
 *
 * localStorage is where `lancast:volume` and the codec-denial list already
 * live, for the same reason. One mechanism.
 */

/** A quality ceiling. `height` and `bitrate` of 0 mean "no limit". */
export interface Quality {
  id: string;
  label: string;
  height: number;
  /** Bits per second, matching the server's Profile. */
  bitrate: number;
}

/*
 * The ladder.
 *
 * Original is first and is the default, because this is a *LAN* server: the
 * ordinary case is a gigabit link to the next room, where every rung below
 * Original is a re-encode that makes the picture worse and the server hotter to
 * solve a problem nobody has. The rungs exist for the cases that do — a remote
 * connection over a domestic uplink, or a laptop on hotel wifi.
 *
 * Each rung pairs a resolution with a bitrate rather than offering them
 * separately. Two independent controls make it easy to ask for 1080p at 1 Mbps,
 * which is a worse picture than 480p at 1 Mbps and looks like the server is
 * broken.
 */
export const QUALITIES: Quality[] = [
  { id: "original", label: "Original", height: 0, bitrate: 0 },
  { id: "1080p20", label: "1080p · 20 Mbps", height: 1080, bitrate: 20_000_000 },
  { id: "1080p10", label: "1080p · 10 Mbps", height: 1080, bitrate: 10_000_000 },
  { id: "720p4", label: "720p · 4 Mbps", height: 720, bitrate: 4_000_000 },
  { id: "720p2", label: "720p · 2 Mbps", height: 720, bitrate: 2_000_000 },
  { id: "480p1", label: "480p · 1.5 Mbps", height: 480, bitrate: 1_500_000 },
  { id: "360p07", label: "360p · 0.7 Mbps", height: 360, bitrate: 700_000 },
];

export function qualityByID(id: string): Quality {
  return QUALITIES.find((q) => q.id === id) ?? QUALITIES[0];
}

/** Where the subtitles sit, as a percentage of the picture height from the bottom. */
export type SubPosition = number;

export interface Prefs {
  /** Quality id; see QUALITIES. */
  quality: string;
  /** An `enumerateDevices` deviceId, or "" for the system default. */
  audioDevice: string;
  /** Auto play the next thing in the queue when this one ends. */
  autoPlay: boolean;

  subColor: string;
  /** Multiplier on the base cue size; 1 is the default. */
  subSize: number;
  /** Percent of the picture height, from the bottom. */
  subPosition: SubPosition;
  /** Seconds to shift the cues by. Positive shows them later. */
  subOffset: number;
}

export const DEFAULTS: Prefs = {
  quality: "original",
  audioDevice: "",
  autoPlay: true,
  subColor: "#ffffff",
  subSize: 1,
  subPosition: 8,
  subOffset: 0,
};

const KEY = "lancast:playback-prefs";

function read(): Prefs {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return DEFAULTS;
    // Spread over the defaults rather than trusting the stored shape. A build
    // that adds a preference finds every older browser's stored object missing
    // it, and `undefined` reaching a <select value> or a CSS variable is a
    // control that renders blank rather than at its default.
    return { ...DEFAULTS, ...(JSON.parse(raw) as Partial<Prefs>) };
  } catch {
    return DEFAULTS;
  }
}

function write(p: Prefs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(p));
  } catch {
    // A browser with no storage still plays; it re-reads the defaults next time.
  }
}

/*
 * One store, shared by every hook instance.
 *
 * The settings panel and the provider both read these, and useState in each
 * would give them separate copies: changing the quality in the panel would
 * update the panel's tick and leave the provider still streaming at the old
 * ceiling. Small enough not to want a state library for, and this is the whole
 * of it.
 */
let current = read();
const listeners = new Set<(p: Prefs) => void>();

export function getPrefs(): Prefs {
  return current;
}

export function setPrefs(patch: Partial<Prefs>) {
  current = { ...current, ...patch };
  write(current);
  for (const fn of listeners) fn(current);
}

/** Back to the shipped defaults. */
export function resetPrefs() {
  current = { ...DEFAULTS };
  write(current);
  for (const fn of listeners) fn(current);
}

export function usePrefs(): [Prefs, (patch: Partial<Prefs>) => void] {
  const [p, setP] = useState(current);
  useEffect(() => {
    listeners.add(setP);
    // Between the first render and this effect another component may have
    // written; re-read so a late subscriber is not a stale one.
    setP(current);
    return () => {
      listeners.delete(setP);
    };
  }, []);
  const update = useCallback((patch: Partial<Prefs>) => setPrefs(patch), []);
  return [p, update];
}

/**
 * qualityQuery is the `&max_height=&max_bitrate=` for the chosen rung, and the
 * empty string for Original.
 *
 * Empty matters: a request that names no ceiling is byte-for-byte the request
 * this client made before quality selection existed, so Original cannot become
 * a subtly different decision from "no opinion".
 */
export function qualityQuery(id: string): string {
  const q = qualityByID(id);
  if (!q.height && !q.bitrate) return "";
  const parts: string[] = [];
  if (q.height) parts.push(`max_height=${q.height}`);
  if (q.bitrate) parts.push(`max_bitrate=${q.bitrate}`);
  return parts.join("&");
}
