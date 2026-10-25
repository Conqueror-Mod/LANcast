import { useMemo, useSyncExternalStore } from "react";
import { useMilkdropPresets } from "@/api/hooks";
import type { MilkdropPresetSummary } from "@/api/types";
import { webgl2Supported } from "./trails";
import { getPrefs, resolveMilkdrop, setPrefs, type Stage } from "./prefs";
import type { ShaderStage } from "./milkdropShaders";

/*
 * The MilkDrop stage's state outside the canvas (ADR 0091, step d): what is
 * offered, what is held back under reduced motion, and what to do with a
 * preset that fails.
 */

export interface MilkdropOffer {
  /** Whether the stage switcher offers MilkDrop at all. */
  offered: boolean;
  /** The stored presets, by name; empty where MilkDrop is not offered. */
  presets: MilkdropPresetSummary[];
  /** Their ids, in the same order, without the ones that failed here. */
  ids: string[];
}

/*
 * useMilkdropOffer: MilkDrop is offered only with WebGL2 and at least one
 * preset stored. The list is asked for only where it could be offered, so a
 * screen without WebGL2 never asks.
 *
 * Its query is the list an upload or a delete invalidates (hooks.ts), so the
 * first upload brings the choice in and the last delete takes it away.
 */
export function useMilkdropOffer(enabled = true): MilkdropOffer {
  const gl = webgl2Supported();
  const { data } = useMilkdropPresets(enabled && gl);
  const broken = useBroken();
  const on = gl && enabled;
  // Memoised, so the player's key handler, which depends on the ids, is not
  // re-subscribed on every render.
  return useMemo(() => {
    const presets = on ? (data ?? []).filter((p) => !broken.has(p.id)) : [];
    return { offered: presets.length > 0, presets, ids: presets.map((p) => p.id) };
  }, [on, data, broken]);
}

// ---- reduced motion -----------------------------------------------------------

/*
 * Under prefers-reduced-motion a MilkDrop preset does not start on its own
 * (ADR 0091, Decision 7). A stage remembered from an earlier session is "on
 * its own": the cover shows until somebody picks MilkDrop, or steps to a
 * preset, in this one. Once picked, it runs, in calm.
 */
let picked = false;
const pickedSubs = new Set<() => void>();

export function markMilkdropPicked() {
  if (picked) return;
  picked = true;
  for (const fn of pickedSubs) fn();
}

/** For tests: back to a session in which nothing has been picked. */
export function resetMilkdropPickedForTests() {
  picked = false;
  broken = new Set();
  fellBack = new Map();
  status = "idle";
  for (const fn of pickedSubs) fn();
  for (const fn of brokenSubs) fn();
  for (const fn of fellBackSubs) fn();
  for (const fn of statusSubs) fn();
}

export function useMilkdropPicked(): boolean {
  return useSyncExternalStore(
    (fn) => {
      pickedSubs.add(fn);
      return () => pickedSubs.delete(fn);
    },
    () => picked,
  );
}

/** Whether a MilkDrop stage is held on the cover rather than started. */
export function milkdropHeld(stage: Stage, reducedMotion: boolean, pickedHere: boolean): boolean {
  return stage === "milkdrop" && reducedMotion && !pickedHere;
}

// ---- a preset that fails ---------------------------------------------------------

/*
 * A preset that will not load, or traps while it runs (eel-wasm 0.0.16 throws
 * a WebAssembly.RuntimeError when `%` meets NaN or an out-of-range value), is
 * dropped for the rest of this session and the stage moves on to the next
 * stored preset. It is the preset that is dropped, never the page or the
 * visualizer. Per session rather than stored: the server may have a fixed
 * copy by tomorrow, and a trap can depend on the music.
 */
let broken = new Set<string>();
const brokenSubs = new Set<() => void>();

function useBroken(): Set<string> {
  return useSyncExternalStore(
    (fn) => {
      brokenSubs.add(fn);
      return () => brokenSubs.delete(fn);
    },
    () => broken,
  );
}

/*
 * dropPreset: mark `id` broken and point the stage at what should play
 * instead: the next preset in `ids` after it that has not failed, or the cover
 * when none is left.
 */
export function dropPreset(id: string, ids: string[]) {
  if (!broken.has(id)) {
    broken = new Set(broken).add(id);
    for (const fn of brokenSubs) fn();
  }
  const p = getPrefs();
  if (p.stage !== "milkdrop" || resolveMilkdrop(p.milkdrop, ids) !== id) return;
  setPrefs(nextAfterBroken(id, ids, broken));
}

// ---- shaders that do not run here ------------------------------------------------

/*
 * A preset whose own warp or comp shader would not compile in this browser is
 * not dropped: it plays with butterchurn's default for that stage, and the
 * row under the stage says so (ADR 0091, step g4). Remembered for the session
 * so the next visit loads the defaults at once rather than compiling the
 * broken shader again, and per session for the same reasons as `broken`: a
 * driver update, or a re-upload, may fix it.
 */
let fellBack = new Map<string, readonly ShaderStage[]>();
const fellBackSubs = new Set<() => void>();

export function setShaderFallback(id: string, stages: readonly ShaderStage[]) {
  fellBack = new Map(fellBack).set(id, stages);
  for (const fn of fellBackSubs) fn();
}

/** The stages of `id` that play with the defaults here, if any. */
export function shaderFallback(id: string): readonly ShaderStage[] | undefined {
  return fellBack.get(id);
}

export function useShaderFallback(id: string | undefined): readonly ShaderStage[] | undefined {
  const map = useSyncExternalStore(
    (fn) => {
      fellBackSubs.add(fn);
      return () => fellBackSubs.delete(fn);
    },
    () => fellBack,
  );
  return id ? map.get(id) : undefined;
}

// ---- loading ---------------------------------------------------------------------

/*
 * What the MilkDrop stage is doing, for the words on it and under it. The
 * engine is a 233 KB chunk, and over a phone's cellular link it took long
 * enough that a stage with nothing on it read as broken (v0.9.92):
 *
 *   loading  the engine is downloading, or a preset is being fetched and
 *            compiled with nothing yet drawn, or a switch is taking long
 *   ready    a preset is drawing
 *   failed   the engine would not load; the stage says so and offers the
 *            cover back, rather than sitting blank
 *
 * One canvas sets it at a time (there is only ever one MilkDrop stage), and
 * it goes back to idle when that canvas goes.
 */
export type MilkdropStatus = "idle" | "loading" | "ready" | "failed";

let status: MilkdropStatus = "idle";
const statusSubs = new Set<() => void>();

export function setMilkdropStatus(next: MilkdropStatus) {
  if (status === next) return;
  status = next;
  for (const fn of statusSubs) fn();
}

export function useMilkdropStatus(): MilkdropStatus {
  return useSyncExternalStore(
    (fn) => {
      statusSubs.add(fn);
      return () => statusSubs.delete(fn);
    },
    () => status,
  );
}

/** The words the stage shows for a status, or "" for none. */
export function milkdropStatusText(s: MilkdropStatus): string {
  if (s === "loading") return "Loading MilkDrop…";
  if (s === "failed") return "MilkDrop could not load";
  return "";
}

/** The prefs that move off a broken preset; pure, for the tests. */
export function nextAfterBroken(id: string, ids: string[], bad: Set<string>): { stage: Stage; milkdrop?: string } {
  const at = ids.indexOf(id);
  for (let i = 1; i <= ids.length; i++) {
    const next = ids[(at + i + ids.length) % ids.length];
    if (!bad.has(next) && next !== id) return { stage: "milkdrop", milkdrop: next };
  }
  return { stage: "cover" };
}
