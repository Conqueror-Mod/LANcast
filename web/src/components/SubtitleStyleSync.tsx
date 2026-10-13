import { useEffect, useRef } from "react";
import { useSetSubtitleStyle, useSubtitleStyle, type SubtitleStyle } from "@/api/hooks";
import { getPrefs, setPrefs, subscribePrefs, type Prefs, type SubFont } from "@/playback/prefs";

/*
 * Subtitle style follows the account (docs/settings-plan.md, decided
 * 2026-10-12).
 *
 * The players draw from this device's preferences, which stay the working
 * copy: they are there at first paint and with no connection. This keeps that
 * copy and the account's in step, in both directions:
 *
 *   - the account's style arrives, and this device takes it on;
 *   - the style changes here, from the player's panel or from Settings, and
 *     the account takes it on, so the next device signed in draws the same.
 *
 * An account that has never chosen a style has none, and nothing is pushed
 * until somebody changes one: signing in on a new device must not overwrite
 * the account with that device's untouched defaults.
 *
 * The offset is not part of it — that is timing for one file on one device.
 */
function styleOf(p: Prefs): SubtitleStyle {
  return { color: p.subColor, font: p.subFont, size: p.subSize, position: p.subPosition };
}

const key = (s: SubtitleStyle) => JSON.stringify([s.color, s.font, s.size, s.position]);

export function SubtitleStyleSync() {
  const { data: style, isSuccess } = useSubtitleStyle();
  const save = useSetSubtitleStyle();
  const saveRef = useRef(save.mutate);
  saveRef.current = save.mutate;
  // What this device and the account last agreed on; a change that leaves the
  // style here is not a change worth sending.
  const agreed = useRef<string | null>(null);
  const loaded = useRef(false);

  useEffect(() => {
    if (!isSuccess) return;
    loaded.current = true;
    if (!style) {
      agreed.current = key(styleOf(getPrefs()));
      return;
    }
    agreed.current = key(style);
    const here = styleOf(getPrefs());
    if (key(here) !== agreed.current) {
      setPrefs({
        subColor: style.color,
        subFont: style.font as SubFont,
        subSize: style.size,
        subPosition: style.position,
      });
    }
  }, [isSuccess, style]);

  useEffect(() => {
    let timer: number | undefined;
    const off = subscribePrefs((p) => {
      if (!loaded.current) return;
      const next = styleOf(p);
      if (key(next) === agreed.current) return;
      // A slider sends a value per pixel; the account wants the one it stops at.
      window.clearTimeout(timer);
      timer = window.setTimeout(() => {
        agreed.current = key(next);
        saveRef.current(next);
      }, 600);
    });
    return () => {
      off();
      window.clearTimeout(timer);
    };
  }, []);

  return null;
}
