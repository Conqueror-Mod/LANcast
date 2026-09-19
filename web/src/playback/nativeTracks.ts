/*
 * Tracks for native playback (ADR 0067, Phase 3).
 *
 * Two translations the browser path never needed:
 *
 *  - **Audio.** The server names a track by its absolute stream index in the
 *    file (docs/api.md, `?audio=`). mpv numbers audio tracks from 1 in file
 *    order, counting only audio. The same track, two numbers.
 *  - **Subtitles.** The browser renders a <track> itself. mpv cannot fetch the
 *    server's subtitle endpoint — it holds a stream ticket for the file and
 *    nothing else (ADR 0068) — so the page parses the same WebVTT the element
 *    would have loaded and draws the cues over the picture. One subtitle
 *    source for both backends, so a chosen track, the offset control and the
 *    styling behave identically either way.
 */

interface StreamLike {
  index: number;
  kind: string;
}

/**
 * mpvAudioTrack is mpv's `aid` for the audio stream at `absoluteIndex`, or null
 * when the file has no such audio stream (leave mpv's own choice alone).
 */
export function mpvAudioTrack(
  streams: readonly StreamLike[] | undefined,
  absoluteIndex: number | null | undefined,
): number | null {
  if (absoluteIndex == null || !streams) return null;
  const audio = streams
    .filter((s) => s.kind === "audio")
    .map((s) => s.index)
    .sort((a, b) => a - b);
  const i = audio.indexOf(absoluteIndex);
  return i < 0 ? null : i + 1;
}

export interface Cue {
  start: number;
  end: number;
  text: string;
}

function seconds(ts: string): number | null {
  // hh:mm:ss.ttt or mm:ss.ttt
  const m = /^(?:(\d+):)?(\d{2}):(\d{2})[.,](\d{3})$/.exec(ts.trim());
  if (!m) return null;
  return (Number(m[1] ?? 0) * 3600 + Number(m[2]) * 60 + Number(m[3])) + Number(m[4]) / 1000;
}

/**
 * parseVTT reads the cues out of a WebVTT file: timings and text, with markup
 * tags removed and entities decoded. Positioning settings are ignored — the
 * element's rendering ignores most of them too, and the page places cues with
 * the same `--cue-bottom` preference either way.
 */
export function parseVTT(src: string): Cue[] {
  const cues: Cue[] = [];
  const blocks = src.replace(/\r\n?/g, "\n").split(/\n{2,}/);
  for (const block of blocks) {
    const lines = block.split("\n");
    const at = lines.findIndex((l) => l.includes("-->"));
    if (at < 0) continue;
    const [a, rest] = lines[at].split("-->");
    const start = seconds(a);
    const end = seconds((rest ?? "").trim().split(/\s+/)[0] ?? "");
    if (start === null || end === null || end <= start) continue;
    const text = lines
      .slice(at + 1)
      .join("\n")
      .replace(/<[^>]*>/g, "")
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&nbsp;/g, " ")
      .replace(/&amp;/g, "&")
      .trim();
    if (text) cues.push({ start, end, text });
  }
  return cues;
}

/**
 * activeCues is what shows at `time`, with the viewer's subtitle offset
 * applied the way the element path applies it: a positive offset shows each
 * cue later.
 */
export function activeCues(cues: readonly Cue[], time: number, offset: number): Cue[] {
  const t = time - offset;
  return cues.filter((c) => c.start <= t && t < c.end);
}
