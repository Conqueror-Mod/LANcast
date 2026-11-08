import { ServerHistory } from "@/components/ServerHistory";
import { AuditLog } from "@/components/AuditLog";
import { Review } from "../Review";
import { Transcodes } from "@/components/Transcodes";

export function DashboardPane() {
  return (
    <>
      {/*
        Review has a permanent home here, and only a conditional one
        in the nav.

        The nav entry appears when something needs a look and
        vanishes when nothing does, which is right for a prompt and
        wrong for a page: the two-files-one-work report (ADR 0042)
        is reachable *only* from Review, and dismissing the last
        fixable match takes the door away with it — the collisions
        are still there, still listed, and no longer openable.
        Reported as exactly that.

        Activity is where it belongs. Both panes answer "what has
        this server been doing and what does it want from me", and
        a settings pane is allowed to be empty in a way a nav badge
        is not.
      */}
      <Review />
      {/* What the server has been doing, minute by minute (ADR 0106),
          above the live list of conversions it puts in context. */}
      <ServerHistory />
      {/*
        Conversions sits above the audit log because it is the only
        live reading on the page: everything else here is a record
        of what has happened, and this is what is happening.
      */}
      <Transcodes />
      <AuditLog />
    </>
  );
}
