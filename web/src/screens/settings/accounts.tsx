import { RetroAchievementsConnection } from "@/components/RetroAchievementsCard";

/*
 * Your account › Connected accounts (settings-3-plan step 4).
 *
 * Services outside LANcast that you have connected to it, each yours alone.
 * RetroAchievements is the only one so far; it lived on the profile page,
 * where a non-admin searching Settings for "achievements" never found it.
 */
export function AccountsPane() {
  return <RetroAchievementsConnection />;
}
