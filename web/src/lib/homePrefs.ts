import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/api/client";
import type { HomePreferences, HomePreferencesUpdate, Library } from "@/api/types";
import { forgetDevice, hasDevice, readDevice, useDevice } from "./device";

/*
 * What you have chosen about your Home page, kept on your account (ADR 0120,
 * settings-3-plan step 3).
 *
 * The spotlight and spoilers were device settings because there was nowhere
 * else to put them: "there is no per-user preference store on the server" was
 * the reason written beside each. There is one now, and both describe what a
 * person wants to see rather than the room the screen is in, so they follow
 * the account. Somebody who watches on two machines sets them once.
 *
 * A server with no accounts answers `account: false`, and every hook below
 * falls back to the device as it always did. In the shipped client that is
 * the sign-in gate's side of the line, so it is a belt, not a path.
 */

const KEY = ["home-prefs"] as const;

// The device keys these settings lived under before. Kept here, not in
// heroMode.ts or spoilers.ts, because carrying them over is this file's job.
export const LEGACY_SPOTLIGHT_KEY = "lancast:hero-mode";
export const LEGACY_PINNED_KEY = "lancast:hero-pinned";
export const LEGACY_SPOILERS_KEY = "lancast:spoilers";

const SPOTLIGHTS = ["continue", "recent", "recommended", "pinned"];
const SPOILERS = ["show", "synopsis", "all"];

export function useHomePrefs() {
  return useQuery({
    queryKey: KEY,
    queryFn: ({ signal }) =>
      apiGet<HomePreferences>("/api/profile/home", signal).then(
        (r): HomePreferences => ({
          account: r.account === true,
          spotlight: r.spotlight ?? null,
          spotlight_item: r.spotlight_item ?? null,
          spoilers: r.spoilers ?? null,
          hidden_shelves: r.hidden_shelves ?? [],
        }),
      ),
    staleTime: 60_000,
  });
}

export function useSetHomePrefs() {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: KEY,
    mutationFn: (u: HomePreferencesUpdate) =>
      apiPost<HomePreferences>("/api/profile/home", u, "PUT"),
    /*
     * Shown at once. The spotlight and a season page both read this, and a
     * select that snaps back to its old value until the round trip lands reads
     * as a setting that did not take.
     */
    onMutate: async (u) => {
      await qc.cancelQueries({ queryKey: KEY });
      const before = qc.getQueryData<HomePreferences>(KEY);
      if (before) qc.setQueryData<HomePreferences>(KEY, { ...before, ...u });
      return { before };
    },
    onError: (_e, _u, ctx) => {
      if (ctx?.before) qc.setQueryData(KEY, ctx.before);
    },
    /*
     * The server's answer is the truth: what Home, the spotlight and the
     * settings pane are all looking at. Unless another save is still on its
     * way — "Pin to homepage" sends the title and then the mode, and the first
     * answer predates the second, so taking it would put the old mode back on
     * screen until the second lands.
     */
    onSuccess: (r) => {
      if (qc.isMutating({ mutationKey: KEY }) > 1) return;
      qc.setQueryData<HomePreferences>(KEY, { ...r, hidden_shelves: r.hidden_shelves ?? [] });
    },
  });
}

/*
 * One preference, from the account when there is one.
 *
 * Until the account answers, and for a field the account has never set, the
 * device's value stands in. That is what makes the carry-over invisible: the
 * same value is on screen before the move, during it, and after.
 */
export function useAccountPref<T>(
  field: "spotlight" | "spotlight_item" | "spoilers",
  deviceKey: string,
  fallback: T,
): [T, (v: T) => void] {
  const { data } = useHomePrefs();
  const [device, setDevice] = useDevice<T>(deviceKey, fallback);
  const set = useSetHomePrefs();
  if (!data?.account) return [device, setDevice];
  const stored = data[field] as T | null;
  return [stored ?? device, (v: T) => set.mutate({ [field]: v } as HomePreferencesUpdate)];
}

/*
 * Moving what this device chose onto the account, once.
 *
 * A device value goes up only where the account has never chosen: the first
 * machine to arrive sets it, and a second machine holding a different older
 * choice does not overwrite the first. Either way the device copy is then
 * forgotten, so there is one answer to the question rather than two.
 *
 * Mounted once, by the signed-in shell. A failed save leaves the device copy
 * where it was, to try again next time the app opens.
 */
export function carryOverUpdate(prefs: HomePreferences): {
  update: HomePreferencesUpdate;
  forget: string[];
} {
  const update: HomePreferencesUpdate = {};
  const forget: string[] = [];
  if (hasDevice(LEGACY_SPOTLIGHT_KEY)) {
    const v = readDevice<string>(LEGACY_SPOTLIGHT_KEY, "");
    if (prefs.spotlight == null && SPOTLIGHTS.includes(v)) {
      update.spotlight = v as HomePreferencesUpdate["spotlight"];
    }
    forget.push(LEGACY_SPOTLIGHT_KEY);
  }
  if (hasDevice(LEGACY_PINNED_KEY)) {
    const v = readDevice<number>(LEGACY_PINNED_KEY, 0);
    if (prefs.spotlight_item == null && Number.isInteger(v) && v > 0) update.spotlight_item = v;
    forget.push(LEGACY_PINNED_KEY);
  }
  if (hasDevice(LEGACY_SPOILERS_KEY)) {
    const v = readDevice<string>(LEGACY_SPOILERS_KEY, "");
    if (prefs.spoilers == null && SPOILERS.includes(v)) {
      update.spoilers = v as HomePreferencesUpdate["spoilers"];
    }
    forget.push(LEGACY_SPOILERS_KEY);
  }
  return { update, forget };
}

export function HomePrefsCarryOver() {
  const { data } = useHomePrefs();
  const set = useSetHomePrefs();
  const done = useRef(false);
  useEffect(() => {
    if (done.current || !data?.account) return;
    done.current = true;
    const { update, forget } = carryOverUpdate(data);
    const drop = () => forget.forEach(forgetDevice);
    if (Object.keys(update).length === 0) drop();
    else set.mutate(update, { onSuccess: drop });
    // `set` is a new object each render; reaching for it here is safe because
    // the ref makes this run once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data]);
  return null;
}

/** The libraries Home gives an Unwatched shelf: films and shows, which are
 * what can be left unwatched. Home and its settings pane share this list. */
export function unwatchedShelfLibraries(libraries: Library[] | undefined): Library[] {
  return (libraries ?? []).filter((l) => l.kind === "movie" || l.kind === "show");
}
