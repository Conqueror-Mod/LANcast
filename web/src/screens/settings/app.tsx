import { useIsAdmin } from "@/api/hooks";
import { RetroCoreSettings } from "@/components/RetroCoreSettings";
import { DesktopSettings } from "@/components/DesktopSettings";
import { ServerModeSettings } from "@/components/ServerModeSettings";
import { ClientLog } from "@/components/ClientLog";

export function AppPane() {
  const isAdmin = useIsAdmin();
  return (
    <>
      <DesktopSettings />
      <ServerModeSettings isAdmin={isAdmin} />
      <RetroCoreSettings />
      {/*
        On the device pane rather than the admin Logs one. This file is
        written by this window, on this machine, and it is not the
        server's to show: a phone opening Settings has no such file, and
        an administrator reading it from another device would be reading
        nothing.
      */}
      <ClientLog />
    </>
  );
}
