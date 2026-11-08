import type { ComponentType } from "react";
import { ScheduledTasks } from "@/components/ScheduledTasks";
import { PlayerSettings } from "@/components/PlayerSettings";
import { AboutSettings } from "@/components/AboutSettings";
import { KeyBindings } from "@/components/KeyBindings";
import { PeerSettings } from "@/components/PeerSettings";
import { MilkdropSettings } from "@/components/MilkdropSettings";
import { WebhookSettings } from "@/components/WebhookSettings";
import { PublicCertificateSettings } from "@/components/PublicCertificateSettings";
import { GamesSettings } from "@/components/GamesSettings";
import { AddonsSection } from "./addons";
import { AppPane } from "./app";
import { BackupPane } from "./backup";
import { ConversionsSection } from "./conversions";
import { DashboardPane } from "./dashboard";
import { DisplayPane } from "./display";
import { DLNASection } from "./dlna";
import { GeneralPane } from "./general";
import { LanguagesPane } from "./languages";
import { LibrariesPane } from "./libraries";
import { LiveTVSection } from "./livetv";
import { MetadataPane } from "./metadata";
import { NetworkPane } from "./network";
import { PicturesSection } from "./pictures";
import { PrivacySection } from "./privacy";
import { ProfileSection } from "./profile";
import { ProgressPane } from "./progress";
import { QualityPane } from "./quality";
import { RetroSection } from "./retro";
import { SecuritySection } from "./security";
import { TranscodingPane } from "./transcoding";
import { TroubleshootingPane } from "./troubleshooting";
import { UsersPane } from "./users";

/*
 * What each settings pane draws, by the pane's id in settingsIndex.ts.
 *
 * One component per pane, each in its own file beside this one, so a change to
 * one pane is a change to one file (docs/settings-3-plan.md, step 1). The
 * shell in Settings.tsx looks the open pane up here and draws nothing else.
 * panes.test.ts checks that every pane in the nav has a view and every view
 * has a pane, so the two lists cannot drift apart.
 */
export const PANE_VIEWS: Record<string, ComponentType> = {
  dashboard: DashboardPane,
  tasks: ScheduledTasks,
  conversions: ConversionsSection,
  troubleshooting: TroubleshootingPane,
  general: GeneralPane,
  network: NetworkPane,
  remote: PublicCertificateSettings,
  users: UsersPane,
  libraries: LibrariesPane,
  metadata: MetadataPane,
  transcoding: TranscodingPane,
  quality: QualityPane,
  progress: ProgressPane,
  livetv: LiveTVSection,
  pictures: PicturesSection,
  retro: RetroSection,
  peers: PeerSettings,
  dlna: DLNASection,
  webhooks: WebhookSettings,
  visualizers: MilkdropSettings,
  addons: AddonsSection,
  backup: BackupPane,
  profile: ProfileSection,
  security: SecuritySection,
  languages: LanguagesPane,
  privacy: PrivacySection,
  player: PlayerSettings,
  display: DisplayPane,
  keyboard: KeyBindings,
  app: AppPane,
  games: GamesSettings,
  about: AboutSettings,
};
